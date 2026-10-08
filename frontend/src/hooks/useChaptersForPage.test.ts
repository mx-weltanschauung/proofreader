import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { chaptersApi } from '../services/api';
import type { Chapter } from '../types';
import { chapterLevelsForPage, useChaptersForPage } from './useChaptersForPage';

// Хелпер: тесты читаются по диапазонам и вложенности, остальные поля Chapter
// на выборку не влияют.
function ch(
  id: number,
  title: string,
  startPage: number,
  endPage: number,
  orderNumber: number,
  children: Chapter[] = [],
): Chapter {
  return {
    id,
    work_id: 4,
    parent_id: null,
    title,
    type: 'chapter',
    order_number: orderNumber,
    start_page: startPage,
    end_page: endPage,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    children,
  };
}

// Фрагмент реального дерева работы 4.
const MANIFEST = ch(260, 'Манифест коммунистической партии', 419, 459, 41, [
  ch(290, 'I. БУРЖУА И ПРОЛЕТАРИИ', 424, 437, 88),
  ch(291, 'II. ПРОЛЕТАРИИ И КОММУНИСТЫ', 437, 447, 89),
]);

// Стр. 133 работы 4: два «Замечания» с одинаковым диапазоном 133-133 —
// стыковая страница, где одно кончается и начинается другое.
const NISHCHETA = ch(230, 'Нищета философии', 65, 185, 20, [
  ch(283, 'Глава вторая. МЕТАФИЗИКА ПОЛИТИЧЕСКОЙ ЭКОНОМИИ', 128, 185, 60, [
    ch(309, '§ I. Метод', 128, 146, 70, [
      ch(321, 'Замечание третье', 133, 133, 82),
      ch(320, 'Замечание второе', 133, 133, 81),
    ]),
  ]),
]);

describe('chapterLevelsForPage', () => {
  it('строит цепочку по вложенности', () => {
    const levels = chapterLevelsForPage([MANIFEST], 446);

    expect(levels.map((level) => level.map((c) => c.title))).toEqual([
      ['Манифест коммунистической партии'],
      ['II. ПРОЛЕТАРИИ И КОММУНИСТЫ'],
    ]);
  });

  it('кладёт сестринские главы стыковой страницы в один уровень по order_number', () => {
    const levels = chapterLevelsForPage([NISHCHETA], 133);

    expect(levels.map((level) => level.map((c) => c.title))).toEqual([
      ['Нищета философии'],
      ['Глава вторая. МЕТАФИЗИКА ПОЛИТИЧЕСКОЙ ЭКОНОМИИ'],
      ['§ I. Метод'],
      ['Замечание второе', 'Замечание третье'],
    ]);
  });

  it('возвращает пусто для страницы вне всех диапазонов', () => {
    expect(chapterLevelsForPage([MANIFEST, NISHCHETA], 500)).toEqual([]);
  });

  it('считает границы диапазона включительно', () => {
    expect(chapterLevelsForPage([MANIFEST], 419)).toHaveLength(1);
    expect(chapterLevelsForPage([MANIFEST], 459)).toHaveLength(1);
    expect(chapterLevelsForPage([MANIFEST], 418)).toEqual([]);
    expect(chapterLevelsForPage([MANIFEST], 460)).toEqual([]);
  });

  it('находит вложенную главу, даже если родитель по диапазону не совпал', () => {
    // Битая разметка: диапазон родителя не покрывает потомка. Обход не должен
    // обрываться, иначе глава исчезнет молча.
    const broken = ch(1, 'Родитель с битым диапазоном', 1, 2, 1, [ch(2, 'Потомок', 100, 110, 1)]);

    expect(chapterLevelsForPage([broken], 105).map((level) => level.map((c) => c.title))).toEqual([
      ['Потомок'],
    ]);
  });

  it('не падает на дереве без children', () => {
    const flat = ch(7, 'Без потомков', 1, 10, 1);
    delete (flat as Partial<Chapter>).children;

    expect(chapterLevelsForPage([flat], 5).map((level) => level.map((c) => c.title))).toEqual([
      ['Без потомков'],
    ]);
  });
});

// Ответы axios несут status/headers/config; хук читает только .data.
function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

describe('useChaptersForPage', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('отдаёт уровни глав для страницы', async () => {
    const list = vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([MANIFEST]));

    const { result } = renderHook(() => useChaptersForPage('4', 446));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(list).toHaveBeenCalledWith(4);
    expect(result.current.levels.map((level) => level.map((c) => c.title))).toEqual([
      ['Манифест коммунистической партии'],
      ['II. ПРОЛЕТАРИИ И КОММУНИСТЫ'],
    ]);
  });

  it('на первом рендере валидного запроса уже загружается', () => {
    // Контракт: isLoading истинен сразу, до ответа API, а не после первого
    // эффекта. Потребитель вправе показать заглушку с первого кадра.
    vi.spyOn(chaptersApi, 'list').mockImplementation(
      () => new Promise<AxiosResponse<Chapter[]>>(() => {}),
    );

    const { result } = renderHook(() => useChaptersForPage('4', 446));

    expect(result.current.isLoading).toBe(true);
    expect(result.current.levels).toEqual([]);
  });

  it('на отказе API отдаёт пустые уровни и не бросает', async () => {
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => Promise.reject(new Error('boom')));
    const consoleError = vi.spyOn(console, 'error').mockImplementation(() => {});

    const { result } = renderHook(() => useChaptersForPage('4', 446));

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.levels).toEqual([]);
    expect(consoleError).toHaveBeenCalled();
  });

  it('не ходит в API без номера страницы', () => {
    const list = vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([MANIFEST]));

    const { result } = renderHook(() => useChaptersForPage('4', undefined));

    expect(list).not.toHaveBeenCalled();
    expect(result.current.levels).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });

  it('не ходит в API при невалидном workId', () => {
    const list = vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([MANIFEST]));

    const { result } = renderHook(() => useChaptersForPage('не-число', 446));

    expect(list).not.toHaveBeenCalled();
    expect(result.current.levels).toEqual([]);
  });

  it('отбрасывает ответ по старому номеру страницы', async () => {
    // Ответ по 446 приезжает уже после перехода на 133 — записаться он не должен.
    let resolve446: (value: AxiosResponse<Chapter[]>) => void = () => {};
    const pending = new Promise<AxiosResponse<Chapter[]>>((res) => {
      resolve446 = res;
    });

    // Первый вызов зависает, все последующие отвечают деревом для стр. 133.
    vi.spyOn(chaptersApi, 'list')
      .mockImplementationOnce(() => pending)
      .mockImplementation(() => ok([NISHCHETA]));

    const { result, rerender } = renderHook(
      ({ page }: { page: number }) => useChaptersForPage('4', page),
      { initialProps: { page: 446 } },
    );

    rerender({ page: 133 });

    resolve446({ data: [MANIFEST] } as unknown as AxiosResponse<Chapter[]>);

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.levels.map((level) => level.map((c) => c.title))).toEqual([
      ['Нищета философии'],
      ['Глава вторая. МЕТАФИЗИКА ПОЛИТИЧЕСКОЙ ЭКОНОМИИ'],
      ['§ I. Метод'],
      ['Замечание второе', 'Замечание третье'],
    ]);
  });
});
