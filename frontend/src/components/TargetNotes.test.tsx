import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { TargetNotes } from './TargetNotes';
import { marginNotesApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { MarginNote } from '../types';

function note(over: Partial<MarginNote>): MarginNote {
  return {
    id: 1,
    work_id: 7,
    target: 'chapter',
    chapter_id: 10,
    chapter_title: '',
    start_page: null,
    end_page: null,
    page_missing: false,
    snapshot_start_page: null,
    quote_key: '',
    quote_tail: '',
    quote_text: '',
    body: 'текст',
    created_at: '',
    updated_at: '',
    ...over,
  };
}

const NOTES: MarginNote[] = [
  note({ id: 1, body: 'к главе 10' }),
  note({ id: 2, chapter_id: 11, body: 'к главе 11' }),
  note({ id: 3, target: 'work', chapter_id: null, body: 'к тому' }),
  note({ id: 4, target: 'quote', chapter_id: null, body: 'к цитате' }),
];

const handlers = () => ({ onCreated: vi.fn(), onChange: vi.fn(), onDelete: vi.fn() });

function setup(props: { chapterId?: number; notes?: MarginNote[] }) {
  const h = handlers();
  render(
    <MemoryRouter>
      <TargetNotes workId={7} chapterId={props.chapterId} notes={props.notes ?? NOTES} {...h} />
    </MemoryRouter>,
  );
  return h;
}

describe('TargetNotes', () => {
  beforeEach(() => {
    useAuth.setState({
      user: { id: 5, role: 'reader' } as never,
    });
  });
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null });
  });

  it('у главы показывает только её заметки', async () => {
    setup({ chapterId: 10 });
    await userEvent.click(screen.getByRole('button', { name: /Мои заметки к главе \(1\)/ }));
    expect(screen.getByText('к главе 10')).toBeInTheDocument();
    expect(screen.queryByText('к главе 11')).toBeNull();
    expect(screen.queryByText('к тому')).toBeNull();
  });

  it('у тома показывает только заметки к тому', async () => {
    setup({});
    await userEvent.click(screen.getByRole('button', { name: /Мои заметки к тому \(1\)/ }));
    expect(screen.getByText('к тому')).toBeInTheDocument();
    expect(screen.queryByText('к главе 10')).toBeNull();
    expect(screen.queryByText('к цитате')).toBeNull();
  });

  it('без заметок — одна кнопка, без заголовка', () => {
    setup({ chapterId: 99 });
    expect(screen.getByRole('button', { name: 'Заметка к главе' })).toBeInTheDocument();
    expect(screen.queryByText(/Мои заметки/)).toBeNull();
  });

  it('создание у главы шлёт target chapter с chapter_id', async () => {
    const created = note({ id: 50, chapter_id: 99, body: 'новая' });
    const spy = vi.spyOn(marginNotesApi, 'create').mockResolvedValue({ data: created } as never);
    const h = setup({ chapterId: 99 });
    await userEvent.click(screen.getByRole('button', { name: 'Заметка к главе' }));
    await userEvent.type(screen.getByLabelText('Текст заметки'), 'новая');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(h.onCreated).toHaveBeenCalledWith(created));
    expect(spy).toHaveBeenCalledWith(7, { target: 'chapter', chapter_id: 99, body: 'новая' });
  });

  it('создание у тома шлёт target work', async () => {
    const created = note({ id: 51, target: 'work', chapter_id: null, body: 'том' });
    const spy = vi.spyOn(marginNotesApi, 'create').mockResolvedValue({ data: created } as never);
    const h = setup({ notes: [] });
    await userEvent.click(screen.getByRole('button', { name: 'Заметка к тому' }));
    await userEvent.type(screen.getByLabelText('Текст заметки'), 'том');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(h.onCreated).toHaveBeenCalledWith(created));
    expect(spy).toHaveBeenCalledWith(7, { target: 'work', body: 'том' });
  });

  it('правка зовёт update и onChange', async () => {
    const updated = note({ id: 1, body: 'иначе' });
    const spy = vi.spyOn(marginNotesApi, 'update').mockResolvedValue({ data: updated } as never);
    const h = setup({ chapterId: 10 });
    await userEvent.click(screen.getByRole('button', { name: /Мои заметки к главе/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }));
    const field = screen.getByLabelText('Текст заметки');
    await userEvent.clear(field);
    await userEvent.type(field, 'иначе');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(h.onChange).toHaveBeenCalledWith(updated));
    expect(spy).toHaveBeenCalledWith(1, 'иначе');
  });

  it('удаление зовёт remove и onDelete', async () => {
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const spy = vi.spyOn(marginNotesApi, 'remove').mockResolvedValue({} as never);
    const h = setup({ chapterId: 10 });
    await userEvent.click(screen.getByRole('button', { name: /Мои заметки к главе/ }));
    await userEvent.click(screen.getByRole('button', { name: 'Изменить' }));
    await userEvent.click(screen.getByRole('button', { name: 'Удалить' }));
    await waitFor(() => expect(h.onDelete).toHaveBeenCalledWith(1));
    expect(spy).toHaveBeenCalledWith(1);
  });

  it('аноним: ничего не рисуется', () => {
    useAuth.setState({ user: null });
    const { container } = render(
      <MemoryRouter>
        <TargetNotes workId={7} notes={NOTES} {...handlers()} />
      </MemoryRouter>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
