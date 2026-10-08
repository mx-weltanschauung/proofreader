import type { EditionHighlight } from '../types';
import { chapterLabel } from './volumeLabel';

/** Подпись пункта: ручная, а без неё — первое предложение заглавия главы. */
export function highlightLabel(h: Pick<EditionHighlight, 'label' | 'chapter_title'>): string {
  return h.label.trim() || chapterLabel(h.chapter_title);
}

/** Координата тома пункта: «т. 23», «т. 26, III»; без номера — пусто. */
export function highlightCoordinate(
  h: Pick<EditionHighlight, 'volume_number' | 'volume_part'>,
): string {
  if (h.volume_number === null || h.volume_number === undefined) return '';
  return h.volume_part ? `т. ${h.volume_number}, ${h.volume_part}` : `т. ${h.volume_number}`;
}
