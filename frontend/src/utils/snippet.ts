export interface SnippetPart {
  text: string;
  hit: boolean;
}

const START = '\u0001';
const STOP = '\u0002';

/**
 * Режет отрывок из /api/search/pages по меткам совпадения. Метки —
 * управляющие символы, а не <mark>: в API нет HTML, и текст полосы с «<»
 * внутри не должен превращаться в разметку. Страница рисует части текстовыми
 * узлами и элементом <mark>, без dangerouslySetInnerHTML.
 */
export function splitSnippet(snippet: string): SnippetPart[] {
  const parts: SnippetPart[] = [];
  let hit = false;
  let buffer = '';
  const flush = () => {
    if (buffer) parts.push({ text: buffer, hit });
    buffer = '';
  };
  for (const ch of snippet) {
    if (ch === START) {
      flush();
      hit = true;
    } else if (ch === STOP) {
      flush();
      hit = false;
    } else {
      buffer += ch;
    }
  }
  flush();
  return parts;
}
