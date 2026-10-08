import { describe, it, expect, vi, afterEach } from 'vitest';
import { act, renderHook, waitFor } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { conceptsApi } from '../services/api';
import type { ConceptEntry, ConceptEntriesPage } from '../types';
import { encodeRubricPath } from '../utils/rubricPathParam';
import { useConceptFragments } from './useConceptFragments';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function entry(referenceId: number, printedStart = 730): ConceptEntry {
  return {
    reference_id: referenceId,
    volume_number: 12,
    printed_start: printedStart,
    printed_end: printedStart,
    work_id: 14,
    work_title: 'Экономические рукописи',
    chapter_title: 'Введение',
    chapter_id: null,
    rubric: 'определение',
    rubric_path: ['определение'],
    is_uncertain: false,
    state: 'fragment',
    pages: [
      {
        page_id: 9000 + printedStart,
        page_number: printedStart,
        printed_page: printedStart,
        page_status: 'не_вычитана',
      },
    ],
    cuts: [
      {
        id: 91,
        bounds: {
          start_page_id: 9000 + printedStart,
          start_offset: 0,
          end_page_id: 9000 + printedStart,
          end_offset: 20,
        },
        status: 'machine',
        head_quote: 'текст',
        parts: [
          {
            page_id: 9000 + printedStart,
            page_number: printedStart,
            printed_page: printedStart,
            page_status: 'не_вычитана',
            html: '<p>текст</p>',
          },
        ],
      },
    ],
    stale_cuts: [],
  };
}

function page(entries: ConceptEntry[], total: number): ConceptEntriesPage {
  return { total, entries };
}

