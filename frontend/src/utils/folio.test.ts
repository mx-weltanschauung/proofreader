import { describe, expect, it } from 'vitest';

import { printedFolio, toRoman } from './folio';

describe('toRoman', () => {
  it('переводит числа книжного диапазона', () => {
    expect(toRoman(1)).toBe('I');
    expect(toRoman(4)).toBe('IV');
    expect(toRoman(7)).toBe('VII');
    expect(toRoman(14)).toBe('XIV');
    expect(toRoman(16)).toBe('XVI');
    expect(toRoman(40)).toBe('XL');
  });
});

describe('printedFolio', () => {
  it('у тома совпадает с page_number: offset 0', () => {
    expect(printedFolio(6, { page_offset: 0, numbering_style: 'arabic' })).toBe('6');
  });

  it('у передних листов даёт римскую с учётом смещения', () => {
    // half 15 тома 5 несёт колонтитул «ПРЕДИСЛОВИЕ РЕДАКТОРА XIV»
    expect(printedFolio(15, { page_offset: -1, numbering_style: 'roman' })).toBe('XIV');
    expect(printedFolio(2, { page_offset: -1, numbering_style: 'roman' })).toBe('I');
  });

  it('возвращает null там, где колонцифры нет', () => {
    // обложка: page_number 1 при offset −1 даёт 0
    expect(printedFolio(1, { page_offset: -1, numbering_style: 'roman' })).toBeNull();
    expect(printedFolio(1, { page_offset: -5, numbering_style: 'arabic' })).toBeNull();
  });

  it('переживает работу без стиля нумерации', () => {
    // старые работы приходят из API без numbering_style
    expect(printedFolio(3, { page_offset: 0 })).toBe('3');
  });
});
