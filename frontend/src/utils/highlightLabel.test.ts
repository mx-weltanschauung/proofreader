import { describe, it, expect } from 'vitest';
import { highlightCoordinate, highlightLabel } from './highlightLabel';

describe('highlightLabel', () => {
  it('ручная подпись в приоритете', () => {
    expect(highlightLabel({ label: 'Капитал, т. I', chapter_title: 'КАПИТАЛ. Критика' })).toBe(
      'Капитал, т. I',
    );
  });

  it('без подписи берёт первое предложение заглавия', () => {
    expect(
      highlightLabel({
        label: '',
        chapter_title: 'Немецкая идеология. Критика новейшей немецкой философии',
      }),
    ).toBe('Немецкая идеология');
  });

  it('подпись из пробелов — это её отсутствие', () => {
    expect(highlightLabel({ label: '   ', chapter_title: 'Нищета философии' })).toBe(
      'Нищета философии',
    );
  });
});

describe('highlightCoordinate', () => {
  it('том, том с частью и том без номера', () => {
    expect(highlightCoordinate({ volume_number: 23, volume_part: null })).toBe('т. 23');
    expect(highlightCoordinate({ volume_number: 26, volume_part: 'III' })).toBe('т. 26, III');
    expect(highlightCoordinate({ volume_number: null, volume_part: null })).toBe('');
  });
});
