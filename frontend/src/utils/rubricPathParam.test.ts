import { describe, expect, it } from 'vitest';
import { decodeRubricPath, encodeRubricPath } from './rubricPathParam';

describe('rubricPathParam', () => {
  it('звено с двоеточием остаётся одним звеном, а не разваливает путь', () => {
    // В корпусе три подрубрики несут двоеточие в заголовке — случай живой.
    const path = ['a:b', 'значение съезда'];
    expect(decodeRubricPath(encodeRubricPath(path))).toEqual(path);
  });

  it('звено с дефисом не склеивается с соседом', () => {
    // Дефис encodeURIComponent не кодирует — ради этого разделителем и взято
    // двоеточие, а не дефис.
    expect(decodeRubricPath(encodeRubricPath(['а-б']))).toEqual(['а-б']);
    expect(decodeRubricPath(encodeRubricPath(['а', 'б']))).toEqual(['а', 'б']);
  });

  it('нераскодируемое звено роняет весь путь, а не пропускается', () => {
    expect(decodeRubricPath('%zz')).toEqual([]);
    expect(decodeRubricPath('a::b')).toEqual([]);
  });

  it('пустое значение — пустой путь', () => {
    expect(decodeRubricPath('')).toEqual([]);
  });
});
