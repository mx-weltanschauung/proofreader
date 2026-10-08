import { describe, it, expect } from 'vitest';
import type { Chapter } from '../types';
import { parsePrintedPage, resolvePageJump } from './pageJump';

const VOLUME = { page_offset: 0, numbering_style: 'arabic' };
// Передние листы: обложка вне счёта, первая полоса — печатная «I».
const FRONT = { page_offset: -1, numbering_style: 'roman' };

function chapter(
  id: number,
  start: number,
  end: number,
  order: number,
  children: Chapter[] = [],
): Chapter {
  return {
    id,
    work_id: 1,
    title: `Глава ${id}`,
    start_page: start,
    end_page: end,
    order_number: order,
    children,
  } as Chapter;
}

// Работа 1 (10—40) с двумя подглавами (10—20 и 20—40: полоса 20 стыковая),
// работа 2 (41—60), полосы 1—9 и 61+ не накрыты ничем.
const TREE = [
  chapter(1, 10, 40, 1, [chapter(11, 10, 20, 1), chapter(12, 20, 40, 2)]),
  chapter(2, 41, 60, 2),
];

describe('parsePrintedPage', () => {
  it('у тома печатный номер и есть номер полосы', () => {
    expect(parsePrintedPage('123', VOLUME)).toBe(123);
  });

  it('терпит пробелы вокруг и приставку «с.»/«стр.»', () => {
    expect(parsePrintedPage('  45 ', VOLUME)).toBe(45);
    expect(parsePrintedPage('с. 45', VOLUME)).toBe(45);
    expect(parsePrintedPage('стр.45', VOLUME)).toBe(45);
  });

  it('у передних листов пересчитывает печатный номер через сдвиг', () => {
    expect(parsePrintedPage('3', FRONT)).toBe(4);
  });

  it('понимает римскую колонцифру в любом регистре', () => {
    expect(parsePrintedPage('iii', FRONT)).toBe(4);
    expect(parsePrintedPage('XIV', FRONT)).toBe(15);
  });

  it('отказывает тому, что номером не является', () => {
    expect(parsePrintedPage('', VOLUME)).toBeNull();
    expect(parsePrintedPage('abc', VOLUME)).toBeNull();
    expect(parsePrintedPage('12a', VOLUME)).toBeNull();
    expect(parsePrintedPage('IIII', FRONT)).toBeNull();
    expect(parsePrintedPage('1.5', VOLUME)).toBeNull();
  });

  it('отказывает номеру, который уходит за начало работы', () => {
    expect(parsePrintedPage('0', VOLUME)).toBeNull();
    expect(parsePrintedPage('0', FRONT)).toBeNull();
  });
});

describe('resolvePageJump', () => {
  it('полоса уже на экране — прокрутка', () => {
    expect(resolvePageJump(15, TREE, (n) => n >= 10 && n <= 40)).toEqual({
      kind: 'here',
      pageNumber: 15,
    });
  });

  it('полоса вне открытого — самая узкая накрывающая глава', () => {
    expect(resolvePageJump(15, TREE, () => false)).toMatchObject({
      kind: 'chapter',
      pageNumber: 15,
      chapter: { id: 11 },
    });
  });

  it('на стыке сестёр — та, что на полосе начинается', () => {
    expect(resolvePageJump(20, TREE, () => false)).toMatchObject({ chapter: { id: 12 } });
  });

  it('глава без подглав — она сама', () => {
    expect(resolvePageJump(50, TREE, () => false)).toMatchObject({ chapter: { id: 2 } });
  });

  it('полоса вне всех глав — не накрыта', () => {
    expect(resolvePageJump(5, TREE, () => false)).toEqual({ kind: 'uncovered', pageNumber: 5 });
    expect(resolvePageJump(70, TREE, () => false)).toEqual({ kind: 'uncovered', pageNumber: 70 });
  });

  it('загруженная полоса вне глав — всё равно прокрутка', () => {
    expect(resolvePageJump(5, TREE, (n) => n === 5)).toEqual({ kind: 'here', pageNumber: 5 });
  });
});
