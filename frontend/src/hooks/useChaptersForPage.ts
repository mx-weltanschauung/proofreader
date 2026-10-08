import { useEffect, useState } from 'react';
import { chaptersApi } from '../services/api';
import type { Chapter } from '../types';

// Принадлежность страницы главе — производная от диапазона start_page/end_page,
// а не хранимая связь: pages.chapter_id не заполняется ничем в проекте.
// Главы иерархичны, поэтому страница попадает сразу в несколько; на стыковых
// страницах совпадают ещё и сестринские главы (там одна секция кончается и
// начинается следующая). Отсюда массив уровней, а не плоский список.
export function chapterLevelsForPage(tree: Chapter[], pageNumber: number): Chapter[][] {
  const byDepth = new Map<number, Chapter[]>();

  const walk = (nodes: Chapter[], depth: number) => {
    for (const chapter of nodes) {
      if (chapter.start_page <= pageNumber && pageNumber <= chapter.end_page) {
        const level = byDepth.get(depth);
        if (level) {
          level.push(chapter);
        } else {
          byDepth.set(depth, [chapter]);
        }
      }

      // Спускаемся всегда, даже если родитель не совпал. В корректных данных
      // диапазон потомка вложен в родительский и это ничего не меняет, а при
      // битой разметке глава иначе исчезла бы молча.
      if (chapter.children && chapter.children.length > 0) {
        walk(chapter.children, depth + 1);
      }
    }
  };

  walk(tree, 0);

  // Глубины без совпадений схлопываем: наружу уходит плотный массив уровней,
  // где levels[0] — самый внешний из найденных.
  return [...byDepth.entries()]
    .sort(([left], [right]) => left - right)
    .map(([, level]) => [...level].sort((a, b) => a.order_number - b.order_number));
}

export interface ChaptersForPageResult {
  levels: Chapter[][];
  isLoading: boolean;
}

// Стабильная ссылка на пустой результат: ChapterBreadcrumb сравнивает levels
// по ссылке во время рендера, и новый [] на каждом рендере стоил бы ему
// лишнего render-phase setState и прохода компонента.
const NO_LEVELS: Chapter[][] = [];

function isValidRequest(
  workId: string | undefined,
  pageNumber: number | undefined,
): pageNumber is number {
  const work = Number(workId);
  // Number('') === 0 и Number('не-число') === NaN, поэтому проверяем именно
  // положительный integer, иначе в API уйдёт заведомо бессмысленный запрос.
  return (
    Number.isInteger(work) &&
    work > 0 &&
    pageNumber !== undefined &&
    Number.isInteger(pageNumber) &&
    pageNumber > 0
  );
}

// Единое состояние загрузки вместо пары независимых setState: levels и
// isLoading выводятся из него, а не рассинхронизируются между собой.
type LoadState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok'; levels: Chapter[][] }
  | { status: 'error' };

// Тонкая обёртка над chapterLevelsForPage: вся нетривиальность — там, здесь
// только загрузка и отмена устаревшего ответа, по образцу usePageByNumber.
export function useChaptersForPage(
  workId: string | undefined,
  pageNumber: number | undefined,
): ChaptersForPageResult {
  const requestKey = `${workId ?? ''}|${pageNumber ?? ''}`;
  const [state, setState] = useState<LoadState>(() =>
    isValidRequest(workId, pageNumber) ? { status: 'loading' } : { status: 'idle' },
  );

  // Смена страницы/работы должна сразу сбросить прошлый результат, а не
  // показывать главы уже покинутой страницы до ответа нового запроса.
  const [prevRequest, setPrevRequest] = useState(requestKey);
  if (prevRequest !== requestKey) {
    setPrevRequest(requestKey);
    setState(isValidRequest(workId, pageNumber) ? { status: 'loading' } : { status: 'idle' });
  }

  useEffect(() => {
    const work = Number(workId);
    if (!isValidRequest(workId, pageNumber)) return;
    const target = pageNumber;

    let cancelled = false;

    chaptersApi
      .list(work)
      .then((res) => {
        if (!cancelled) setState({ status: 'ok', levels: chapterLevelsForPage(res.data, target) });
      })
      .catch((err: unknown) => {
        // Главы — вспомогательная навигация, а не содержимое страницы: их
        // отказ не должен ломать экран и не стоит тоста поверх редактора.
        if (!cancelled) {
          console.error('Failed to load chapters:', err);
          setState({ status: 'error' });
        }
      });

    return () => {
      cancelled = true;
    };
  }, [workId, pageNumber]);

  const levels = state.status === 'ok' ? state.levels : NO_LEVELS;
  const isLoading = state.status === 'loading';

  return { levels, isLoading };
}
