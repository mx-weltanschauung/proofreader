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

function isValidRequest(
  workId: string | undefined,
  pageNumber: string | undefined,
  verifiedPageId: string | null,
): boolean {
  const work = Number(workId);
  const number = Number(pageNumber);
  // Number('') === 0, поэтому пустого параметра мало — нужен положительный
  // integer, иначе в API уйдёт заведомо бессмысленный запрос.
  return (
    Number.isInteger(work) &&
    work > 0 &&
    Number.isInteger(number) &&
    number > 0 &&
    (verifiedPageId === null ||
      (/^[1-9][0-9]*$/.test(verifiedPageId) && Number.isSafeInteger(Number(verifiedPageId))))
  );
}

// Резолвит номер страницы из URL в саму страницу. Резолв и трактовка 404
// живут здесь, а не в компонентах, чтобы PageView и PageEditor не разъехались
// в поведении на одинаковых ссылках.
export function usePageByNumber(
  workId: string | undefined,
  pageNumber: string | undefined,
  verifiedPageId: string | null = null,
): PageByNumberResult {
  const requestKey = JSON.stringify([workId, pageNumber, verifiedPageId]);
  const valid = isValidRequest(workId, pageNumber, verifiedPageId);
  const [state, setState] = useState<ResolveState>(() =>
    valid ? { status: 'loading' } : { status: 'invalid' },
  );

  // Смена номера страницы (в том числе на невалидный) должна сразу вернуть
  // состояние к «свежему» резолву, а не унаследовать ответ на прошлый запрос.
  const [prevRequest, setPrevRequest] = useState(requestKey);
  if (prevRequest !== requestKey) {
    setPrevRequest(requestKey);
    setState(valid ? { status: 'loading' } : { status: 'invalid' });
  }

  useEffect(() => {
    const work = Number(workId);
    const number = Number(pageNumber);
    if (!valid) return;

    let cancelled = false;
    const expectedId = verifiedPageId === null ? null : Number(verifiedPageId);
    // Между переходом по отсылке и чтением полосу могли заменить либо
    // перенумеровать. Проверяем и идентификатор, и адрес, указанный в ссылке.
    const request =
      expectedId === null ? pagesApi.getByNumber(work, number) : pagesApi.get(work, expectedId);

    request
      .then(({ data: page }) => {
        if (cancelled) return;
        if (
          expectedId !== null &&
          (page.id !== expectedId || page.work_id !== work || page.page_number !== number)
        ) {
          setState({ status: 'notfound' });
        } else {
          setState({ status: 'ok', page });
        }
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
  }, [valid, workId, pageNumber, verifiedPageId]);

  const page = state.status === 'ok' ? state.page : null;
  const isLoading = state.status === 'loading';
  const notFound = state.status === 'invalid' || state.status === 'notfound';
  const error = state.status === 'error' ? state.message : '';

  return { page, isLoading, notFound, error };
}
