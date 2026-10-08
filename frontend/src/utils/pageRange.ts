/**
 * Подпись диапазона страниц: одна и та же страница с начала и до конца —
 * просто номер, иначе — «начало—конец» с длинным тире (не дефисом — тот
 * читается как перенос слова, а не как диапазон).
 *
 * Было продублировано байт в байт в трёх местах (conceptReferences,
 * pageConceptGroups, ConceptView) — здесь единственный источник.
 */
export function pageRange(start: number, end: number): string {
  return start === end ? `${start}` : `${start}—${end}`;
}
