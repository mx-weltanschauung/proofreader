/** Та же нормализация, что у конфига ru на сервере: регистр и ё→е. */
export function normalizeWord(s: string): string {
  return s.toLowerCase().replace(/ё/g, 'е');
}

// Матчится в пределах одного текстового узла, поэтому слово, разбитое
// границей инлайн-разметки посередине («<em>Геге</em>ля»), не подсвечивается
// целиком — безопасная деградация, а не недосмотр: инлайн-разметка внутри
// слова в тексте читалки встречается легитимно (курсив на части слова и т.п.).
const WORD_RE = /[\p{L}\p{N}]+/gu;

/**
 * Оборачивает в <mark class="search-hit"> слова, начинающиеся с одной из
 * лемм. Лемма snowball — слово без суффикса, поэтому «гегел» подсветит и
 * «Гегеля», и «Гегелем» без стеммера на клиенте; изредка зацепит и лишнее
 * («парт» — «партизан»), это терпимо. Обходит только текстовые узлы, уже
 * подсвеченное пропускает — вызов идемпотентен. Возвращает число обёрнутых.
 */
export function highlightTerms(root: HTMLElement, terms: string[]): number {
  const stems = terms.map(normalizeWord).filter((s) => s.length > 0);
  if (stems.length === 0) return 0;

  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode: (node) => {
      const parent = node.parentElement;
      // .katex — вывод формулы: и скрытый исходник TeX (annotation в
      // katex-mathml), и разложенная по спанам вёрстка, где каждый знак —
      // свой текстовый узел. Числовой или однобуквенный запрос обернул бы
      // такие знаки в mark и разъехал бы формулу.
      if (!parent || parent.closest('mark.search-hit, script, style, .katex')) {
        return NodeFilter.FILTER_REJECT;
      }
      return NodeFilter.FILTER_ACCEPT;
    },
  });
  const textNodes: Text[] = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) textNodes.push(n as Text);

  let count = 0;
  for (const node of textNodes) {
    const text = node.data;
    const ranges: [number, number][] = [];
    for (const m of text.matchAll(WORD_RE)) {
      const word = normalizeWord(m[0]);
      if (stems.some((stem) => word.startsWith(stem))) {
        ranges.push([m.index ?? 0, (m.index ?? 0) + m[0].length]);
      }
    }
    if (ranges.length === 0) continue;

    const fragment = document.createDocumentFragment();
    let cursor = 0;
    for (const [start, end] of ranges) {
      if (start > cursor) fragment.appendChild(document.createTextNode(text.slice(cursor, start)));
      const mark = document.createElement('mark');
      mark.className = 'search-hit';
      mark.textContent = text.slice(start, end);
      fragment.appendChild(mark);
      cursor = end;
      count += 1;
    }
    if (cursor < text.length) fragment.appendChild(document.createTextNode(text.slice(cursor)));
    node.parentNode?.replaceChild(fragment, node);
  }
  return count;
}
