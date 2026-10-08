import { describe, it, expect } from 'vitest';
import { isTooShortQuery, normalizeQuery, MIN_QUERY_LENGTH } from './searchQuery';

describe('searchQuery', () => {
  it('порог считает знаки, а не байты и не единицы UTF-16', () => {
    expect(isTooShortQuery('и')).toBe(true);
    expect(isTooShortQuery('на')).toBe(false);
    // Один знак вне BMP: у строки .length здесь 2, а знак всё равно один —
    // порог обязан его отклонить.
    expect(isTooShortQuery('𝔞')).toBe(true);
    expect(MIN_QUERY_LENGTH).toBe(2);
  });

  it('края и лишние пробелы не считаются длиной', () => {
    expect(isTooShortQuery('   и   ')).toBe(true);
    expect(isTooShortQuery('')).toBe(true);
    expect(normalizeQuery('  что   делать ')).toBe('что делать');
  });
});
