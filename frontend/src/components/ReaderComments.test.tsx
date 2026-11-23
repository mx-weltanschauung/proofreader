import { act, render, screen, fireEvent, waitFor, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { describe, it, expect, vi, afterEach } from 'vitest';
import { CommentedMarginNotes, ReaderCommentCard, TargetComments } from './ReaderComments';
import { useAuth } from '../hooks/useAuth';
import { readerCommentsApi } from '../services/api';
import type { ReaderComment } from '../types';
import { DEFAULT_PREFS } from '../contexts/readingPrefs';
import { ReadingPreferencesContext } from '../contexts/readingPrefsContext';

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

const root = {
  ...comment,
  work_id: 7,
  target: 'work',
  body: 'Исходный комментарий',
  published: true,
  status: undefined,
  published_body: undefined,
  can_edit: false,
  can_manage: false,
  reply_count: 1,
} satisfies ReaderComment;
const reply = {
  ...root,
  id: 2,
  thread_id: root.id,
  body: 'Одобренный ответ',
  author_nickname: 'Собеседник',
  reply_count: undefined,
};

describe('Ветки комментариев', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, isAuthenticated: false });
  });

  it('свёрнутая ветка показывает одобренный ответ после обновления списка тома', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    const empty = { ...root, reply_count: undefined };
    vi.spyOn(readerCommentsApi, 'ofWork')
      .mockResolvedValueOnce({ data: [empty] } as never)
      .mockResolvedValue({ data: [root] } as never);
    vi.spyOn(readerCommentsApi, 'thread')
      .mockResolvedValueOnce({ data: { root: empty, replies: [] } } as never)
      .mockResolvedValue({ data: { root, replies: [reply] } } as never);
    vi.spyOn(document, 'visibilityState', 'get').mockReturnValue('visible');
    render(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByText(/Комментарии к тому/));
    fireEvent.click(screen.getByRole('button', { name: 'Ответить' }));
    fireEvent.click(await screen.findByRole('button', { name: 'Отмена' }));
    fireEvent.click(screen.getByRole('button', { name: 'Скрыть ответы' }));
    expect(screen.queryByRole('button', { name: /Ответы/ })).toBeNull();
    await act(async () => document.dispatchEvent(new Event('visibilitychange')));
    fireEvent.click(await screen.findByRole('button', { name: 'Ответы (1)' }));
    expect(await screen.findByText(reply.body)).toBeVisible();
  });

  it('свою ожидающую реплику можно снова открыть после сворачивания ветки', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    const empty = { ...root, reply_count: undefined };
    const pending = {
      ...reply,
      body: 'Реплика на проверке',
      published: false,
      status: 'на_рассмотрении',
      can_edit: true,
    };
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [empty] } as never);
    vi.spyOn(readerCommentsApi, 'thread')
      .mockResolvedValueOnce({ data: { root: empty, replies: [] } } as never)
      .mockResolvedValue({ data: { root: empty, replies: [pending] } } as never);
    vi.spyOn(readerCommentsApi, 'reply').mockResolvedValue({ data: pending } as never);
    render(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByText(/Комментарии к тому/));
    fireEvent.click(screen.getByRole('button', { name: 'Ответить' }));
    fireEvent.change(await screen.findByRole('textbox'), { target: { value: pending.body } });
    fireEvent.click(screen.getByRole('button', { name: 'Отправить на проверку' }));
    expect(await screen.findByText('Ждёт проверки')).toBeVisible();
    fireEvent.click(screen.getByRole('button', { name: 'Скрыть ответы' }));
    fireEvent.click(screen.getByRole('button', { name: 'Ответы' }));
    expect(await screen.findByText(pending.body)).toBeVisible();
  });

  it('загружает ответы при раскрытии и показывает адресата ответа на ответ', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [root] } as never);
    const thread = vi
      .spyOn(readerCommentsApi, 'thread')
      .mockResolvedValue({ data: { root, replies: [reply] } } as never);
    const create = vi.spyOn(readerCommentsApi, 'reply').mockResolvedValue({
      data: {
        ...reply,
        id: 3,
        body: 'Новая реплика',
        reply_to_nickname: reply.author_nickname,
        published: false,
        status: 'на_рассмотрении',
        can_edit: true,
      },
    } as never);
    render(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByText(/Комментарии к тому/));
    expect(thread).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Ответы (1)' }));
    const replyCard = (await screen.findByText(reply.body)).closest('.reader-comment')!;
    fireEvent.click(within(replyCard as HTMLElement).getByRole('button', { name: 'Ответить' }));
    fireEvent.change(screen.getByRole('textbox', { name: 'Текст комментария' }), {
      target: { value: 'Новая реплика' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Отправить на проверку' }));
    expect(
      await screen.findByText('Новая реплика', { selector: '.reader-comment-body' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Ответ читателю Собеседник')).toBeInTheDocument();
    expect(screen.getByText('Ждёт проверки')).toBeInTheDocument();
    expect(create).toHaveBeenCalledWith(reply.id, 'Новая реплика');
    expect(screen.queryByText('Вернуть в заметки')).toBeNull();
  });

  it('после удаления корня сохраняет опубликованные ответы под пометкой', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    const own = { ...root, can_edit: true, can_manage: true };
    const tombstone = { ...own, removed: 'author', published: false };
    vi.spyOn(readerCommentsApi, 'ofWork')
      .mockResolvedValueOnce({ data: [own] } as never)
      .mockResolvedValue({ data: [tombstone] } as never);
    vi.spyOn(readerCommentsApi, 'thread').mockResolvedValue({
      data: { root: tombstone, replies: [reply] },
    } as never);
    vi.spyOn(readerCommentsApi, 'remove').mockResolvedValue({} as never);
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    render(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByText(/Комментарии к тому/));
    fireEvent.click(screen.getByRole('button', { name: 'Удалить' }));
    expect(await screen.findByText('Комментарий удалён')).toBeInTheDocument();
    expect(screen.queryByText(root.body)).toBeNull();
    expect(screen.queryByRole('button', { name: 'Изменить' })).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Ответы (1)' }));
    expect(await screen.findByText(reply.body)).toBeInTheDocument();
  });

  it('по прямому адресу открывает только нужную ветку у абзаца и гасит показанные ответы', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    const foreign = {
      ...root,
      target: 'quote',
      start_page: 5,
      quote_key: 'Второй абзац.',
    };
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [] } as never);
    const thread = vi.spyOn(readerCommentsApi, 'thread').mockResolvedValue({
      data: {
        root: foreign,
        replies: [
          { ...reply, unread: true },
          { ...reply, id: 4, body: 'Другой ответ' },
        ],
      },
    } as never);
    const markRead = vi.spyOn(readerCommentsApi, 'markThreadRead').mockImplementation(async () => {
      expect(screen.getByText(reply.body)).toBeVisible();
      return {} as never;
    });
    const paragraph = document.createElement('div');
    paragraph.className = 'page-html-content';
    paragraph.dataset.page = '5';
    paragraph.innerHTML = '<p>Первый.</p><p>Второй абзац.</p>';
    document.body.append(paragraph);
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
    const { unmount } = render(
      <MemoryRouter initialEntries={['/works/7/pages/5?thread=1']}>
        <ReadingPreferencesContext.Provider
          value={{
            prefs: { ...DEFAULT_PREFS, readerComments: false },
            setPref: vi.fn(),
            reset: vi.fn(),
          }}
        >
          <CommentedMarginNotes
            workId={7}
            threadId={1}
            contentRef={{ current: paragraph }}
            notes={[]}
            pageNumbers={[5]}
            deps={[]}
            onChange={vi.fn()}
            onDelete={vi.fn()}
          />
        </ReadingPreferencesContext.Provider>
      </MemoryRouter>,
    );
    try {
      const rendered = await screen.findByText(reply.body);
      expect(rendered.closest('.margin-note-host')?.previousElementSibling).toHaveTextContent(
        'Второй абзац.',
      );
      await waitFor(() => expect(markRead).toHaveBeenCalledWith(root.id, [reply.id]));
      expect(thread).toHaveBeenCalledTimes(1);
      expect(thread).toHaveBeenCalledWith(root.id);
      await waitFor(() => expect(scroll).toHaveBeenCalled());
      expect(scroll.mock.contexts[0]).toHaveTextContent('Второй абзац.');
      expect(readerCommentsApi.ofWork).toHaveBeenCalledWith(7, false, 0);
    } finally {
      unmount();
      paragraph.remove();
    }
  });

  it('прямая ссылка прокручивает к началу своей цитаты при пересечении с чужой', async () => {
    const foreign = {
      ...root,
      id: 3,
      target: 'quote',
      start_page: 5,
      quote_key: 'Первый абзац.',
      quote_tail: 'Второй',
      body: 'Комментарий к пересекающейся цитате',
    };
    const linked = {
      ...root,
      target: 'quote',
      start_page: 5,
      quote_key: 'Второй абзац.',
      quote_tail: '',
    };
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [foreign] } as never);
    vi.spyOn(readerCommentsApi, 'thread').mockResolvedValue({
      data: { root: linked, replies: [reply] },
    } as never);
    const rootEl = document.createElement('div');
    rootEl.className = 'page-html-content';
    rootEl.dataset.page = '5';
    rootEl.innerHTML = '<p>Первый абзац.</p><p>Второй абзац.</p>';
    document.body.append(rootEl);
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView');
    const { unmount } = render(
      <MemoryRouter initialEntries={['/works/7/pages/5?thread=1']}>
        <CommentedMarginNotes
          workId={7}
          threadId={root.id}
          contentRef={{ current: rootEl }}
          notes={[]}
          pageNumbers={[5]}
          deps={[]}
          onChange={vi.fn()}
          onDelete={vi.fn()}
        />
      </MemoryRouter>,
    );
    try {
      const rendered = await screen.findByText(reply.body);
      expect(rendered.closest('.margin-note-host')?.previousElementSibling).toHaveTextContent(
        'Второй абзац.',
      );
      await waitFor(() => expect(scroll).toHaveBeenCalled());
      expect(scroll.mock.contexts[0]).toHaveTextContent('Второй абзац.');
      expect(rootEl.querySelector('p mark.margin-hit')).toHaveAttribute('data-note-ids', '-3 -1');
    } finally {
      unmount();
      rootEl.remove();
    }
  });

  it('прочитывает только полученные ответы пакетами и учитывает новый ответ при повторном открытии', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    const unread = Array.from({ length: 201 }, (_, i) => ({
      ...reply,
      id: 10 + i,
      unread: true,
      body: `Реплика ${i}`,
    }));
    const own = { ...root, reply_count: unread.length };
    const late = { ...reply, id: 999, unread: true, body: 'Поздний ответ' };
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [own] } as never);
    const thread = vi
      .spyOn(readerCommentsApi, 'thread')
      .mockResolvedValueOnce({ data: { root: own, replies: unread } } as never)
      .mockResolvedValue({ data: { root: own, replies: [...unread, late] } } as never);
    const markRead = vi.spyOn(readerCommentsApi, 'markThreadRead').mockResolvedValue({} as never);
    render(
      <MemoryRouter>
        <TargetComments workId={7} />
      </MemoryRouter>,
    );
    fireEvent.click(await screen.findByText(/Комментарии к тому/));
    expect(markRead).not.toHaveBeenCalled();
    fireEvent.click(screen.getByRole('button', { name: 'Ответы (201)' }));
    await waitFor(() => expect(markRead).toHaveBeenCalledTimes(2));
    expect(markRead).toHaveBeenNthCalledWith(
      1,
      root.id,
      unread.slice(0, 200).map((r) => r.id),
    );
    expect(markRead).toHaveBeenNthCalledWith(2, root.id, [unread[200].id]);
    fireEvent.click(screen.getByRole('button', { name: 'Скрыть ответы' }));
    expect(screen.queryByText('Поздний ответ')).toBeNull();
    fireEvent.click(screen.getByRole('button', { name: 'Ответы (201)' }));
    expect(await screen.findByText('Поздний ответ')).toBeVisible();
    await waitFor(() => expect(markRead).toHaveBeenCalledTimes(3));
    expect(markRead).toHaveBeenLastCalledWith(root.id, [late.id]);
    expect(thread).toHaveBeenCalledTimes(2);
  });

  it('прямая ссылка к тому не показывает и не прочитывает ветку другой работы', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    vi.spyOn(readerCommentsApi, 'ofWork').mockResolvedValue({ data: [] } as never);
    const thread = vi.spyOn(readerCommentsApi, 'thread').mockResolvedValue({
      data: { root: { ...root, work_id: 8 }, replies: [{ ...reply, unread: true }] },
    } as never);
    const markRead = vi.spyOn(readerCommentsApi, 'markThreadRead');
    render(
      <MemoryRouter>
        <TargetComments workId={7} threadId={root.id} />
      </MemoryRouter>,
    );
    await waitFor(() => expect(thread).toHaveBeenCalledWith(root.id));
    expect(screen.queryByText(root.body)).toBeNull();
    expect(markRead).not.toHaveBeenCalled();
  });
});
