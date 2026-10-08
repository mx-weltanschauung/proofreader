import { describe, it, expect } from 'vitest';
import { filterOptions, type ComboOption } from './filterOptions';

const VOLUMES: ComboOption[] = [
  { id: 1, label: 'Том 1', group: 'Плеханов', search: 'Плеханов Том 1' },
  { id: 14, label: 'Том 14', group: 'Плеханов', search: 'Плеханов Том 14' },
  { id: 10, label: 'Том 10', group: 'Плеханов', search: 'Плеханов Том 10' },
  { id: 101, label: 'Том 1', group: 'Ленин', search: 'Ленин Том 1' },
  { id: 500, label: 'Отдельная работа', group: 'Отдельные работы' },
];

// Порядок чтения, дерево через глубину: у «Письма первого» и «Письма
// второго» один родитель, «Искусство и общественная жизнь» — отдельный корень.
const CHAPTERS: ComboOption[] = [
  { id: 1, label: 'Письма без адреса', depth: 0 },
  { id: 2, label: 'Письмо первое', depth: 1 },
  { id: 3, label: 'Письмо второе', depth: 1 },
  { id: 4, label: 'Искусство и общественная жизнь', depth: 0 },
  { id: 5, label: 'Ещё о ёмкости', depth: 0 },
];

const ids = (options: ComboOption[]) => options.map((o) => o.id);

describe('filterOptions', () => {
  it('без запроса отдаёт всё в исходном порядке', () => {
    expect(ids(filterOptions(VOLUMES, ''))).toEqual([1, 14, 10, 101, 500]);
  });

  it('каждое слово запроса обязано найтись, регистр не важен', () => {
    expect(ids(filterOptions(VOLUMES, 'плЕХ том'))).toEqual([1, 14, 10]);
  });

  it('число в запросе — целое число, а не начало другого', () => {
    // «том 1» не должен тащить за собой тома 10 и 14.
    expect(ids(filterOptions(VOLUMES, 'плех 1'))).toEqual([1]);
  });

  it('ищет по полю search, а не только по подписи', () => {
    expect(ids(filterOptions(VOLUMES, 'ленин'))).toEqual([101]);
  });

  it('ё и е не различаются ни в запросе, ни в тексте', () => {
    expect(ids(filterOptions(CHAPTERS, 'ещё емкост'))).toEqual([5]);
  });

  it('у совпавшей вложенной главы показывает её родителей', () => {
    expect(ids(filterOptions(CHAPTERS, 'второе'))).toEqual([1, 3]);
  });

  it('не тащит родителя, если совпал сам корень', () => {
    expect(ids(filterOptions(CHAPTERS, 'искусство'))).toEqual([4]);
  });
});
