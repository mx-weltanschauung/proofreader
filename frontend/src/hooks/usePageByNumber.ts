import { useEffect, useState } from 'react';
import { pagesApi } from '../services/api';
import type { Page } from '../types';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';

export interface PageByNumberResult {
  page: Page | null;
  isLoading: boolean;
  /** Такого номера в работе нет либо параметр URL — не число. */
  notFound: boolean;
  /** Прочие ошибки загрузки. */
  error: string;
}

// Единое состояние резолва вместо четырёх независимых setState: у страницы,
// 404 и прочей ошибки нет промежуточных комбинаций, значит и хранить их стоит
// одним полем, а не рассинхронизируемым набором флагов.
type ResolveState =
  | { status: 'invalid' }
  | { status: 'loading' }
  | { status: 'ok'; page: Page }
  | { status: 'notfound' }
  | { status: 'error'; message: string };

function isValidRequest(workId: string | undefined, pageNumber: string | undefined): boolean {
  const work = Number(workId);
  const number = Number(pageNumber);
  // Number('') === 0, поэтому пустого параметра мало — нужен положительный
  // integer, иначе в API уйдёт заведомо бессмысленный запрос.
  return Number.isInteger(work) && work > 0 && Number.isInteger(number) && number > 0;
}

// Резолвит номер страницы из URL в саму страницу. Резолв и трактовка 404
// живут здесь, а не в компонентах, чтобы PageView и PageEditor не разъехались
// в поведении на одинаковых ссылках.
export function usePageByNumber(
  workId: string | undefined,
  pageNumber: string | undefined,
): PageByNumberResult {
  const requestKey = `${workId ?? ''}|${pageNumber ?? ''}`;
  const [state, setState] = useState<ResolveState>(() =>
    isValidRequest(workId, pageNumber) ? { status: 'loading' } : { status: 'invalid' },
  );

  // Смена номера страницы (в том числе на невалидный) должна сразу вернуть
  // состояние к «свежему» резолву, а не унаследовать ответ на прошлый запрос.
  const [prevRequest, setPrevRequest] = useState(requestKey);
  if (prevRequest !== requestKey) {
    setPrevRequest(requestKey);
    setState(isValidRequest(workId, pageNumber) ? { status: 'loading' } : { status: 'invalid' });
  }

  useEffect(() => {
    const work = Number(workId);
    const number = Number(pageNumber);
    if (!isValidRequest(workId, pageNumber)) return;

    let cancelled = false;

    pagesApi
      .getByNumber(work, number)
      .then((res) => {
        if (!cancelled) setState({ status: 'ok', page: res.data });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        if (apiErrorStatus(err) === 404) {
          setState({ status: 'notfound' });
        } else {
          setState({ status: 'error', message: apiErrorMessage(err, 'Failed to load page') });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workId, pageNumber]);

  const page = state.status === 'ok' ? state.page : null;
  const isLoading = state.status === 'loading';
  const notFound = state.status === 'invalid' || state.status === 'notfound';
  const error = state.status === 'error' ? state.message : '';

  return { page, isLoading, notFound, error };
}
