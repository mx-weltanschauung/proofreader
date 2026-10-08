import { useEffect, useState } from 'react';
import { worksApi } from '../services/api';

/**
 * Карта страниц на работу. Кэш модульный, а не в состоянии компонента:
 * PageView перемонтируется на каждой странице, а карта у всей работы одна —
 * без кэша том в 840 страниц перекачивал бы её на каждый шаг листания.
 */
const cache = new Map<number, number[]>();

/** Только для тестов: между прогонами кэш не должен протекать. */
export function __clearPageMapCache(): void {
  cache.clear();
}

interface State {
  workId: number | undefined;
  numbers: number[];
  isLoading: boolean;
}

function initialState(workId: number | undefined): State {
  return {
    workId,
    numbers: workId === undefined ? [] : (cache.get(workId) ?? []),
    isLoading: workId !== undefined && !cache.has(workId),
  };
}

export function useWorkPageMap(workId: number | undefined): {
  numbers: number[];
  isLoading: boolean;
} {
  const [state, setState] = useState<State>(() => initialState(workId));

  // Смена workId у уже смонтированного хука (тот же PageView, другая работа)
  // пересчитывается прямо в рендере, а не в эффекте: setState синхронно
  // внутри эффекта — лишний кадр с картой прошлой работы, react-hooks это и
  // не советует (see https://react.dev/learn/you-might-not-need-an-effect).
  if (state.workId !== workId) {
    setState(initialState(workId));
  }

  useEffect(() => {
    if (workId === undefined || cache.has(workId)) return;

    let cancelled = false;

    worksApi
      .pageMap(workId)
      .then((response) => {
        const list = (response.data ?? []).map((entry) => entry.page_number);
        cache.set(workId, list);
        if (!cancelled) setState({ workId, numbers: list, isLoading: false });
      })
      .catch(() => {
        // Карта — украшение: без неё страница открывается, только пейджер
        // не появится. Ошибку не показываем, чтобы не пугать на пустом месте.
        if (!cancelled) {
          setState((prev) =>
            prev.workId === workId ? { ...prev, numbers: [], isLoading: false } : prev,
          );
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workId]);

  return { numbers: state.numbers, isLoading: state.isLoading };
}
