import { describe, expect, it } from 'vitest';
import { countReadingWords, formatReadingDuration } from './readingTime';

describe('countReadingWords', () => {
  it('counts Cyrillic, Latin and numeric words without punctuation', () => {
    expect(countReadingWords('Привет, мир! Marx-Engels 1917\nещё')).toBe(5);
  });

  it('returns zero for whitespace and punctuation', () => {
    expect(countReadingWords(' \n — … ')).toBe(0);
  });
});

describe('formatReadingDuration', () => {
  it('omits empty text and describes subminute reading', () => {
    expect(formatReadingDuration(0)).toBeNull();
    expect(formatReadingDuration(1)).toBe('меньше минуты');
    expect(formatReadingDuration(179)).toBe('меньше минуты');
  });

  it('rounds whole minutes up and uses hours for long chapters', () => {
    expect(formatReadingDuration(180)).toBe('≈ 1 мин');
    expect(formatReadingDuration(181)).toBe('≈ 2 мин');
    expect(formatReadingDuration(10_800)).toBe('≈ 1 ч');
    expect(formatReadingDuration(10_980)).toBe('≈ 1 ч 1 мин');
  });
});
