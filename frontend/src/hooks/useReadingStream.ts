import { useCallback, useEffect, useState } from 'react';
import { readingApi } from '../services/api';
import type { ReadingPage } from '../types';
import { apiErrorMessage } from '../utils/apiError';

/** Одно загруженное окно: страницы и номер, с которого оно начиналось. */
export interface LoadedWindow {
  from: number;
  pages: ReadingPage[];
}

export interface ReadingStreamResult {
  windows: LoadedWindow[];
  totalPages: number;
  /** Первое окно ещё летит — на экране нечего показывать. */
  isLoading: boolean;
  /** Догрузка следующего окна; текст на экране уже есть. */
  isLoadingMore: boolean;
  error: string;
  hasMore: boolean;
  loadMore: () => void;
  retry: () => void;
}

// Одно состояние вместо связки флагов: у «грузим первое», «грузим ещё»,
// «ошибка» и «дошли до конца» нет промежуточных комбинаций, а
// рассинхронизировать раздельные setState — вопрос времени. Та же причина,
// что в useConceptFragments.
interface StreamState {
  key: string;
  windows: LoadedWindow[];
  totalPages: number;
  /**
   * Окно, которое запрашивается прямо сейчас. null — в полёте ничего нет.
   *
   * Отдельно от nextFrom намеренно. Эффект стреляет на смену этого поля, и
   * если бы сюда же клался номер следующего окна из ответа, каждый успешный
   * ответ немедленно запускал бы загрузку следующего — поток выкачал бы всю
   * работу залпом, ровно то, от чего мы уходим.
   */
  requested: number | null;
  /** С какой страницы начинается следующее окно. null — работа кончилась. */
  nextFrom: number | null;
  loaded: boolean;
  pending: boolean;
  error: string;
  attempt: number;
}

function streamKey(workId: number | undefined, startPage: number | undefined): string {
  return `${workId ?? ''}|${startPage ?? ''}`;
}

function initial(key: string, startPage: number | undefined, pending: boolean): StreamState {
  return {
    key,
    windows: [],
    totalPages: 0,
    requested: startPage ?? null,
    nextFrom: null,
    loaded: false,
    pending,
    error: '',
    attempt: 0,
  };
}

/**
 * Поток страниц работы окнами, вниз от стартовой страницы.
 *
 * Вверх не грузит: точка входа — верх потока. Войдя на 400-й странице,
 * читатель видит её первой, и выше подниматься некуда.
 */
export function useReadingStream(
  workId: number | undefined,
  startPage: number | undefined,
): ReadingStreamResult {
  const key = streamKey(workId, startPage);
  const ready = workId != null && startPage != null;
  const [state, setState] = useState<StreamState>(() => initial(key, startPage, ready));

  // Смена работы или точки входа должна сразу вернуть поток к пустому —
  // иначе экран покажет страницы из прежнего тома. Сравнение в теле
  // компонента, а не setState в эффекте: последнее запрещает
  // react-hooks/set-state-in-effect.
  const [prevKey, setPrevKey] = useState(key);
  if (prevKey !== key) {
    setPrevKey(key);
    setState(initial(key, startPage, ready));
  }

  const { requested, attempt } = state;

  useEffect(() => {
    if (!ready || requested === null) return;
    let cancelled = false;

    readingApi
      .window(workId, requested)
      .then((res) => {
        if (cancelled) return;
        setState((prev) => {
          if (prev.key !== key) return prev;
          return {
            ...prev,
            windows: [...prev.windows, { from: requested, pages: res.data.pages }],
            totalPages: res.data.total_pages,
            // Откуда следующее окно, знает только что пришедший ответ; null
            // закрывает hasMore. Само по себе это загрузку не запускает —
            // запустит loadMore, переложив номер в requested.
            nextFrom: res.data.next_from,
            requested: null,
            loaded: true,
            pending: false,
            error: '',
          };
        });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setState((prev) => {
          if (prev.key !== key) return prev;
          return {
            ...prev,
            pending: false,
            // requested не сбрасываем: retry поднимет attempt, и эффект
            // перезапросит ровно то окно, которое не доехало.
            error: apiErrorMessage(err, 'Не удалось загрузить страницы'),
          };
        });
      });

    return () => {
      cancelled = true;
    };
  }, [ready, workId, key, requested, attempt]);

  const loadMore = useCallback(() => {
    setState((prev) => {
      if (prev.pending || prev.error !== '' || prev.nextFrom === null || !prev.loaded) return prev;
      return { ...prev, requested: prev.nextFrom, pending: true };
    });
  }, []);

  const retry = useCallback(() => {
    setState((prev) =>
      prev.error === '' ? prev : { ...prev, pending: true, error: '', attempt: prev.attempt + 1 },
    );
  }, []);

  return {
    windows: state.windows,
    totalPages: state.totalPages,
    isLoading: state.pending && !state.loaded,
    isLoadingMore: state.pending && state.loaded,
    error: state.error,
    hasMore: state.loaded && state.nextFrom !== null,
    loadMore,
    retry,
  };
}
