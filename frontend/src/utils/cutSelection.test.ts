import { describe, it, expect } from 'vitest';
import { byteOffset, selectionOffsets } from './cutSelection';

describe('byteOffset', () => {
  it('кириллица считается в байтах, а не в символах', () => {
    // Сервер режет markdown байтами; JS считает строки в единицах UTF-16.
    // Без пересчёта вырезка «абв» уехала бы втрое.
    expect(byteOffset('абв', 3)).toBe(6);
    expect(byteOffset('abc', 3)).toBe(3);
    expect(byteOffset('аbв', 2)).toBe(3);
  });

  it('нулевое и полное смещение', () => {
    expect(byteOffset('абв', 0)).toBe(0);
    expect(byteOffset('', 0)).toBe(0);
  });
});

describe('selectionOffsets', () => {
  function withDom(html: string, pick: (root: HTMLElement) => Range) {
    const root = document.createElement('pre');
    root.innerHTML = html;
    document.body.appendChild(root);
    const selection = window.getSelection()!;
    selection.removeAllRanges();
    selection.addRange(pick(root));
    const got = selectionOffsets(root, selection);
    document.body.removeChild(root);
    return got;
  }

  it('выделение внутри одного узла', () => {
    const got = withDom('абвгд', (root) => {
      const range = document.createRange();
      range.setStart(root.firstChild!, 1);
      range.setEnd(root.firstChild!, 3);
      return range;
    });

    expect(got).toEqual({ start: 2, end: 6 });
  });

  it('выделение через подсвеченную границу', () => {
    // Подсветка режет текст на несколько узлов: смещение узла — сумма длин
    // предшествующих текстовых узлов, иначе вторая половина выделения
    // считалась бы от нуля.
    const got = withDom('аб<mark>вг</mark>де', (root) => {
      const range = document.createRange();
      range.setStart(root.childNodes[0], 1);
      range.setEnd(root.childNodes[2], 1);
      return range;
    });

    expect(got).toEqual({ start: 2, end: 10 });
  });

  it('схлопнутое выделение — не выделение', () => {
    const got = withDom('абвгд', (root) => {
      const range = document.createRange();
      range.setStart(root.firstChild!, 2);
      range.setEnd(root.firstChild!, 2);
      return range;
    });

    expect(got).toBeNull();
  });

  it('выделение вне контейнера игнорируется', () => {
    const outside = document.createElement('div');
    outside.textContent = 'чужой текст';
    document.body.appendChild(outside);
    const root = document.createElement('pre');
    root.textContent = 'наш текст';
    document.body.appendChild(root);

    const selection = window.getSelection()!;
    const range = document.createRange();
    range.selectNodeContents(outside);
    selection.removeAllRanges();
    selection.addRange(range);

    expect(selectionOffsets(root, selection)).toBeNull();

    document.body.removeChild(outside);
    document.body.removeChild(root);
  });
});
