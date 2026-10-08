import { describe, it, expect, vi, afterEach } from 'vitest';
import { renderHook, waitFor, act } from '@testing-library/react';
import type { AxiosResponse } from 'axios';
import { audioApi } from '../services/api';
import type { WorkAudio } from '../types';
import { useWorkAudio } from './useWorkAudio';

const AUDIO: WorkAudio = { tracks: [], recordings: [] };

afterEach(() => vi.restoreAllMocks());

describe('useWorkAudio', () => {
  it('грузит звук тома и перечитывает по reload', async () => {
    const forWork = vi
      .spyOn(audioApi, 'forWork')
      .mockResolvedValue({ data: AUDIO } as AxiosResponse<WorkAudio>);
    const { result } = renderHook(() => useWorkAudio(4));
    await waitFor(() => expect(result.current.audio).toEqual(AUDIO));
    await act(() => result.current.reload());
    expect(forWork).toHaveBeenCalledTimes(2);
    expect(forWork).toHaveBeenCalledWith(4);
  });

  // Review Focus 5: звук — необязательная часть главы; упавший запрос не
  // превращается в ошибку страницы.
  it('упавший запрос — просто нет звука', async () => {
    vi.spyOn(audioApi, 'forWork').mockRejectedValue(new Error('сеть'));
    const { result } = renderHook(() => useWorkAudio(4));
    await waitFor(() => expect(audioApi.forWork).toHaveBeenCalled());
    expect(result.current.audio).toBeNull();
  });

  // Тикет 07, п. 4: сотруднику «нет звука» и «звук не загрузился» — разное.
  // При пустом списке он прикрепит повторно то, что уже лежит.
  it('упавший запрос отличим от пустого звука; удачный reload снимает признак', async () => {
    const forWork = vi.spyOn(audioApi, 'forWork').mockRejectedValueOnce(new Error('сеть'));
    const { result } = renderHook(() => useWorkAudio(4));
    await waitFor(() => expect(result.current.failed).toBe(true));
    expect(result.current.audio).toBeNull();
    forWork.mockResolvedValueOnce({ data: AUDIO } as AxiosResponse<WorkAudio>);
    await act(() => result.current.reload());
    expect(result.current.failed).toBe(false);
    expect(result.current.audio).toEqual(AUDIO);
  });

  // Рецензия ветки тикета 07: упавшее ПЕРЕчитывание того же тома не стирает
  // известный звук — иначе панель с менеджером записей размонтируется.
  it('упало перечитывание — прежний звук остаётся, признак провала стоит', async () => {
    const OLD: WorkAudio = { tracks: [{ id: 4 } as WorkAudio['tracks'][number]], recordings: [] };
    const forWork = vi
      .spyOn(audioApi, 'forWork')
      .mockResolvedValueOnce({ data: OLD } as AxiosResponse<WorkAudio>);
    const { result } = renderHook(() => useWorkAudio(4));
    await waitFor(() => expect(result.current.audio).toEqual(OLD));
    forWork.mockRejectedValueOnce(new Error('сеть'));
    await act(() => result.current.reload());
    expect(result.current.failed).toBe(true);
    expect(result.current.audio).toEqual(OLD);
  });

  it('первый провал у другого тома — прежний звук не переносится', async () => {
    const OLD: WorkAudio = { tracks: [{ id: 4 } as WorkAudio['tracks'][number]], recordings: [] };
    const forWork = vi
      .spyOn(audioApi, 'forWork')
      .mockResolvedValueOnce({ data: OLD } as AxiosResponse<WorkAudio>);
    const { result, rerender } = renderHook(({ id }) => useWorkAudio(id), {
      initialProps: { id: 4 },
    });
    await waitFor(() => expect(result.current.audio).toEqual(OLD));
    forWork.mockRejectedValueOnce(new Error('сеть'));
    rerender({ id: 5 });
    await waitFor(() => expect(result.current.failed).toBe(true));
    expect(result.current.audio).toBeNull();
  });

  it('пока ответа нет — не «упал»', () => {
    vi.spyOn(audioApi, 'forWork').mockReturnValue(new Promise(() => {}));
    const { result } = renderHook(() => useWorkAudio(4));
    expect(result.current.failed).toBe(false);
  });

  // Рецензия, п. 5: ChapterView переходит между томами без размонтирования.
  // Звук прежнего тома не должен показываться у главы нового — ни до ответа,
  // ни после опоздавшего ответа прежнего тома.
  it('другой том — прежний звук сразу гаснет, опоздавший ответ не в счёт', async () => {
    const OLD: WorkAudio = { tracks: [{ id: 4 } as WorkAudio['tracks'][number]], recordings: [] };
    const NEW: WorkAudio = { tracks: [{ id: 5 } as WorkAudio['tracks'][number]], recordings: [] };
    let releaseOld!: () => void;
    const forWork = vi.spyOn(audioApi, 'forWork');
    forWork.mockResolvedValueOnce({ data: OLD } as AxiosResponse<WorkAudio>);
    const { result, rerender } = renderHook(({ id }) => useWorkAudio(id), {
      initialProps: { id: 4 },
    });
    await waitFor(() => expect(result.current.audio).toEqual(OLD));

    // Второй запрос тома 4 (перечитывание) задержан и придёт после тома 5.
    forWork.mockImplementationOnce(
      () =>
        new Promise((resolve) => {
          releaseOld = () => resolve({ data: OLD } as AxiosResponse<WorkAudio>);
        }),
    );
    const late = result.current.reload();
    forWork.mockResolvedValueOnce({ data: NEW } as AxiosResponse<WorkAudio>);
    rerender({ id: 5 });
    expect(result.current.audio).toBeNull();
    await waitFor(() => expect(result.current.audio).toEqual(NEW));
    await act(async () => {
      releaseOld();
      await late;
    });
    expect(result.current.audio).toEqual(NEW);
  });

  it('без номера тома не ходит никуда', () => {
    const forWork = vi.spyOn(audioApi, 'forWork');
    renderHook(() => useWorkAudio(null));
    expect(forWork).not.toHaveBeenCalled();
  });
});
