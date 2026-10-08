import type { PageMapEntry, Work } from '../types';
import { PAGE_STATUS_LABEL } from './pageStatus';
import { printedFolio } from './folio';

/**
 * Подпись клетки страницы. Живёт отдельно от `PageCells.tsx`: экспорт
 * функции рядом с компонентом ловит `react-refresh/only-export-components`
 * при `--max-warnings 0` (см. `conceptAlphabet.ts` — тот же приём). Подпись —
 * единственное, чем статус доступен без различения цвета: значок внутри
 * клетки в 10 px не читается.
 */
export function pageCellLabel(work: Work, entry: PageMapEntry): string {
  const folio = printedFolio(entry.page_number, work);
  const head =
    folio && folio !== String(entry.page_number)
      ? `стр. ${entry.page_number}, печатная ${folio}`
      : `стр. ${entry.page_number}`;
  return `${head}, ${PAGE_STATUS_LABEL[entry.status]}`;
}
