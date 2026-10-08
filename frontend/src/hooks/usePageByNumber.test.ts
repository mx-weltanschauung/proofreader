import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { pagesApi } from '../services/api';
import type { Page } from '../types';
import { usePageByNumber } from './usePageByNumber';

// Ответы axios несут status/headers/config; хук читает только .data.
function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const PAGE = { id: 1882, work_id: 3, page_number: 245 } as Page;

describe('usePageByNumber', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('резолвит номер страницы в саму страницу', async () => {
    const get = vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() => ok(PAGE));

    const { result } = renderHook(() => usePageByNumber('3', '245'));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(get).toHaveBeenCalledWith(3, 245);
    expect(result.current.page).toEqual(PAGE);
    expect(result.current.notFound).toBe(false);
    expect(result.current.error).toBe('');
  });

  it('сообщает notFound на 404', async () => {
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() =>
      Promise.reject({ response: { status: 404 } }),
    );

    const { result } = renderHook(() => usePageByNumber('3', '9999'));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.notFound).toBe(true);
    expect(result.current.page).toBeNull();
  });

  // Нечисловой параметр URL неотличим для пользователя от несуществующего
  // номера, но сети не стоит: запрос не уходит вовсе.
  it('сообщает notFound на нечисловом номере, не ходя в API', async () => {
    const get = vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() => ok(PAGE));

    const { result } = renderHook(() => usePageByNumber('3', 'abc'));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.notFound).toBe(true);
    expect(get).not.toHaveBeenCalled();
  });

  it('прочие ошибки отдаёт через error, а не notFound', async () => {
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() =>
      Promise.reject({ response: { status: 500, data: { message: 'boom' } } }),
    );

    const { result } = renderHook(() => usePageByNumber('3', '245'));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.notFound).toBe(false);
    expect(result.current.error).toBe('boom');
  });
});
