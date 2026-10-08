import { useEffect, useState } from 'react';
import { conceptsApi } from '../services/api';
import type { ConceptBacklink } from '../types';

export interface PageConceptsResult {
  backlinks: ConceptBacklink[];
  isLoading: boolean;
  /** Запрос не удался. Отличается от пустого списка: тот — норма. */
  failed: boolean;
}

// Стабильная ссылка на пустой результат — по образцу NO_LEVELS в
// useChaptersForPage: новый [] на каждом рендере стоил бы потребителю
// лишнего прохода.
const NO_BACKLINKS: ConceptBacklink[] = [];

type LoadState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok'; backlinks: ConceptBacklink[] }
  | { status: 'error' };

function isValidRequest(workId: number | undefined, pageId: number | undefined): boolean {
  return (
    workId !== undefined &&
    Number.isInteger(workId) &&
    workId > 0 &&
    pageId !== undefined &&
    Number.isInteger(pageId) &&
    pageId > 0
  );
}

/**
 * Понятия, описываемые на странице тома. Работа без координат тома получает
 * с сервера пустой список — отдельной проверки здесь не нужно.
 */
export function usePageConcepts(
  workId: number | undefined,
  pageId: number | undefined,
): PageConceptsResult {
  const requestKey = `${workId ?? ''}|${pageId ?? ''}`;
  const [state, setState] = useState<LoadState>(() =>
    isValidRequest(workId, pageId) ? { status: 'loading' } : { status: 'idle' },
  );

  // Переход на соседнюю страницу должен сразу убрать прошлые понятия, а не
  // показывать их до ответа нового запроса.
  const [prevRequest, setPrevRequest] = useState(requestKey);
  if (prevRequest !== requestKey) {
    setPrevRequest(requestKey);
    setState(isValidRequest(workId, pageId) ? { status: 'loading' } : { status: 'idle' });
  }

  useEffect(() => {
    if (!isValidRequest(workId, pageId) || workId === undefined || pageId === undefined) return;

    let cancelled = false;

    conceptsApi
      .forPage(workId, pageId)
      .then((response) => {
        if (!cancelled) setState({ status: 'ok', backlinks: response.data ?? [] });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        console.error('Failed to load page concepts:', err);
        setState({ status: 'error' });
      });

    return () => {
      cancelled = true;
    };
  }, [workId, pageId]);

  return {
    backlinks: state.status === 'ok' ? state.backlinks : NO_BACKLINKS,
    isLoading: state.status === 'loading',
    failed: state.status === 'error',
  };
}
