import { describe, expect, it } from 'vitest';

import type { PageStatus } from '../types';
import { pageStatusLabel, readerStatusLabel } from './pageStatusLabel';

describe('readerStatusLabel', () => {
  it('молчит только о вычитанной человеком странице', () => {
    expect(readerStatusLabel('вычитана')).toBe('');
  });

  it('называет всё, что человеком не вычитано', () => {
    expect(readerStatusLabel('не_вычитана')).toBe('не вычитана');
    expect(readerStatusLabel('вычитано_машиной')).toBe('вычитано машиной');
    expect(readerStatusLabel('требует_внимания')).toBe('требует внимания');
    expect(readerStatusLabel('есть_проблемы')).toBe('есть проблемы');
    expect(readerStatusLabel('вычитывается')).toBe('вычитывается');
    expect(readerStatusLabel('пустая_страница')).toBe('пустая страница');
  });

  it('незнакомое значение не роняет подпись', () => {
    expect(readerStatusLabel('нечто_иное' as PageStatus)).toBe('нечто иное');
  });
});

describe('pageStatusLabel', () => {
  // В отличие от readerStatusLabel эта подпись — для карточки страницы, где
  // молчание про «вычитана» читалось бы как пропавший статус, а не как
  // одобрение.
  it('называет статус «вычитана», а не молчит о нём', () => {
    expect(pageStatusLabel('вычитана')).toBe('вычитана');
  });

  it('называет остальные статусы заменой подчёркиваний на пробелы', () => {
    expect(pageStatusLabel('не_вычитана')).toBe('не вычитана');
    expect(pageStatusLabel('вычитано_машиной')).toBe('вычитано машиной');
    expect(pageStatusLabel('требует_внимания')).toBe('требует внимания');
  });
});
