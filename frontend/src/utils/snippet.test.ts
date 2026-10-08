import { describe, it, expect } from 'vitest';
import { splitSnippet } from './snippet';

// eslint-disable-next-line no-control-regex -- метки совпадения из /api/search/pages, см. snippet.ts
const MARKERS = /[\u0001\u0002]/g;

/** Части не теряют текст: соединённые как есть, они дают исходную строку без меток. */
function rejoin(snippet: string): string {
  return splitSnippet(snippet)
    .map((p) => p.text)
    .join('');
}

describe('splitSnippet', () => {
  it('режет отрывок по меткам U+0001/U+0002', () => {
    expect(splitSnippet('про \u0001Гегеля\u0002 и \u0001Маркса\u0002.')).toEqual([
      { text: 'про ', hit: false },
      { text: 'Гегеля', hit: true },
      { text: ' и ', hit: false },
      { text: 'Маркса', hit: true },
      { text: '.', hit: false },
    ]);
  });

  it('без меток — один обычный кусок; пустая строка — пусто', () => {
    expect(splitSnippet('тихо')).toEqual([{ text: 'тихо', hit: false }]);
    expect(splitSnippet('')).toEqual([]);
  });

  it('незакрытая метка не теряет текст', () => {
    expect(splitSnippet('a\u0001b')).toEqual([
      { text: 'a', hit: false },
      { text: 'b', hit: true },
    ]);
  });

  it('совпадение в самом начале отрывка не теряет текст', () => {
    const snippet = '\u0001Гегель\u0002 — идеалист.';
    expect(splitSnippet(snippet)).toEqual([
      { text: 'Гегель', hit: true },
      { text: ' — идеалист.', hit: false },
    ]);
    expect(rejoin(snippet)).toBe(snippet.replace(MARKERS, ''));
  });

  it('совпадение в самом конце отрывка не теряет текст', () => {
    const snippet = 'об этом писал \u0001Гегель\u0002';
    expect(splitSnippet(snippet)).toEqual([
      { text: 'об этом писал ', hit: false },
      { text: 'Гегель', hit: true },
    ]);
    expect(rejoin(snippet)).toBe(snippet.replace(MARKERS, ''));
  });

  it('две метки подряд без текста между ними не теряют текст', () => {
    const snippet = 'a\u0001Гегель\u0002\u0001Маркс\u0002b';
    expect(splitSnippet(snippet)).toEqual([
      { text: 'a', hit: false },
      { text: 'Гегель', hit: true },
      { text: 'Маркс', hit: true },
      { text: 'b', hit: false },
    ]);
    expect(rejoin(snippet)).toBe(snippet.replace(MARKERS, ''));
  });
});
