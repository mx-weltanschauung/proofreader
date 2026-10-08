/**
 * Печатная колонцифра страницы.
 *
 * Конвенция всего проекта: `печатная = page_number + page_offset`. У тома
 * offset нулевой, поэтому номер в адресе, на экране и на скане совпадают.
 * У передних листов нумерация римская и своя: там offset сдвигает счёт на
 * ненумерованную обложку.
 */

const ROMAN: ReadonlyArray<readonly [number, string]> = [
  [1000, 'M'],
  [900, 'CM'],
  [500, 'D'],
  [400, 'CD'],
  [100, 'C'],
  [90, 'XC'],
  [50, 'L'],
  [40, 'XL'],
  [10, 'X'],
  [9, 'IX'],
  [5, 'V'],
  [4, 'IV'],
  [1, 'I'],
];

export function toRoman(n: number): string {
  let rest = n;
  let out = '';
  for (const [value, sign] of ROMAN) {
    while (rest >= value) {
      out += sign;
      rest -= value;
    }
  }
  return out;
}

/** Минимум полей работы, нужный для расчёта колонцифры. */
export interface FolioSource {
  page_offset: number;
  numbering_style?: string;
}

/**
 * Печатный номер страницы, или null, если колонцифры у неё нет: обложка и
 * форзацы стоят вне счёта и дают ноль или отрицательное.
 */
export function printedFolio(pageNumber: number, work: FolioSource): string | null {
  const n = pageNumber + work.page_offset;
  if (n < 1) {
    return null;
  }
  return work.numbering_style === 'roman' ? toRoman(n) : String(n);
}
