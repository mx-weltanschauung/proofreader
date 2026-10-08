import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { conceptsApi } from '../services/api';
import type { ConceptBacklink } from '../types';
import { usePageConcepts } from './usePageConcepts';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const BACKLINKS: ConceptBacklink[] = [
  {
    concept_id: 1,
    slug: 'abstrakciya',
    title: 'Абстракция',
    rubric: 'и действительность',
    page_start: 85,
    page_end: 85,
  },
];

describe('usePageConcepts', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('отдаёт обратные ссылки страницы', async () => {
    const forPage = vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(BACKLINKS));

    const { result } = renderHook(() => usePageConcepts(4, 1001));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(forPage).toHaveBeenCalledWith(4, 1001);
    expect(result.current.backlinks).toHaveLength(1);
    expect(result.current.failed).toBe(false);
  });

  it('не ходит в API без id страницы', () => {
    const forPage = vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(BACKLINKS));

    const { result } = renderHook(() => usePageConcepts(4, undefined));

    expect(forPage).not.toHaveBeenCalled();
    expect(result.current.backlinks).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });

  it('переживает null вместо пустого списка', async () => {
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok(null));

    const { result } = renderHook(() => usePageConcepts(4, 1001));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.backlinks).toEqual([]);
  });

  it('на отказе API поднимает failed, а не молчит', async () => {
    // Пустой список и сбой выглядят одинаково — пустой секцией. Разница
    // должна доезжать до компонента, иначе поломка неотличима от нормы.
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => Promise.reject(new Error('boom')));

    const { result } = renderHook(() => usePageConcepts(4, 1001));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.failed).toBe(true);
    expect(result.current.backlinks).toEqual([]);
  });

  it('отбрасывает ответ по покинутой странице', async () => {
    let resolveFirst: (value: AxiosResponse<ConceptBacklink[] | null>) => void = () => {};
    const pending = new Promise<AxiosResponse<ConceptBacklink[] | null>>((res) => {
      resolveFirst = res;
    });
    vi.spyOn(conceptsApi, 'forPage')
      .mockImplementationOnce(() => pending)
      .mockImplementation(() => ok([]));

    const { result, rerender } = renderHook(
      ({ pageId }: { pageId: number }) => usePageConcepts(4, pageId),
      {
        initialProps: { pageId: 1001 },
      },
    );

    rerender({ pageId: 1002 });
    resolveFirst({ data: BACKLINKS } as unknown as AxiosResponse<ConceptBacklink[] | null>);

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.backlinks).toEqual([]);
  });
});
