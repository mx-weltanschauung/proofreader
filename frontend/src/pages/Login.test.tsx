import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';

vi.mock('../services/api', () => ({
  authApi: {
    login: vi.fn().mockResolvedValue({
      data: { user: { id: 1, email: 'e@x.io', role: 'editor' }, token: 'tok' },
    }),
    me: vi.fn(),
  },
}));

import { Login } from './Login';
import { useAuth } from '../hooks/useAuth';

const TARGET = '/works/6/pages/493/edit';

function renderAt(entry: string) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <Routes>
        <Route path="/login" element={<Login />} />
        <Route path="/" element={<div>home page</div>} />
        <Route path={TARGET} element={<div>edit page</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

async function submitLogin() {
  const user = userEvent.setup();
  await user.type(screen.getByLabelText('Почта'), 'e@x.io');
  await user.type(screen.getByLabelText('Пароль'), 'secret');
  await user.click(screen.getByRole('button', { name: 'Войти' }));
}

describe('Login', () => {
  it('форма входа подписана по-русски', () => {
    renderAt('/login');
    expect(screen.getByRole('heading', { name: 'Вход' })).toBeInTheDocument();
    expect(screen.getByLabelText('Почта')).toBeInTheDocument();
    expect(screen.getByLabelText('Пароль')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Войти' })).toBeInTheDocument();
  });

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('после входа возвращает на адрес из next', async () => {
    renderAt(`/login?next=${encodeURIComponent(TARGET)}`);
    await submitLogin();
    await waitFor(() => expect(screen.getByText('edit page')).toBeInTheDocument());
  });

  it('внешний next игнорируется — уводит на главную', async () => {
    renderAt('/login?next=https%3A%2F%2Fevil.example');
    await submitLogin();
    await waitFor(() => expect(screen.getByText('home page')).toBeInTheDocument());
  });

  it('без next уводит на главную', async () => {
    renderAt('/login');
    await submitLogin();
    await waitFor(() => expect(screen.getByText('home page')).toBeInTheDocument());
  });

  it('уже залогиненного сразу отправляет по next, не показывая форму', () => {
    useAuth.setState({
      user: { id: 1, email: 'e@x.io', role: 'editor' },
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    renderAt(`/login?next=${encodeURIComponent(TARGET)}`);
    expect(screen.getByText('edit page')).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: 'Войти' })).toBeNull();
  });

  it('уже залогиненного с внешним next уводит на главную, а не на внешний адрес', () => {
    useAuth.setState({
      user: { id: 1, email: 'e@x.io', role: 'editor' },
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    renderAt('/login?next=https%3A%2F%2Fevil.example');
    expect(screen.getByText('home page')).toBeInTheDocument();
  });
});
