import type { Chapter, PageMapEntry } from '../types';

/**
 * Страницы, не покрытые ни одной главой верхнего уровня, сведённые в
 * непрерывные диапазоны. В томе 16 это 1—2 (титул), 527—528 и 639—840
 * (примечания, указатели, содержание) — четверть тома, которой в оглавлении
 * нет и до которой иначе не добраться.
 */
export function uncoveredRanges(
  pages: PageMapEntry[],
  chapters: Chapter[],
): { start: number; end: number }[] {
  const covered = new Set<number>();
  for (const chapter of chapters) {
    for (let n = chapter.start_page; n <= chapter.end_page; n += 1) covered.add(n);
  }

  const ranges: { start: number; end: number }[] = [];
  for (const page of pages) {
    const n = page.page_number;
    if (covered.has(n)) continue;
    const last = ranges[ranges.length - 1];
    // Соседними считаются только подряд идущие номера: дыра в нумерации —
    // это отсутствующая страница, а не часть диапазона.
    if (last && last.end === n - 1) last.end = n;
    else ranges.push({ start: n, end: n });
  }
  return ranges;
}
