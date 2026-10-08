import { describe, expect, it, vi, beforeEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';

import { useReadingStream } from './useReadingStream';
import { readingApi } from '../services/api';
import type { ReadingWindow } from '../types';

vi.mock('../services/api', () => ({
  readingApi: { window: vi.fn() },
}));

const mockedWindow = vi.mocked(readingApi.window);

function windowOf(from: number, count: number, nextFrom: number | null): ReadingWindow {
  return {
    pages: Array.from({ length: count }, (_, i) => ({
      page_number: from + i,
      html: `<p>Страница ${from + i}</p>`,
      notes_html: '',
      blank: false,
    })),
    next_from: nextFrom,
    total_pages: 784,
  };
}

function resolveWith(win: ReadingWindow) {
  return Promise.resolve({ data: win } as Awaited<ReturnType<typeof readingApi.window>>);
}

beforeEach(() => {
  mockedWindow.mockReset();
});

describe('useReadingStream', () => {
  it('грузит первое окно от стартовой страницы', async () => {
    mockedWindow.mockReturnValue(resolveWith(windowOf(43, 10, 53)));

    const { result } = renderHook(() => useReadingStream(47, 43));

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(mockedWindow).toHaveBeenCalledWith(47, 43);
    expect(result.current.windows).toHaveLength(1);
    expect(result.current.windows[0].pages[0].page_number).toBe(43);
    expect(result.current.totalPages).toBe(784);
    expect(result.current.hasMore).toBe(true);
  });

  it('догрузка добавляет окно вниз, не трогая прежние', async () => {
    mockedWindow.mockReturnValueOnce(resolveWith(windowOf(43, 10, 53)));
    const { result } = renderHook(() => useReadingStream(47, 43));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    mockedWindow.mockReturnValueOnce(resolveWith(windowOf(53, 10, 63)));
    act(() => result.current.loadMore());

    await waitFor(() => expect(result.current.windows).toHaveLength(2));
    expect(result.current.windows[0].pages[0].page_number).toBe(43);
    expect(result.current.windows[1].pages[0].page_number).toBe(53);
    expect(mockedWindow).toHaveBeenLastCalledWith(47, 53);
  });

  it('не пускает второй запрос, пока летит первый', async () => {
    mockedWindow.mockReturnValueOnce(resolveWith(windowOf(1, 10, 11)));
    const { result } = renderHook(() => useReadingStream(47, 1));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    mockedWindow.mockReturnValue(new Promise(() => {})); // не разрешается
    act(() => result.current.loadMore());
    act(() => result.current.loadMore());
    act(() => result.current.loadMore());

    // Один вызов на первое окно и ровно один на догрузку.
    expect(mockedWindow).toHaveBeenCalledTimes(2);
  });

  it('останавливается, когда next_from пуст', async () => {
    mockedWindow.mockReturnValue(resolveWith(windowOf(775, 10, null)));
    const { result } = renderHook(() => useReadingStream(47, 775));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.hasMore).toBe(false);

    act(() => result.current.loadMore());
    expect(mockedWindow).toHaveBeenCalledTimes(1);
  });

  it('ошибка не роняет накопленное, повтор запрашивает то же окно', async () => {
    mockedWindow.mockReturnValueOnce(resolveWith(windowOf(43, 10, 53)));
    const { result } = renderHook(() => useReadingStream(47, 43));
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    mockedWindow.mockReturnValueOnce(Promise.reject(new Error('сеть')));
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.error).not.toBe(''));

    // Прежнее окно на месте.
    expect(result.current.windows).toHaveLength(1);

    mockedWindow.mockReturnValueOnce(resolveWith(windowOf(53, 10, 63)));
    act(() => result.current.retry());
    await waitFor(() => expect(result.current.windows).toHaveLength(2));
    expect(result.current.error).toBe('');
    expect(mockedWindow).toHaveBeenLastCalledWith(47, 53);
  });

  it('смена работы или стартовой страницы обнуляет поток', async () => {
    mockedWindow.mockReturnValue(resolveWith(windowOf(43, 10, 53)));
    const { result, rerender } = renderHook(
      ({ page }: { page: number }) => useReadingStream(47, page),
      { initialProps: { page: 43 } },
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    mockedWindow.mockReturnValue(resolveWith(windowOf(200, 10, 210)));
    rerender({ page: 200 });

    await waitFor(() => expect(result.current.windows[0].pages[0].page_number).toBe(200));
    expect(result.current.windows).toHaveLength(1);
  });
});
