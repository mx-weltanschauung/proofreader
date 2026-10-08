import type { Chapter } from '../types';
import { chapterLevelsForPage } from '../hooks/useChaptersForPage';
import { toRoman, type FolioSource } from './folio';

/**
 * Номер полосы по тому, что набрал читатель.
 *
 * Читатель приходит с печатной ссылкой («ПСС, т. 6, с. 123»), поэтому набранное
 * — колонцифра, а не адресный номер: `page_number = печатная − page_offset`
 * (обратное к `printedFolio`). У тома сдвиг нулевой и разницы нет; у
 * передних листов колонцифра римская, и её тоже можно набрать — «iii».
 * Приставка «с.»/«стр.» терпится: её копируют вместе с номером из сноски.
 *
 * null — набранное номером не является или уходит за начало работы.
 */
export function parsePrintedPage(input: string, work: FolioSource): number | null {
  const raw = input
    .trim()
    .replace(/^с(?:тр)?\.?\s*/i, '')
    .toUpperCase();
  let printed: number;
  if (/^\d+$/.test(raw)) {
    printed = Number(raw);
  } else if (/^[IVXLCDM]+$/.test(raw)) {
    printed = fromRoman(raw);
    // «IIII» и «VX» складываются в число, но колонцифрой не бывают: годится
    // только запись, которую напечатал бы сам printedFolio.
    if (toRoman(printed) !== raw) return null;
  } else {
    return null;
  }
  // Колонцифры меньше единицы не печатают: обложка, которую сдвиг передних
  // листов выводит за счёт, набранным номером не адресуется.
  if (printed < 1) return null;
  const pageNumber = printed - work.page_offset;
  return pageNumber >= 1 ? pageNumber : null;
}

function fromRoman(roman: string): number {
  const value: Record<string, number> = { I: 1, V: 5, X: 10, L: 50, C: 100, D: 500, M: 1000 };
  let total = 0;
  for (let i = 0; i < roman.length; i += 1) {
    const current = value[roman[i]];
    const next = value[roman[i + 1]] ?? 0;
    total += current < next ? -current : current;
  }
  return total;
}

/**
 * Куда ведёт переход к полосе.
 *
 * - `here` — полоса уже в документе: прокрутка, без перехода.
 * - `chapter` — самая узкая глава, которая её накрывает (то же правило, что у
 *   поиска и `/seo`: `pageHitChapterJoin`, `FindByPage`). На стыке сестёр —
 *   та, что на этой полосе начинается: читатель с номером из ссылки ищет
 *   начало текста, а не хвост предыдущего.
 * - `uncovered` — ни одна глава полосу не накрывает (содержание, колофон).
 *   Есть ли она в томе вообще, отсюда не видно: это решает звавший, по карте
 *   полос.
 */
export type PageJumpTarget =
  | { kind: 'here'; pageNumber: number }
  | { kind: 'chapter'; pageNumber: number; chapter: Chapter }
  | { kind: 'uncovered'; pageNumber: number };

export function resolvePageJump(
  pageNumber: number,
  tree: Chapter[],
  isLoaded: (pageNumber: number) => boolean,
): PageJumpTarget {
  if (isLoaded(pageNumber)) return { kind: 'here', pageNumber };
  const levels = chapterLevelsForPage(tree, pageNumber);
  const narrowest = levels[levels.length - 1];
  if (!narrowest) return { kind: 'uncovered', pageNumber };
  // Уровень отсортирован по order_number, поэтому последняя — та, что
  // начинается позже всех, то есть на самой полосе, если такая есть.
  return { kind: 'chapter', pageNumber, chapter: narrowest[narrowest.length - 1] };
}

/** Отказ, одинаковый у всех мест, где номер набирают руками. */
export function missingPageMessage(input: string): string {
  return `В томе нет страницы ${input.trim()}`;
}
