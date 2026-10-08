import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

vi.mock('../services/api', () => ({
  authApi: {
    join: vi.fn().mockResolvedValue({
      data: { user: { id: 5, email: '', nickname: 'Читатель', role: 'reader' }, token: 'tok' },
    }),
    me: vi.fn(),
  },
}));

import { Join } from './Join';
import { authApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';

const TARGET = '/works/6/pages/493/suggest';

function renderAt(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/join" element={<Join />} />
        <Route path="/" element={<div>home page</div>} />
        <Route path={TARGET} element={<div>suggest page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

async function fillForm(nickname: string, password: string) {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText('Имя'), nickname);
  await user.type(screen.getByLabelText('Пароль'), password);
  await user.click(screen.getByRole('button', { name: 'Записаться' }));
}

describe('Join', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  // Единственное место, где читатель узнаёт о невосстановимости пароля —
  // до того, как заведётся. Три факта проверяются по отдельности: сжатая
  // фраза вроде «Восстановление пароля недоступно» проходит /восстанов/i
  // не хуже полной, но не говорит читателю ни что почты не будет, ни что
  // пароль нужно записать самому.
  it('показывает, что восстанавливать пароль нечем', () => {
    render(<Join />, { wrapper: MemoryRouter });
    const warning = screen.getByText(/восстанов/i);
    // Каждая проверка — свой факт, а не пересказ одной и той же фразы: сжатая
    // формулировка вроде «Восстановление пароля недоступно» проходит первую
    // регулярку и молчит про остальные два факта.
    expect(warning).toHaveTextContent(/почты\s+мы\s+не\s+спрашиваем/i);
    expect(warning).toHaveTextContent(/сброса\s+не\s+делаем/i);
    expect(warning).toHaveTextContent(/запишите/i);
  });

  it('форма подписана по-русски', () => {
    render(<Join />, { wrapper: MemoryRouter });
    expect(screen.getByRole('heading', { name: 'Записаться в читальню' })).toBeInTheDocument();
    expect(screen.getByLabelText('Имя')).toBeInTheDocument();
    expect(screen.getByLabelText('Пароль')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Записаться' })).toBeInTheDocument();
  });

  it('не отправляет форму с коротким паролем', async () => {
    render(<Join />, { wrapper: MemoryRouter });
    await fillForm('Читатель', 'коротк');

    expect(authApi.join).not.toHaveBeenCalled();
    expect(await screen.findByText(/не короче/i)).toBeInTheDocument();
  });

  // Рецензия: предел сервера — 72 БАЙТА, не знака. maxLength на поле считает
  // знаки (UTF-16), поэтому 40 букв кириллицы (80 байт) проходят через него
  // не глядя — настоящую проверку делает byteLength в handleSubmit, той же
  // мерой, что и сервер.
  it('не отправляет форму с паролем длиннее 72 байт (кириллица)', async () => {
    render(<Join />, { wrapper: MemoryRouter });
    await fillForm('Читатель', 'ж'.repeat(40));

    expect(authApi.join).not.toHaveBeenCalled();
    expect(await screen.findByText(/72\s*байт/i)).toBeInTheDocument();
  });

  it('поле пароля ограничено 72 знаками (maxLength)', () => {
    render(<Join />, { wrapper: MemoryRouter });
    expect(screen.getByLabelText('Пароль')).toHaveAttribute('maxLength', '72');
  });

  it('передаёт ник и пароль в join', async () => {
    renderAt('/join');
    await fillForm('Читатель', 'secretpass');

    await waitFor(() => expect(authApi.join).toHaveBeenCalledWith('Читатель', 'secretpass'));
  });

  it('после входа возвращает на адрес из next', async () => {
    renderAt(`/join?next=${encodeURIComponent(TARGET)}`);
    await fillForm('Читатель', 'secretpass');
    await waitFor(() => expect(screen.getByText('suggest page')).toBeInTheDocument());
  });

  it('внешний next игнорируется — уводит на главную', async () => {
    renderAt('/join?next=https%3A%2F%2Fevil.example');
    await fillForm('Читатель', 'secretpass');
    await waitFor(() => expect(screen.getByText('home page')).toBeInTheDocument());
  });

  it('без next уводит на главную', async () => {
    renderAt('/join');
    await fillForm('Читатель', 'secretpass');
    await waitFor(() => expect(screen.getByText('home page')).toBeInTheDocument());
  });

  it('уже вошедшего читателя сразу отправляет по next, не показывая форму', () => {
    useAuth.setState({
      user: { id: 5, email: '', nickname: 'Читатель', role: 'reader' },
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    renderAt(`/join?next=${encodeURIComponent(TARGET)}`);
    expect(screen.getByText('suggest page')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Записаться' })).toBeNull();
  });

  it('показывает ошибку сервера при отказе', async () => {
    vi.mocked(authApi.join).mockRejectedValueOnce({
      response: { status: 401, data: { message: 'Неверный пароль' } },
    });
    render(<Join />, { wrapper: MemoryRouter });
    await fillForm('Читатель', 'secretpass');
    expect(await screen.findByText('Неверный пароль')).toBeInTheDocument();
  });
});
