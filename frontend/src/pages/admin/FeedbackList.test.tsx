import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { feedbackApi } from '../../services/api';
import type { Feedback } from '../../types';
import { FeedbackList } from './FeedbackList';

vi.mock('../../services/api', () => ({
  feedbackApi: {
    list: vi.fn(),
    setHandled: vi.fn(),
    remove: vi.fn(),
  },
}));

vi.mock('react-hot-toast', () => ({
  default: { success: vi.fn(), error: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const LETTERS: Feedback[] = [
  {
    id: 2,
    message: 'на 412-й опечатка',
    source_path: '/works/16/pages/412',
    handled_at: null,
    created_at: '2026-08-27T10:00:00Z',
  },
  {
    id: 1,
    message: 'спасибо за читальню',
    source_path: '',
    handled_at: '2026-08-26T10:00:00Z',
    created_at: '2026-08-26T09:00:00Z',
  },
];

describe('разбор обращений', () => {
  beforeEach(() => {
    vi.mocked(feedbackApi.list).mockReset();
    vi.mocked(feedbackApi.setHandled).mockReset();
    vi.mocked(feedbackApi.remove).mockReset();
    vi.mocked(feedbackApi.list).mockReturnValue(ok(LETTERS));
  });

  function renderList() {
    return render(
      <MemoryRouter>
        <FeedbackList />
      </MemoryRouter>,
    );
  }

  it('показывает письма и ссылку на место, о котором речь', async () => {
    renderList();

    expect(await screen.findByText('на 412-й опечатка')).toBeInTheDocument();
    expect(screen.getByText('спасибо за читальню')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: '/works/16/pages/412' })).toHaveAttribute(
      'href',
      '/works/16/pages/412',
    );
  });

  it('помечает письмо разобранным', async () => {
    vi.mocked(feedbackApi.setHandled).mockReturnValue(ok({ ok: true }));
    renderList();

    await screen.findByText('на 412-й опечатка');
    await userEvent.click(screen.getAllByRole('button', { name: 'Разобрано' })[0]);

    await waitFor(() => expect(feedbackApi.setHandled).toHaveBeenCalledWith(2, true));
  });

  it('удаляет письмо только после подтверждения', async () => {
    vi.mocked(feedbackApi.remove).mockReturnValue(ok(undefined));
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    renderList();

    await screen.findByText('на 412-й опечатка');
    await userEvent.click(screen.getAllByRole('button', { name: 'Удалить' })[0]);
    expect(feedbackApi.remove).not.toHaveBeenCalled();

    confirmSpy.mockReturnValue(true);
    await userEvent.click(screen.getAllByRole('button', { name: 'Удалить' })[0]);
    await waitFor(() => expect(feedbackApi.remove).toHaveBeenCalledWith(2));

    confirmSpy.mockRestore();
  });

  it('переключатель «только новые» запрашивает их у сервера', async () => {
    renderList();

    await screen.findByText('на 412-й опечатка');
    await userEvent.click(screen.getByLabelText(/только новые/i));

    await waitFor(() => expect(feedbackApi.list).toHaveBeenLastCalledWith(false));
  });

  // Пустой ответ не должен ронять страницу: списочные маршруты читальни
  // местами отдают null вместо [].
  it('переживает пустой ответ', async () => {
    vi.mocked(feedbackApi.list).mockReturnValue(ok(null as unknown as Feedback[]));
    renderList();

    expect(await screen.findByText(/писем пока нет/i)).toBeInTheDocument();
  });

  // Приёмный бэкенд чистит source_path (normalizeSourcePath), но админка не
  // должна полагаться на то, что в базу ничего не попало другим путём —
  // правкой руками, будущим каналом записи, переносом старых данных.
  it('не рисует ссылку наружу, даже если source_path — чужой адрес', async () => {
    vi.mocked(feedbackApi.list).mockReturnValue(
      ok([
        {
          id: 3,
          message: 'фишинговое письмо',
          source_path: 'https://evil.example.com/phish',
          handled_at: null,
          created_at: '2026-08-27T10:00:00Z',
        },
      ]),
    );
    renderList();

    expect(await screen.findByText('https://evil.example.com/phish')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /evil\.example\.com/ })).toBeNull();
  });
});
