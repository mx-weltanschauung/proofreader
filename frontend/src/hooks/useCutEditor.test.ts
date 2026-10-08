import { describe, it, expect, vi, beforeEach } from 'vitest';
import { renderHook, act, waitFor } from '@testing-library/react';
import { useCutEditor } from './useCutEditor';
import { conceptsApi } from '../services/api';
import type { ConceptCutInput } from '../types';

vi.mock('../services/api', () => ({
  conceptsApi: { putCuts: vi.fn() },
}));

const putCuts = vi.mocked(conceptsApi.putCuts);

function existingCut(over: Partial<ConceptCutInput> = {}): ConceptCutInput {
  return { start_page: 730, start_offset: 0, end_page: 730, end_offset: 10, ...over };
}

describe('useCutEditor', () => {
  beforeEach(() => {
    putCuts.mockReset();
    putCuts.mockResolvedValue({ data: { state: 'fragment', cuts: [] } } as never);
  });

  it('показывает только вырезки текущей страницы', () => {
    const { result } = renderHook(() =>
      useCutEditor('abstraktnyj-trud', 473, 730, [
        existingCut(),
        existingCut({ start_page: 745, end_page: 745 }),
      ]),
    );

    // Чужая вырезка (страница 745) не входит в правимый набор — её нечем
    // показать на развороте страницы 730.
    expect(result.current.cuts).toEqual([{ start: 0, end: 10 }]);
    expect(result.current.foreignCount).toBe(1);
  });

  it('добавляет и удаляет границы', () => {
    const { result } = renderHook(() =>
      useCutEditor('abstraktnyj-trud', 473, 730, [existingCut()]),
    );

    act(() => result.current.addCut({ start: 20, end: 30 }));
    expect(result.current.cuts).toHaveLength(2);

    act(() => result.current.removeCut(0));
    expect(result.current.cuts).toEqual([{ start: 20, end: 30 }]);
  });

  it('сохранение шлёт правимые вырезки как confirmed', async () => {
    const { result } = renderHook(() =>
      useCutEditor('abstraktnyj-trud', 473, 730, [existingCut({ status: 'machine' })]),
    );

    await act(async () => {
      await result.current.save();
    });

    // Человек посмотрел границы этой страницы — они перестают быть
    // машинными, независимо от того, каким статусом пришли.
    expect(putCuts).toHaveBeenCalledWith('abstraktnyj-trud', 473, {
      status: 'confirmed',
      cuts: [
        { start_page: 730, start_offset: 0, end_page: 730, end_offset: 10, status: 'confirmed' },
      ],
    });
  });

  it('не теряет чужую вырезку при сохранении и не трогает её статус', async () => {
    const { result } = renderHook(() =>
      useCutEditor('abstraktnyj-trud', 473, 730, [
        existingCut({ status: 'machine' }),
        existingCut({
          start_page: 745,
          start_offset: 2,
          end_page: 745,
          end_offset: 9,
          status: 'machine',
        }),
      ]),
    );

    await act(async () => {
      await result.current.save();
    });

    const [, , body] = putCuts.mock.calls[0];
    expect(body.cuts).toHaveLength(2);
    // Чужая вырезка уходит как есть, со своим прежним статусом — не
    // confirmed только потому, что человек тронул соседнюю на другой
    // странице.
    expect(body.cuts).toContainEqual({
      start_page: 745,
      start_offset: 2,
      end_page: 745,
      end_offset: 9,
      status: 'machine',
    });
    expect(body.cuts).toContainEqual({
      start_page: 730,
      start_offset: 0,
      end_page: 730,
      end_offset: 10,
      status: 'confirmed',
    });
  });

  it('удаление последней правимой вырезки — законное стирание, чужая всё равно уходит', async () => {
    const { result } = renderHook(() =>
      useCutEditor('abstraktnyj-trud', 473, 730, [
        existingCut(),
        existingCut({ start_page: 745, end_page: 745, status: 'confirmed' }),
      ]),
    );

    act(() => result.current.removeCut(0));
    expect(result.current.cuts).toEqual([]);

    await act(async () => {
      await result.current.save();
    });

    expect(putCuts).toHaveBeenCalledWith('abstraktnyj-trud', 473, {
      status: 'confirmed',
      cuts: [
        { start_page: 745, start_offset: 0, end_page: 745, end_offset: 10, status: 'confirmed' },
      ],
    });
  });

  it('ошибка сохранения не стирает правки', async () => {
    putCuts.mockRejectedValueOnce(new Error('нет сети'));
    const { result } = renderHook(() => useCutEditor('abstraktnyj-trud', 473, 730, []));

    act(() => result.current.addCut({ start: 5, end: 9 }));
    await act(async () => {
      await result.current.save();
    });

    await waitFor(() => expect(result.current.error).not.toBe(''));
    expect(result.current.cuts).toEqual([{ start: 5, end: 9 }]);
  });

  it('после успешного сохранения чужой набор обновляется из нового existing, а не остаётся прежним', async () => {
    // Находка финального разбора: PutCuts делает DELETE+INSERT
    // (ReplaceForReference), поэтому у каждой вырезки после сохранения новый
    // id. Хук должен пересчитать foreign из нового existing, а не хранить
    // старые id, иначе следующее сохранение отправит id, которого на
    // сервере уже нет, и получит 404.
    const { result, rerender } = renderHook(
      ({ existing }: { existing: ConceptCutInput[] }) =>
        useCutEditor('abstraktnyj-trud', 473, 730, existing),
      { initialProps: { existing: [existingCut(), { id: 5 } as ConceptCutInput] } },
    );

    await act(async () => {
      await result.current.save();
    });

    // Сервер переписал набор адреса — у чужой вырезки (id 5) теперь другой
    // id, как и было бы после реального ReplaceForReference.
    rerender({ existing: [existingCut(), { id: 6 } as ConceptCutInput] });

    putCuts.mockClear();
    await act(async () => {
      await result.current.save();
    });

    const [, , body] = putCuts.mock.calls[0];
    expect(body.cuts).toContainEqual({ id: 6 });
    expect(body.cuts).not.toContainEqual({ id: 5 });
  });

  it('пустой набор сохраняется — это стирание вырезок этой страницы', async () => {
    const { result } = renderHook(() => useCutEditor('abstraktnyj-trud', 473, 730, []));

    await act(async () => {
      await result.current.save();
    });

    expect(putCuts).toHaveBeenCalledWith('abstraktnyj-trud', 473, {
      status: 'confirmed',
      cuts: [],
    });
  });
});
