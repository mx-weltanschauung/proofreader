import type { PageMapEntry, PageStatus } from '../types';
import { PAGE_STATUS_LABEL, PAGE_STATUS_MODIFIER, PAGE_STATUS_ORDER } from './pageStatus';

export interface ReadinessSegment {
  status: PageStatus;
  /** Латинский суффикс класса: русские статусы в CSS-селекторы не пишем. */
  modifier: string;
  /** Доля от всего диапазона, 0…1. */
  share: number;
  count: number;
}

/**
 * Статусы, которые полоска показывает. `не_вычитана` — фон полоски, а
 * `пустая_страница` — не работа: вычитывать в пустой полосе нечего, и красить
 * её как сделанную было бы враньём.
 */
const SHOWN: PageStatus[] = PAGE_STATUS_ORDER.filter(
  (status) => status !== 'не_вычитана' && status !== 'пустая_страница',
);

/**
 * Доли статусов в диапазоне. **Пустой массив означает «полоску не рисовать»**:
 * в томе, где вычитки ещё не было, полоска у каждой строки была бы одинаковым
 * серым прямоугольником — шумом на полутора сотнях строк. Место под неё
 * компонент оставляет в любом случае, чтобы правый край не плясал.
 */
export function readinessSegments(pages: PageMapEntry[]): ReadinessSegment[] {
  if (pages.length === 0) return [];

  const counts = new Map<PageStatus, number>();
  for (const page of pages) counts.set(page.status, (counts.get(page.status) ?? 0) + 1);

  const segments: ReadinessSegment[] = [];
  for (const status of SHOWN) {
    const count = counts.get(status) ?? 0;
    if (count === 0) continue;
    segments.push({
      status,
      modifier: PAGE_STATUS_MODIFIER[status],
      share: count / pages.length,
      count,
    });
  }
  return segments;
}

/**
 * Имя полоски для скринридера: цвет — единственное, чем она говорит, поэтому
 * без подписи она нема.
 */
export function readinessLabel(pages: PageMapEntry[]): string {
  const parts = readinessSegments(pages).map(
    (segment) => `${PAGE_STATUS_LABEL[segment.status]} ${segment.count}`,
  );
  return [...parts, `всего ${pages.length}`].join(', ');
}
