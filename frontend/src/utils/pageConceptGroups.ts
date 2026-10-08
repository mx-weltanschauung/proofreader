import type { ConceptBacklink } from '../types';
import { rubricPathLabel } from './conceptReferences';
import { pageRange } from './pageRange';

export interface GroupedConcept {
  slug: string;
  title: string;
  entries: ConceptBacklink[];
}

/**
 * Обратные ссылки приходят по одной на ссылку указателя, поэтому одно понятие
 * встречается несколько раз — с разными подрубриками. На экране это одна
 * запись со списком подрубрик.
 */
export function groupBySlug(backlinks: ConceptBacklink[]): GroupedConcept[] {
  const groups = new Map<string, GroupedConcept>();
  for (const backlink of backlinks) {
    const existing = groups.get(backlink.slug);
    if (existing) existing.entries.push(backlink);
    else
      groups.set(backlink.slug, {
        slug: backlink.slug,
        title: backlink.title,
        entries: [backlink],
      });
  }
  return [...groups.values()];
}

/** Подпись диапазона страниц одной ссылки; для страницы в одну ячейку — просто номер. */
export function pagesLabel(backlink: ConceptBacklink): string {
  return pageRange(backlink.page_start, backlink.page_end);
}

/** Подпись подрубрики обратной ссылки — та же, что у адреса и записи потока. */
export function rubricLabel(backlink: ConceptBacklink): string {
  return rubricPathLabel(backlink);
}
