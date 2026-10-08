import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { worksApi } from '../services/api';
import { useWorkPageMap, __clearPageMapCache } from './useWorkPageMap';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

describe('useWorkPageMap', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    __clearPageMapCache();
  });

  it('отдаёт номера страниц работы', async () => {
    vi.spyOn(worksApi, 'pageMap').mockImplementation(() =>
      ok([
        { page_number: 1, status: 'не_вычитана' as const },
        { page_number: 2, status: 'вычитана' as const },
      ]),
    );

    const { result } = renderHook(() => useWorkPageMap(41));
    await waitFor(() => expect(result.current.numbers).toEqual([1, 2]));
  });

  // Листание не должно перезапрашивать карту на каждой странице: на самом
  // большом томе это 45 КБ за шаг.
  it('запрашивает карту работы один раз', async () => {
    const spy = vi
      .spyOn(worksApi, 'pageMap')
      .mockImplementation(() => ok([{ page_number: 1, status: 'не_вычитана' as const }]));

    const first = renderHook(() => useWorkPageMap(41));
    await waitFor(() => expect(first.result.current.numbers).toHaveLength(1));
    renderHook(() => useWorkPageMap(41));

    expect(spy).toHaveBeenCalledTimes(1);
  });

  // Без карты страница обязана открываться: пейджер просто не появится.
  it('на ошибке отдаёт пустую карту', async () => {
    vi.spyOn(worksApi, 'pageMap').mockImplementation(() => Promise.reject(new Error('нет')));

    const { result } = renderHook(() => useWorkPageMap(41));
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.numbers).toEqual([]);
  });
});
