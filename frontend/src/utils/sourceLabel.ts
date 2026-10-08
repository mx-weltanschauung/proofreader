import type { CollectionEntry } from '../types';

/**
 * Подпись источника строки состава подборки: «Сочинения, 2-е изд., т. 21,
 * с. 269—317». Используется и в оглавлении подборки (CollectionView), и на
 * странице чтения элемента (CollectionRead) — печатные страницы уже посчитаны
 * сервером и лежат в entry.source, здесь только форматирование в строку.
 */
export function sourceLabel(entry: CollectionEntry): string {
  const src = entry.source;
  if (!src) return '';

  const parts: string[] = [];
  if (src.edition_title) parts.push(src.edition_title);
  if (src.volume_number != null) {
    parts.push(`т. ${src.volume_number}${src.volume_part ? ` (${src.volume_part})` : ''}`);
  } else if (src.work_title) {
    parts.push(src.work_title);
  }
  // У битого элемента-главы work_id сохраняется (иначе потерялась бы связь
  // с томом), поэтому source всё же приходит — но с нулевыми страницами.
  // Проверка page_start их и гасит: печатать «с. 0—0» неправда.
  if (src.page_start) parts.push(`с. ${src.page_start}—${src.page_end}`);

  return parts.join(', ');
}
