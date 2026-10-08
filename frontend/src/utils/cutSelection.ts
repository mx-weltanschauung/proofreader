/**
 * Смещение в БАЙТАХ UTF-8.
 *
 * Сервер режет markdown байтами, JS считает строки в единицах UTF-16: на
 * кириллице разница ровно вдвое, и без пересчёта вырезка уехала бы.
 */
export function byteOffset(text: string, jsOffset: number): number {
  return new TextEncoder().encode(text.slice(0, jsOffset)).length;
}

/**
 * Границы выделения внутри контейнера, в байтах исходного текста.
 *
 * Подсветка режет текст на несколько узлов, поэтому смещение узла — сумма
 * длин всех текстовых узлов до него; считать от начала своего узла значило бы
 * терять всё, что левее.
 */
export function selectionOffsets(
  container: HTMLElement,
  selection: Selection,
): { start: number; end: number } | null {
  if (selection.rangeCount === 0) return null;
  const range = selection.getRangeAt(0);
  if (range.collapsed) return null;
  if (!container.contains(range.startContainer) || !container.contains(range.endContainer)) {
    return null;
  }

  const walker = document.createTreeWalker(container, NodeFilter.SHOW_TEXT);
  let seen = '';
  let start: number | null = null;
  let end: number | null = null;

  for (let node = walker.nextNode(); node; node = walker.nextNode()) {
    const text = node.textContent ?? '';
    if (node === range.startContainer)
      start = byteOffset(seen + text, seen.length + range.startOffset);
    if (node === range.endContainer) end = byteOffset(seen + text, seen.length + range.endOffset);
    seen += text;
  }

  if (start === null || end === null || start >= end) return null;
  return { start, end };
}
