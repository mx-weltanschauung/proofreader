import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { CommentReviewQueue, MyComments } from './CommentPages';
import { useAuth } from '../hooks/useAuth';
import { readerCommentsApi } from '../services/api';

const comment = {
  id: 12,
  work_id: 7,
  work_title: 'Книга',
  target: 'work',
  body: 'Новая мысль',
  published_body: 'Старая мысль',
  published_revision: 1,
  revision: 3,
  author_nickname: 'читатель',
  status: 'на_рассмотрении',
  can_edit: false,
  can_manage: true,
  chapter_id: null,
};
afterEach(() => {
  vi.restoreAllMocks();
  useAuth.setState({ user: null, isAuthenticated: false });
});
describe('Страницы комментариев', () => {
  it('очередь показывает обе редакции и рассматривает именно открытую редакцию', async () => {
    useAuth.setState({ user: { id: 1, role: 'editor' } as never });
    vi.spyOn(readerCommentsApi, 'queue').mockResolvedValue({ data: [comment] } as never);
    const review = vi.spyOn(readerCommentsApi, 'review').mockResolvedValue({} as never);
    render(
      <MemoryRouter>
        <CommentReviewQueue />
      </MemoryRouter>,
    );
    expect(await screen.findByText('Новая мысль')).toBeInTheDocument();
    expect(screen.getByText('Старая мысль')).toBeInTheDocument();
    fireEvent.click(screen.getByRole('button', { name: 'Одобрить' }));
    await waitFor(() => expect(review).toHaveBeenCalledWith(12, 3, true));
  });
  it('свои комментарии доступны вместе с причиной отказа и ссылкой на текст', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    vi.spyOn(readerCommentsApi, 'mine').mockResolvedValue({
      data: [{ ...comment, status: 'отклонено', reject_reason: 'Не по теме', can_edit: true }],
    } as never);
    render(
      <MemoryRouter>
        <MyComments />
      </MemoryRouter>,
    );
    expect(await screen.findByText('Отклонён: Не по теме')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Книга' })).toHaveAttribute('href', '/works/7');
  });
});
