import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { feedbackApi } from '../services/api';
import { Feedback } from './Feedback';

vi.mock('../services/api', () => ({
  feedbackApi: { send: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function renderAt(path = '/feedback') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Feedback />
    </MemoryRouter>,
  );
}

describe('форма обращения', () => {
  beforeEach(() => {
    vi.mocked(feedbackApi.send).mockReset();
  });

  it('отправляет письмо и путь, откуда пришёл читатель', async () => {
    vi.mocked(feedbackApi.send).mockReturnValue(ok({ ok: true }));
    renderAt('/feedback?from=/works/16/pages/412');

    await userEvent.type(screen.getByLabelText(/сообщение/i), 'нашёл опечатку');
    await userEvent.click(screen.getByRole('button', { name: /отправить/i }));

    await waitFor(() => expect(feedbackApi.send).toHaveBeenCalledTimes(1));
    // Контакта в теле нет и быть не может: поле убрано, состояние тоже.
    expect(feedbackApi.send).toHaveBeenCalledWith({
      message: 'нашёл опечатку',
      source_path: '/works/16/pages/412',
      binding_ref: '',
    });
  });

  it('после отправки показывает благодарность вместо формы', async () => {
    vi.mocked(feedbackApi.send).mockReturnValue(ok({ ok: true }));
    renderAt();

    await userEvent.type(screen.getByLabelText(/сообщение/i), 'спасибо за читальню');
    await userEvent.click(screen.getByRole('button', { name: /отправить/i }));

    expect(await screen.findByText(/письмо ушло/i)).toBeInTheDocument();
    expect(screen.queryByLabelText(/сообщение/i)).not.toBeInTheDocument();
  });

  it('не отправляет пустое письмо', async () => {
    renderAt();

    await userEvent.click(screen.getByRole('button', { name: /отправить/i }));

    expect(feedbackApi.send).not.toHaveBeenCalled();
  });

  // Текст сервера обязан доехать до читателя: иначе он не поймёт, почему
  // письмо не ушло, и просто потеряет написанное.
  it('показывает объяснение сервера при отказе', async () => {
    vi.mocked(feedbackApi.send).mockRejectedValue({
      response: {
        status: 429,
        data: { message: 'вы уже отправили несколько писем за последний час, попробуйте позже' },
      },
    });
    renderAt();

    await userEvent.type(screen.getByLabelText(/сообщение/i), 'ещё одно');
    await userEvent.click(screen.getByRole('button', { name: /отправить/i }));

    expect(await screen.findByText(/за последний час/i)).toBeInTheDocument();
    // Написанное не пропало: читателю есть что отправить повторно.
    expect(screen.getByLabelText(/сообщение/i)).toHaveValue('ещё одно');
  });

  it('ловушка спрятана от читателя и уезжает пустой', async () => {
    vi.mocked(feedbackApi.send).mockReturnValue(ok({ ok: true }));
    const { container } = renderAt();

    const trap = container.querySelector('input[name="binding_ref"]');
    expect(trap).not.toBeNull();
    expect(trap).toHaveAttribute('tabindex', '-1');
    expect(trap).toHaveAttribute('aria-hidden', 'true');
  });

  // Экран благодарности рисует ?from= живой ссылкой до всякого обращения к
  // серверу: без проверки чужой сайт в адресной строке увёл бы читателя
  // наружу прямо со страницы читальни.
  it('не уводит на чужой сайт из ?from=', async () => {
    vi.mocked(feedbackApi.send).mockReturnValue(ok({ ok: true }));
    renderAt('/feedback?from=https://evil.example.com/phish');

    await userEvent.type(screen.getByLabelText(/сообщение/i), 'нашёл опечатку');
    await userEvent.click(screen.getByRole('button', { name: /отправить/i }));

    await waitFor(() => expect(feedbackApi.send).toHaveBeenCalledTimes(1));
    expect(feedbackApi.send).toHaveBeenCalledWith(expect.objectContaining({ source_path: '' }));

    const link = await screen.findByRole('link', { name: /вернуться к чтению/i });
    expect(link).toHaveAttribute('href', '/');
  });

  // Всё утверждение «читальня не собирает персональных данных» держится на
  // отсутствии этого поля. Вернут «ради удобства» — падает здесь.
  it('не спрашивает обратного адреса', () => {
    renderAt();
    expect(screen.queryByLabelText(/как ответить/i)).toBeNull();
    expect(screen.getByText(/Письма анонимны/)).toBeInTheDocument();
    // Ниже формы жила приписка «сохраняются текст письма, контакт (если вы
    // его оставили)…» — поле убрали, а она осталась и врала. Тесты этого не
    // видели, поймал скриншот. Теперь слова «контакт» на странице быть не
    // должно вовсе.
    expect(screen.queryByText(/контакт/i)).toBeNull();
    expect(screen.getByRole('link', { name: 'Почему так' })).toHaveAttribute(
      'href',
      '/legal#personal-data',
    );
  });
});
