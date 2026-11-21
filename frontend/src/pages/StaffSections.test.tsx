import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { useAuth } from '../hooks/useAuth';
import { feedbackApi, readerCommentsApi, suggestionsApi } from '../services/api';
import { StaffSections } from './StaffSections';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as AxiosResponse<T>);
}

function signIn(role: 'administrator' | 'editor' | 'reader') {
  useAuth.setState({
    user: { id: 1, email: 'test@example.org', role },
    token: 'test',
    isAuthenticated: true,
    isLoading: false,
  });
}

function page() {
  return render(
    <MemoryRouter>
      <StaffSections />
    </MemoryRouter>,
  );
}

describe('StaffSections', () => {
  beforeEach(() => {
    vi.spyOn(suggestionsApi, 'queue').mockReturnValue(ok({ items: [], total: 3 }) as never);
    vi.spyOn(readerCommentsApi, 'pendingCount').mockReturnValue(ok({ count: 4 }));
    vi.spyOn(feedbackApi, 'unreadCount').mockReturnValue(ok({ count: 1 }));
  });
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('администратору группирует все разделы и показывает счётчики у нужных ссылок', async () => {
    signIn('administrator');
    page();
    const frequent = screen.getByRole('region', { name: 'Часто используемые' });
    expect(within(frequent).getByRole('link', { name: /Предложения/ })).toHaveAttribute(
      'href',
      '/suggestions/queue',
    );
    expect(within(frequent).getByRole('link', { name: 'Читатели' })).toHaveAttribute(
      'href',
      '/admin/readers',
    );
    expect(within(frequent).getByRole('link', { name: 'Статистика' })).toHaveAttribute(
      'href',
      '/admin/stats',
    );
    const work = screen.getByRole('region', { name: 'Работа' });
    expect(within(work).getByRole('link', { name: 'Разборы на проверку' })).toHaveAttribute(
      'href',
      '/documents/review',
    );
    expect(within(work).getByRole('link', { name: /Комментарии/ })).toHaveAttribute(
      'href',
      '/reader-comments/review',
    );
    expect(within(work).getByRole('link', { name: 'Озвучка' })).toHaveAttribute(
      'href',
      '/admin/audio',
    );
    const management = screen.getByRole('region', { name: 'Управление' });
    for (const [name, href] of [
      ['Пользователи', '/admin/users'],
      ['Обращения', '/admin/feedback'],
      ['Закрытия', '/admin/closures'],
      ['Кэш', '/admin/cache'],
      ['Настройки', '/admin/settings'],
    ])
      expect(within(management).getByRole('link', { name: new RegExp(name) })).toHaveAttribute(
        'href',
        href,
      );
    const mine = screen.getByRole('region', { name: 'Моё' });
    expect(within(mine).getByRole('link', { name: 'Заметки' })).toHaveAttribute(
      'href',
      '/mine/notes',
    );
    expect(within(mine).getByRole('link', { name: 'Мои комментарии' })).toHaveAttribute(
      'href',
      '/mine/comments',
    );
    await waitFor(() =>
      expect(within(frequent).getByRole('link', { name: 'Предложения 3' })).toBeInTheDocument(),
    );
    expect(within(work).getByRole('link', { name: 'Комментарии 4' })).toBeInTheDocument();
    expect(within(management).getByRole('link', { name: 'Обращения 1' })).toBeInTheDocument();
  });

  it('редактору не показывает административные разделы и не запрашивает обращения', async () => {
    signIn('editor');
    page();
    expect(screen.queryByRole('region', { name: 'Управление' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Читатели' })).toBeNull();
    expect(screen.queryByRole('link', { name: 'Статистика' })).toBeNull();
    await screen.findByRole('link', { name: 'Комментарии 4' });
    expect(feedbackApi.unreadCount).not.toHaveBeenCalled();
  });

  it('читателю и гостю не показывает служебные ссылки', () => {
    signIn('reader');
    const view = page();
    expect(screen.queryByRole('link')).toBeNull();
    view.unmount();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    page();
    expect(screen.queryByRole('link')).toBeNull();
  });

  it('при ошибке счётчиков ссылки остаются доступными', async () => {
    vi.mocked(suggestionsApi.queue).mockRejectedValue(new Error('нет сети'));
    vi.mocked(readerCommentsApi.pendingCount).mockRejectedValue(new Error('нет сети'));
    vi.mocked(feedbackApi.unreadCount).mockRejectedValue(new Error('нет сети'));
    signIn('administrator');
    page();
    await waitFor(() =>
      expect(screen.getByRole('link', { name: 'Предложения' })).toBeInTheDocument(),
    );
    expect(screen.getByRole('link', { name: 'Комментарии' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Обращения' })).toBeInTheDocument();
  });
});
