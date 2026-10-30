import { act, renderHook, waitFor } from '@testing-library/react';
import { afterEach, describe, expect, it, vi } from 'vitest';
import type { AxiosResponse } from 'axios';
import { useAuth } from './useAuth';
import { useReaderComments } from './useReaderComments';
import { readerCommentsApi } from '../services/api';
import { ReadingPreferencesContext } from '../contexts/readingPrefsContext';
import { DEFAULT_PREFS } from '../contexts/readingPrefs';
import type { ReaderComment } from '../types';

function HiddenComments({ children }: { children: React.ReactNode }) {
  return (
    <ReadingPreferencesContext.Provider
      value={{
        prefs: { ...DEFAULT_PREFS, readerComments: false },
        setPref: vi.fn(),
        reset: vi.fn(),
      }}
    >
      {children}
    </ReadingPreferencesContext.Provider>
  );
}

afterEach(() => {
  vi.restoreAllMocks();
  useAuth.setState({ user: null, isAuthenticated: false });
});
describe('useReaderComments', () => {
  it('запрашивает опубликованные комментарии у анонима по умолчанию', async () => {
    useAuth.setState({ user: null });
    const request = vi
      .spyOn(readerCommentsApi, 'ofWork')
      .mockResolvedValue({ data: [{ id: 1, body: 'Опубликованный комментарий' }] } as AxiosResponse<
        ReaderComment[]
      >);
    const { result } = renderHook(() => useReaderComments(7));
    await waitFor(() => expect(result.current.comments).toHaveLength(1));
    expect(request).toHaveBeenCalledWith(7, true, 0);
  });
  it('не запрашивает комментарии у анонима, явно выключившего их показ', () => {
    useAuth.setState({ user: null });
    const request = vi.spyOn(readerCommentsApi, 'ofWork');
    const { result } = renderHook(() => useReaderComments(7), { wrapper: HiddenComments });
    expect(result.current.comments).toEqual([]);
    expect(request).not.toHaveBeenCalled();
  });
  it('запрашивает только свои и скрывает их сразу при выходе', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    const request = vi
      .spyOn(readerCommentsApi, 'ofWork')
      .mockResolvedValue({ data: [{ id: 1, body: 'личный черновик' }] } as AxiosResponse<
        ReaderComment[]
      >);
    const { result } = renderHook(() => useReaderComments(7), { wrapper: HiddenComments });
    await waitFor(() => expect(result.current.comments).toHaveLength(1));
    expect(request).toHaveBeenCalledWith(7, false, 0);
    act(() => useAuth.setState({ user: null }));
    expect(result.current.comments).toEqual([]);
  });
  it('включение настройки разрешает публичный запрос', async () => {
    useAuth.setState({ user: null });
    const request = vi
      .spyOn(readerCommentsApi, 'ofWork')
      .mockResolvedValue({ data: [] } as unknown as AxiosResponse<ReaderComment[]>);
    renderHook(() => useReaderComments(7), {
      wrapper: ({ children }) => (
        <ReadingPreferencesContext.Provider
          value={{
            prefs: { ...DEFAULT_PREFS, readerComments: true },
            setPref: vi.fn(),
            reset: vi.fn(),
          }}
        >
          {children}
        </ReadingPreferencesContext.Provider>
      ),
    });
    await waitFor(() => expect(request).toHaveBeenCalledWith(7, true, 0));
  });
  it('поздний ответ прежнего тома не появляется в новом', async () => {
    useAuth.setState({ user: { id: 3, role: 'reader' } as never });
    let resolve!: (r: AxiosResponse<ReaderComment[]>) => void;
    vi.spyOn(readerCommentsApi, 'ofWork')
      .mockReturnValueOnce(
        new Promise((r) => {
          resolve = r;
        }),
      )
      .mockResolvedValue({ data: [] } as unknown as AxiosResponse<ReaderComment[]>);
    const { result, rerender } = renderHook(({ work }) => useReaderComments(work), {
      initialProps: { work: 7 },
    });
    rerender({ work: 8 });
    await act(async () =>
      resolve({ data: [{ id: 1, body: 'старое' }] } as unknown as AxiosResponse<ReaderComment[]>),
    );
    expect(result.current.comments).toEqual([]);
  });
});
