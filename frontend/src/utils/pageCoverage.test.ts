import { describe, it, expect } from 'vitest';
import type { Chapter, PageMapEntry } from '../types';
import { uncoveredRanges } from './pageCoverage';

const pages = (...numbers: number[]): PageMapEntry[] =>
  numbers.map((n) => ({ page_number: n, status: 'не_вычитана' }));

const chapter = (start: number, end: number): Chapter =>
  ({ id: start, work_id: 1, title: 'г', start_page: start, end_page: end }) as Chapter;

describe('uncoveredRanges', () => {
  it('собирает непокрытые страницы в непрерывные диапазоны', () => {
    expect(uncoveredRanges(pages(1, 2, 3, 4, 5, 6, 7), [chapter(3, 5)])).toEqual([
      { start: 1, end: 2 },
      { start: 6, end: 7 },
    ]);
  });

  it('на томе без глав отдаёт весь том одним диапазоном', () => {
    expect(uncoveredRanges(pages(1, 2, 3), [])).toEqual([{ start: 1, end: 3 }]);
  });

  it('на полностью покрытом томе отдаёт пустой список', () => {
    expect(uncoveredRanges(pages(1, 2), [chapter(1, 2)])).toEqual([]);
  });

  // Дыры в нумерации страниц не склеиваются в один диапазон: страницы 4 в
  // томе нет, и обещать её нельзя.
  it('разрывает диапазон на пропущенном номере', () => {
    expect(uncoveredRanges(pages(1, 2, 5), [])).toEqual([
      { start: 1, end: 2 },
      { start: 5, end: 5 },
    ]);
  });
});
