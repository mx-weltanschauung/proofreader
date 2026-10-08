import { describe, expect, it } from 'vitest';
import { pageAnchorId, pageNumberFromHash } from './pageAnchor';

describe('pageAnchorId', () => {
  it('собирает якорь секции полосы', () => {
    expect(pageAnchorId(42)).toBe('chapter-page-42');
  });
});

describe('pageNumberFromHash', () => {
  it('разбирает номер полосы из хэша с решёткой', () => {
    expect(pageNumberFromHash('#chapter-page-42')).toBe(42);
  });

  it('разбирает номер полосы из хэша без решётки', () => {
    expect(pageNumberFromHash('chapter-page-42')).toBe(42);
  });

  it('пустой хэш — undefined', () => {
    expect(pageNumberFromHash('')).toBeUndefined();
  });

  it('хэш другого вида — undefined', () => {
    expect(pageNumberFromHash('#footnote-3')).toBeUndefined();
  });

  // Не путать с якорем подглавы (#chapter-page-N-sub и т. п.) — частичное
  // совпадение не должно давать ложный номер полосы.
  it('хэш с хвостом после номера — undefined', () => {
    expect(pageNumberFromHash('#chapter-page-42-extra')).toBeUndefined();
  });
});