describe('useConceptFragments', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('путь подрубрики уезжает в запрос закодированным и участвует в ключе потока', async () => {
    // Массив в зависимостях эффекта менял бы ссылку на каждый рендер — путь
    // обязан попадать в ключ СТРОКОЙ, иначе поток перезапрашивался бы без
    // конца. Проверяется тем, что второй рендер с ТЕМ ЖЕ путём не даёт второго
    // запроса.
    const fragments = vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok(page([], 0)));
    const { rerender } = renderHook(
      ({ path }: { path: string[] }) => useConceptFragments('x', '', path, '', 'rubric'),
      { initialProps: { path: ['II съезд РСДРП', 'значение съезда'] } },
    );

    await waitFor(() => expect(fragments).toHaveBeenCalledTimes(1));
    expect(fragments.mock.calls[0][1]).toMatchObject({
      rubric_path: encodeRubricPath(['II съезд РСДРП', 'значение съезда']),
    });

    rerender({ path: ['II съезд РСДРП', 'значение съезда'] });
    await waitFor(() => expect(fragments).toHaveBeenCalledTimes(1));

    rerender({ path: ['III съезд РСДРП'] });
    await waitFor(() => expect(fragments).toHaveBeenCalledTimes(2));
  });

  it('пустой путь в запрос не пишется', async () => {
    const fragments = vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok(page([], 0)));
    renderHook(() => useConceptFragments('x', '', [], '', 'rubric'));

    await waitFor(() => expect(fragments).toHaveBeenCalledTimes(1));
    expect(fragments.mock.calls[0][1]).not.toHaveProperty('rubric_path');
  });

  it('смена пути после прокрутки сбрасывает поток, а не дописывает чужие записи в хвост', async () => {
    // Ровно та работа, для которой key и существует. Без пути в ключе
    // состояние не сбрасывается: эффект уходит за новой подрубрикой со
    // СТАРЫМ офсетом, ответ проходит проверку `prev.key !== key` и
    // дописывается к записям прежнего съезда — читатель получает смесь
    // двух. На первой порции (offset 0) дефект невидим, потому тест обязан
    // сперва прокрутить.
    const twenty = Array.from({ length: 20 }, (_, i) => entry(500 + i, 730 + i));
    const twentyMore = Array.from({ length: 20 }, (_, i) => entry(600 + i, 900 + i));
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation((_slug, params) =>
        ok(page(params.offset === 0 ? twenty : twentyMore, 40)),
      );

    const { result, rerender } = renderHook(
      ({ path }: { path: string[] }) => useConceptFragments('x', '', path, '', 'rubric'),
      { initialProps: { path: ['II съезд РСДРП'] } },
    );
    await waitFor(() => expect(result.current.entries).toHaveLength(20));

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.entries).toHaveLength(40));

    rerender({ path: ['III съезд РСДРП'] });
    await waitFor(() => expect(result.current.entries).toHaveLength(20));

    // Обязательная проверка: без неё тест зелен и при сбросе в пустоту без
    // нового запроса (offset не вернулся к 0, rubric_path не сменился).
    const last = fragments.mock.calls[fragments.mock.calls.length - 1];
    expect(last[1]).toMatchObject({
      offset: 0,
      rubric_path: encodeRubricPath(['III съезд РСДРП']),
    });
  });

  it('грузит первую порцию', async () => {
    // total = 25 — больше порции (20), чтобы hasMore честно отражал, что
    // слоты ещё остались (см. арифметику ниже: offset + BATCH < total).
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok(page([entry(473)], 25)));

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(spy).toHaveBeenCalledWith('abstraktnyj-trud', { limit: 20, offset: 0 });
    expect(result.current.entries).toHaveLength(1);
    expect(result.current.total).toBe(25);
    expect(result.current.hasMore).toBe(true);
  });

  it('дописывает следующую порцию по смещению в слотах, не теряя загруженное', async () => {
    // total = 21 — ровно пример из разбора: после первой порции (offset 0)
    // остаётся один неспрошенный слот, loadMore обязан попросить offset 20,
    // а не количество фактически пришедших записей.
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation((_slug, params) =>
        ok(page([entry(params.offset === 0 ? 473 : 474, params.offset === 0 ? 730 : 731)], 21)),
      );

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.hasMore).toBe(true);

    act(() => result.current.loadMore());

    await waitFor(() => expect(result.current.entries).toHaveLength(2));
    expect(spy).toHaveBeenLastCalledWith('abstraktnyj-trud', { limit: 20, offset: 20 });
    expect(result.current.hasMore).toBe(false);
  });

  it('офсет считается в слотах: неполная порция (пропуск страницы) не сдвигает следующий запрос', async () => {
    // Сервер вернул 19 записей из 20 слотов первой порции — например, одному
    // из адресов не хватило страниц в базе. Следующий запрос всё равно
    // должен уйти с offset 20 (число спрошенных слотов), а не 19 (число
    // пришедших записей) — иначе 20-й слот переспросится и запись покажется
    // дважды.
    const nineteen = Array.from({ length: 19 }, (_, i) => entry(500 + i, 730 + i));
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation((_slug, params) =>
        ok(page(params.offset === 0 ? nineteen : [entry(600, 800)], 41)),
      );

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.entries).toHaveLength(19);

    act(() => result.current.loadMore());

    await waitFor(() => expect(result.current.entries).toHaveLength(20));
    expect(spy).toHaveBeenLastCalledWith('abstraktnyj-trud', { limit: 20, offset: 20 });
  });

  it('хвостовой пропуск не подвешивает «загружаем ещё»: слоты кончились, а записей пришло меньше total', async () => {
    // Последний слот тоже оказался пропуском (адрес пропал из базы) — записей
    // пришло меньше total навсегда, но слоты кончились, и hasMore обязан
    // стать false, а не зависнуть в true (см. находку 3).
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation((_slug, params) => ok(page(params.offset === 0 ? [entry(473)] : [], 21)));

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(result.current.hasMore).toBe(true);

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.isLoadingMore).toBe(false));

    expect(result.current.entries).toHaveLength(1);
    expect(result.current.total).toBe(21);
    expect(result.current.hasMore).toBe(false);
    expect(spy).toHaveBeenCalledTimes(2);
  });

  it('пропуск записи не сдвигает пагинацию', async () => {
    // Сервер считает total по слотам-адресам: адрес, чьи страницы исчезли,
    // в entries не приезжает, но в total сидит. hasMore обязан считаться по
    // запрошенным слотам, иначе поток либо задвоит записи, либо оборвётся.
    const spy = vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok(page([], 40)));
    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));

    expect(result.current.entries).toHaveLength(0);
    expect(result.current.hasMore).toBe(true);
    expect(spy).toHaveBeenCalledWith('abstraktnyj-trud', { limit: 20, offset: 0 });
  });

  it('передаёт фильтры и сбрасывает поток при их смене', async () => {
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok(page([entry(473, 745)], 1)));

    const { result, rerender } = renderHook(
      ({ rubric, volume }: { rubric: string; volume: string }) =>
        useConceptFragments('abstraktnyj-trud', rubric, [], volume, 'rubric'),
      { initialProps: { rubric: '', volume: '' } },
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    rerender({ rubric: 'его мера', volume: '25-II' });

    // Сброс виден сразу: прошлая порция не должна висеть под новым фильтром.
    expect(result.current.entries).toEqual([]);
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(spy).toHaveBeenLastCalledWith('abstraktnyj-trud', {
      rubric: 'его мера',
      volume: 25,
      volume_part: 'II',
      limit: 20,
      offset: 0,
    });
  });

  it('ошибка не стирает загруженное и снимается повтором', async () => {
    // total = 25 (не 4): при total <= BATCH слотов для loadMore уже не
    // остаётся, и вызов был бы молча проигнорирован гвардом hasMore.
    let calls = 0;
    const spy = vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => {
      calls += 1;
      if (calls === 2) return Promise.reject(new Error('сеть'));
      return ok(page([entry(473)], 25));
    });

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));

    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.error).not.toBe(''));
    expect(result.current.entries).toHaveLength(1);

    act(() => result.current.retry());
    await waitFor(() => expect(result.current.error).toBe(''));
    expect(spy).toHaveBeenCalledTimes(3);
  });

  it('порядок по умолчанию в запрос не пишется', async () => {
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok(page([entry(473)], 1)));

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(spy).toHaveBeenCalledWith('abstraktnyj-trud', { limit: 20, offset: 0 });
  });

  it('порядок по томам уезжает в запрос', async () => {
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok(page([entry(473)], 1)));

    const { result } = renderHook(() =>
      useConceptFragments('abstraktnyj-trud', '', [], '', 'page'),
    );

    await waitFor(() => expect(result.current.isLoading).toBe(false));
    expect(spy).toHaveBeenCalledWith('abstraktnyj-trud', { order: 'page', limit: 20, offset: 0 });
  });

  it('смена порядка сбрасывает поток на первую порцию', async () => {
    // total = 25: после первой порции остаются неспрошенные слоты, и офсет
    // сдвинут. Смена порядка обязана вернуть его к нулю — записи в новом
    // порядке стоят на других местах, и дописывать к прежнему хвосту нечего.
    const spy = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok(page([entry(473)], 25)));

    const { result, rerender } = renderHook(
      ({ order }: { order: 'rubric' | 'page' }) =>
        useConceptFragments('abstraktnyj-trud', '', [], '', order),
      // Явный union, а не `as const`: последний сузил бы Props до литерала
      // 'rubric', и rerender с 'page' не проходил бы проверку типов.
      { initialProps: { order: 'rubric' as 'rubric' | 'page' } },
    );
    await waitFor(() => expect(result.current.isLoading).toBe(false));
    act(() => result.current.loadMore());
    await waitFor(() => expect(result.current.entries).toHaveLength(2));

    rerender({ order: 'page' });

    await waitFor(() => expect(result.current.entries).toHaveLength(1));
    expect(spy).toHaveBeenLastCalledWith('abstraktnyj-trud', {
      order: 'page',
      limit: 20,
      offset: 0,
    });
  });

  it('без slug в API не ходит', () => {
    const spy = vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok(page([], 0)));

    const { result } = renderHook(() => useConceptFragments(undefined, '', [], '', 'rubric'));

    expect(spy).not.toHaveBeenCalled();
    expect(result.current.entries).toEqual([]);
    expect(result.current.isLoading).toBe(false);
  });

  describe('replaceEntry', () => {
    it('заменяет запись на месте, не меняя порядок и не трогая соседей', async () => {
      const spy = vi
        .spyOn(conceptsApi, 'fragments')
        .mockImplementationOnce(() => ok(page([entry(473), entry(474, 731)], 25)));

      const { result } = renderHook(() =>
        useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
      );
      await waitFor(() => expect(result.current.isLoading).toBe(false));
      expect(result.current.entries).toHaveLength(2);

      // Отличается от исходной ровно тем, что должна была принести
      // подстановка — так тест ловит и «не подставили», и «подставили не то».
      const updated: ConceptEntry = { ...entry(473), state: 'stale' };
      spy.mockImplementationOnce(() => ok(page([updated], 1)));

      await act(async () => {
        await result.current.replaceEntry(473);
      });

      expect(spy).toHaveBeenLastCalledWith('abstraktnyj-trud', { reference_id: 473, limit: 1 });
      // Позиция и сосед не тронуты: замена на месте, а не пересборка списка.
      expect(result.current.entries).toHaveLength(2);
      expect(result.current.entries[0]).toEqual(updated);
      expect(result.current.entries[1]).toEqual(entry(474, 731));
      // Пагинация не сдвинута: точечная замена — не перезагрузка порции.
      expect(result.current.total).toBe(25);
    });

    it('пустой ответ (адрес перестал подходить под фильтр) ничего не меняет', async () => {
      const spy = vi
        .spyOn(conceptsApi, 'fragments')
        .mockImplementationOnce(() => ok(page([entry(473)], 25)));

      const { result } = renderHook(() =>
        useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
      );
      await waitFor(() => expect(result.current.isLoading).toBe(false));

      spy.mockImplementationOnce(() => ok(page([], 0)));

      await act(async () => {
        await result.current.replaceEntry(473);
      });

      expect(result.current.entries).toHaveLength(1);
      expect(result.current.entries[0]).toEqual(entry(473));
    });

    it('сбой запроса не портит уже загруженное и не поднимает ошибку потока', async () => {
      const spy = vi
        .spyOn(conceptsApi, 'fragments')
        .mockImplementationOnce(() => ok(page([entry(473)], 25)));

      const { result } = renderHook(() =>
        useConceptFragments('abstraktnyj-trud', '', [], '', 'rubric'),
      );
      await waitFor(() => expect(result.current.isLoading).toBe(false));

      spy.mockImplementationOnce(() => Promise.reject(new Error('сеть')));

      await act(async () => {
        await result.current.replaceEntry(473);
      });

      expect(result.current.entries).toHaveLength(1);
      expect(result.current.entries[0]).toEqual(entry(473));
      expect(result.current.error).toBe('');
    });
  });
});
