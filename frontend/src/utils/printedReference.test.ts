import { describe, expect, it } from 'vitest';
import type { Edition } from '../types';
import {
  matchingEditions,
  parsePrintedReference,
  printedReferencePage,
  sameVolumePart,
} from './printedReference';

const fifth = {
  id: 1,
  title: 'В. И. Авторов. Полное собрание сочинений, 5-е изд.',
} as Edition;
const second = {
  id: 2,
  title: 'В. И. Авторов. Собрание сочинений, 2-е издание',
} as Edition;
const other = { id: 3, title: 'Е. Н. Другов. Сочинения, 2-е изд.' } as Edition;
const editions = [fifth, second, other];

describe('parsePrintedReference', () => {
  it('разбирает короткую ссылку с указанием издания', () => {
    expect(parsePrintedReference('Сочинения, 5-е изд., т. 4, с. 358')).toEqual({
      source: 'Сочинения, 5-е изд.',
      volume: 4,
      part: undefined,
      page: '358',
    });
  });

  it('разбирает подпись из буфера и начало диапазона', () => {
    expect(
      parsePrintedReference(
        '> Цитата\n\nАвторов. Глава // ПСС, изд. 5, том 26, ч. II, стр. xiv—xvi.\nhttps://example.org/source',
      ),
    ).toMatchObject({ source: 'ПСС, изд. 5', volume: 26, part: 'II', page: 'XIV' });
  });

  it('понимает часть с корешка', () => {
    expect(parsePrintedReference('ПСС т.26·II с.14')).toMatchObject({ volume: 26, part: 'II' });
  });

  it.each([
    'ПСС, т. 4, с. 12а',
    'ПСС, т. 4, с. IIII',
    'ПСС, т. 4, с. 0',
    'ПСС, т. 4, с. 2.5',
    'ПСС, т. 4, с. 99999999999999999999',
    'ПСС, т. 0, с. 12',
    'ПСС, т. 4',
    'т. 4, с. 12',
  ])('отклоняет неполную или неверную ссылку %s', (input) => {
    expect(parsePrintedReference(input)).toBeNull();
  });
});

describe('matchingEditions', () => {
  it('различает издания даже при сокращённом заглавии', () => {
    expect(matchingEditions('Сочинения, 5-е изд.', editions)).toEqual([fifth]);
    expect(matchingEditions('См.: Сочинения, 5 изд.', editions)).toEqual([fifth]);
    expect(matchingEditions('Авторов В. И. Полн. собр. соч.', editions)).toEqual([fifth]);
    expect(matchingEditions('Авторов. Соч., 2 изд.', editions)).toEqual([second]);
    expect(matchingEditions('Авторов. Сочинения, 3-е изд.', editions)).toEqual([]);
  });

  it('показывает совпадающие точные названия при неоднозначности', () => {
    expect(matchingEditions('Сочинения, 2-е изд.', editions)).toEqual([second, other]);
    expect(matchingEditions('Сочинения', editions)).toEqual(editions);
    expect(matchingEditions('Авторов', editions)).toEqual([fifth, second]);
  });

  it('узнаёт аббревиатуру с фамилией и её падежным окончанием', () => {
    expect(matchingEditions('П.С.С. Авторова', editions)).toEqual([fifth]);
    expect(matchingEditions('ПСС, изд. 5', editions)).toEqual([fifth]);
    expect(matchingEditions('Чужой ПСС', editions)).toEqual([]);
  });

  it('не предлагает собрание с условными номерами томов', () => {
    expect(matchingEditions('Сочинения', [{ ...fifth, volume_numbering: 'order' }])).toEqual([]);
  });
});

it('вычисляет порядковый номер по смещению и сверяет стиль колонтитула', () => {
  const roman = { page_offset: -1, numbering_style: 'roman' };
  const arabic = { page_offset: 2, numbering_style: 'arabic' };
  expect(printedReferencePage('IV', roman)).toBe(5);
  expect(printedReferencePage('04', arabic)).toBe(2);
  expect(printedReferencePage('IV', arabic)).toBeNull();
  expect(printedReferencePage('4', roman)).toBeNull();
  expect(printedReferencePage('1', arabic)).toBeNull();
});

it('сопоставляет арабское и римское обозначения части', () => {
  expect(sameVolumePart('II', '2')).toBe(true);
  expect(sameVolumePart('II', 'III')).toBe(false);
  expect(sameVolumePart('II', undefined)).toBe(false);
});
