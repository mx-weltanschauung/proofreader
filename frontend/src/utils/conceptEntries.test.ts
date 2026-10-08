import { describe, it, expect } from 'vitest';
import { entryAnchorId, entryAddressLabel, changedLevels } from './conceptEntries';
import type { ConceptEntry } from '../types';

function entry(over: Partial<ConceptEntry> = {}): ConceptEntry {
  return {
    reference_id: 473,
    volume_number: 12,
    printed_start: 730,
    printed_end: 731,
    work_id: 14,
    work_title: 'Экономические рукописи',
    chapter_title: 'Введение',
    chapter_id: null,
    rubric: 'определение',
    rubric_path: ['определение'],
    is_uncertain: false,
    state: 'fragment',
    pages: [
      { page_id: 9143, page_number: 730, printed_page: 730, page_status: 'не_вычитана' },
      { page_id: 9144, page_number: 731, printed_page: 731, page_status: 'не_вычитана' },
    ],
    cuts: [],
    stale_cuts: [],
    ...over,
  };
}

describe('entryAnchorId', () => {
  it('строится по адресу, а не по странице', () => {
    // Два адреса на одну страницу — обычное дело; якорь по странице увёл бы
    // обе ссылки панели в одно место.
    expect(entryAnchorId(473)).toBe('frag-ref-473');
    expect(entryAnchorId(541)).not.toBe(entryAnchorId(473));
  });
});

describe('entryAddressLabel', () => {
  it('печатный диапазон одной страницы не дублируется', () => {
    expect(entryAddressLabel(entry({ printed_start: 730, printed_end: 730 }))).toBe(
      'т. 12 · с. 730',
    );
  });

  it('диапазон показывается через тире', () => {
    expect(entryAddressLabel(entry())).toBe('т. 12 · с. 730—731');
  });

  it('часть тома входит в подпись', () => {
    expect(entryAddressLabel(entry({ volume_number: 25, volume_part: 'II' }))).toBe(
      'т. 25, II · с. 730—731',
    );
  });
});

describe('changedLevels', () => {
  it('первая запись объявляет все уровни своего пути', () => {
    expect(changedLevels(['II съезд', 'значение съезда']).map((h) => [h.level, h.title])).toEqual([
      [0, 'II съезд'],
      [1, 'значение съезда'],
    ]);
  });

  it('смена листа внутри того же съезда печатает только лист', () => {
    expect(
      changedLevels(['II съезд', 'о Бунде'], ['II съезд', 'значение съезда']).map((h) => h.title),
    ).toEqual(['о Бунде']);
  });

  it('смена съезда печатает ОБА уровня, хотя лист совпал', () => {
    // Без этого новый съезд начался бы без объявления: «значение съезда» уже
    // стояло заголовком, и повторять его нечем.
    expect(
      changedLevels(['III съезд', 'значение съезда'], ['II съезд', 'значение съезда']).map(
        (h) => h.title,
      ),
    ).toEqual(['III съезд', 'значение съезда']);
  });

  it('возврат на уровень выше повторяет заголовок раздела', () => {
    // Свои адреса съезда после его аспектов: путь стал короче, но группа
    // сменилась, и без повтора записи легли бы под чужим заголовком.
    expect(changedLevels(['II съезд'], ['II съезд', 'о Бунде']).map((h) => h.title)).toEqual([
      'II съезд',
    ]);
  });

  it('тот же путь заголовка не печатает', () => {
    expect(changedLevels(['II съезд', 'о Бунде'], ['II съезд', 'о Бунде'])).toEqual([]);
  });

  it('пустой путь заголовков не даёт', () => {
    expect(changedLevels([], ['II съезд'])).toEqual([]);
  });
});
