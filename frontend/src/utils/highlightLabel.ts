import type { EditionHighlight } from '../types';
import { chapterLabel } from './volumeLabel';
import { shownVolumeNumber } from './shownVolume';

/** Подпись пункта: ручная, а без неё — первое предложение заглавия главы. */
export function highlightLabel(h: Pick<EditionHighlight, 'label' | 'chapter_title'>): string {
  return h.label.trim() || chapterLabel(h.chapter_title);
}

/**
 * Координата тома пункта: «т. 23», «т. 26, III»; без номера — пусто. У
 * собрания с условной нумерацией номера тоже нет: сервер гасит его у
 * избранного сам, а экран выбора избранного (`EditionHighlightsEdit`) кладёт
 * признак тома в пункт, собранный на клиенте.
 */
export function highlightCoordinate(
  h: Pick<EditionHighlight, 'volume_number' | 'volume_part' | 'volume_order_only'>,
): string {
  const number = shownVolumeNumber(h);
  if (number === undefined) return '';
  return h.volume_part ? `т. ${number}, ${h.volume_part}` : `т. ${number}`;
}
