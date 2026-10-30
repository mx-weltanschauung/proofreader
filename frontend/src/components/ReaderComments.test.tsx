import { render, screen, fireEvent, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { ReaderCommentCard, TargetComments } from './ReaderComments';
import { useAuth } from '../hooks/useAuth';
import { readerCommentsApi } from '../services/api';
import type { ReaderComment } from '../types';

const comment = {
  id: 1,
  body: '<img src=x onerror=alert(1)>',
  author_nickname: 'Читатель',
  status: 'на_рассмотрении',
  revision: 2,
  can_edit: true,
  can_manage: true,
  published_body: 'Прежнее',
  published_revision: 1,
} as ReaderComment;
describe('ReaderCommentCard', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, isAuthenticated: false });
  });
  it('смена пользователя сбрасывает открытый редактор нового комментария', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [] } as never);
    const { rerender } = render(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Комментарий к тому' }));
    fireEvent.change(screen.getByRole('textbox'), { target: { value: 'Чужой черновик' } });
    useAuth.setState({ user: { id: 4, role: 'reader' } as never });
    rerender(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole('textbox')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Комментарий к тому' }));
    expect(screen.getByRole('textbox')).toHaveValue('');
  });
  it('показывает простой текст и состояние, без исполнения HTML', () => {
    const { container } = render(
      <MemoryRouter>
        <ReaderCommentCard comment={comment} onChange={vi.fn()} onDelete={vi.fn()} />
      </MemoryRouter>,
    );
    expect(screen.getByText('<img src=x onerror=alert(1)>')).toBeInTheDocument();
    expect(container.querySelector('img')).toBeNull();
    expect(screen.getByText(/Ждёт проверки/)).toBeInTheDocument();
  });
  it('сохраняет текст после отказа и отправляет номер редакции', async () => {
    vi.spyOn(readerCommentsApi, 'update').mockRejectedValue({ response: { status: 401 } });
    render(
      <MemoryRouter>
        <ReaderCommentCard comment={comment} onChange={vi.fn()} onDelete={vi.fn()} />
      </MemoryRouter>,
    );
    fireEvent.click(screen.getByRole('button', { name: 'Изменить' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Текст комментария' }), {
      target: { value: 'Новая мысль' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Отправить на проверку' }));
    await waitFor(() => expect(screen.getByRole('alert')).toBeInTheDocument());
    expect(screen.getByRole('textbox')).toHaveValue('Новая мысль');
    expect(readerCommentsApi.update).toHaveBeenCalledWith(1, 'Новая мысль', 2);
  });
});
