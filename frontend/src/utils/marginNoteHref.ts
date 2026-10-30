import type { MarginNote } from '../types';
import { chapterPath, pagePath, workPath } from './paths';

/** Адрес места заметки; пустой `search` у главы и тома. */
export function noteHref(n: MarginNote): { to: string; missing: boolean; deletedChapter: boolean } {
  const work = { id: n.work_id, slug: n.work_slug };
  if (n.target === 'quote') {
    const page = n.start_page ?? n.snapshot_start_page;
    if (page === null) return { to: workPath(work), missing: true, deletedChapter: false };
    const params = [
      n.quote_key ? `quote=${encodeURIComponent(n.quote_key)}` : '',
      n.quote_tail ? `to=${encodeURIComponent(n.quote_tail)}` : '',
    ].filter(Boolean);
    const search = params.length > 0 ? `?${params.join('&')}` : '';
    return {
      to: `${pagePath(work, page)}${search}`,
      missing: n.start_page === null,
      deletedChapter: false,
    };
  }
  if (n.target === 'chapter') {
    if (n.chapter_id === null) return { to: workPath(work), missing: false, deletedChapter: true };
    return {
      to: chapterPath(work, { id: n.chapter_id }),
      missing: false,
      deletedChapter: false,
    };
  }
  return { to: workPath(work), missing: false, deletedChapter: false };
}
