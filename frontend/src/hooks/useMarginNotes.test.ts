import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { useMarginNotes } from './useMarginNotes';
import { useAuth } from './useAuth';
import { marginNotesApi } from '../services/api';
import type { MarginNote } from '../types';

function note(id: number): MarginNote {
  return {
    id,
    work_id: 19,
    target: 'quote',
    chapter_id: null,
    chapter_title: '',
    start_page: 5,
    end_page: 5,
    page_missing: false,
    snapshot_start_page: 5,
    quote_key: 'к',
    quote_tail: '',
    quote_text: 'к',
    body: `заметка ${id}`,
    created_at: '2026-10-29T00:00:00Z',
    updated_at: '2026-10-29T00:00:00Z',
  };
}

/** Ответ на список, который отдаётся, когда тест скажет. */
function deferredList() {
  let resolve!: (list: MarginNote[]) => void;
  const promise = new Promise<AxiosResponse<MarginNote[]>>((r) => {
    resolve = (list) => r({ data: list } as AxiosResponse<MarginNote[]>);
  });
  return { promise, resolve };
}

function asReader() {
  useAuth.setState({
    user: { id: 3, role: 'reader' } as never,
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });
}

describe('useMarginNotes', () => {
  it('сразу показывает возвращённую из комментария заметку только своей сессии', async () => {
    asReader();
    vi.spyOn(marginNotesApi, 'ofWork').mockResolvedValue({ data: [] } as unknown as AxiosResponse<
      MarginNote[]
    >);
    const { result } = renderHook(() => useMarginNotes(19));
    await act(async () => {});
    await act(async () => {
      window.dispatchEvent(
        new CustomEvent('margin-note-created', { detail: { note: note(77), userId: 3 } }),
      );
    });
    expect(result.current.notes.map((n) => n.id)).toEqual([77]);
    await act(async () => {
      window.dispatchEvent(
        new CustomEvent('margin-note-created', { detail: { note: note(78), userId: 4 } }),
      );
    });
    expect(result.current.notes.map((n) => n.id)).toEqual([77]);
  });
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('аноним — пусто и без запроса', () => {
    const ofWork = vi.spyOn(marginNotesApi, 'ofWork');
    const { result } = renderHook(() => useMarginNotes(19));
    expect(result.current.notes).toEqual([]);
    expect(ofWork).not.toHaveBeenCalled();
  });

  it('null — без запроса и у вошедшего', () => {
    asReader();
    const ofWork = vi.spyOn(marginNotesApi, 'ofWork');
    renderHook(() => useMarginNotes(null));
    expect(ofWork).not.toHaveBeenCalled();
  });

  it('заметка, записанная до ответа на список, не теряется', async () => {
    asReader();
    const list = deferredList();
    vi.spyOn(marginNotesApi, 'ofWork').mockReturnValue(list.promise);
    const { result } = renderHook(() => useMarginNotes(19));

    act(() => result.current.add(note(9)));
    expect(result.current.notes.map((n) => n.id)).toEqual([9]);

    await act(async () => list.resolve([note(1), note(2)]));
    await waitFor(() => expect(result.current.notes.map((n) => n.id)).toEqual([1, 2, 9]));
  });

  it('заметка, снятая до ответа на список, не возвращается из ответа', async () => {
    asReader();
    const list = deferredList();
    vi.spyOn(marginNotesApi, 'ofWork').mockReturnValue(list.promise);
    const { result } = renderHook(() => useMarginNotes(19));

    act(() => result.current.drop(2));
    await act(async () => list.resolve([note(1), note(2)]));
    await waitFor(() => expect(result.current.notes.map((n) => n.id)).toEqual([1]));
  });

  it('после ответа правки идут поверх списка', async () => {
    asReader();
    vi.spyOn(marginNotesApi, 'ofWork').mockResolvedValue({
      data: [note(1)],
    } as AxiosResponse<MarginNote[]>);
    const { result } = renderHook(() => useMarginNotes(19));
    await waitFor(() => expect(result.current.notes).toHaveLength(1));

    act(() => result.current.add(note(4)));
    act(() => result.current.replace({ ...note(1), body: 'новая' }));
    act(() => result.current.drop(4));
    expect(result.current.notes).toEqual([{ ...note(1), body: 'новая' }]);
  });
});
