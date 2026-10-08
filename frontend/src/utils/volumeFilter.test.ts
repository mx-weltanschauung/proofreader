import { describe, expect, it } from 'vitest';

import { parseVolumeFilter, volumeFilterValue } from './volumeFilter';

describe('parseVolumeFilter', () => {
  it('читает номер тома', () => {
    expect(parseVolumeFilter('12')).toEqual({ volume: 12 });
  });

  it('читает том с частью: 25 и 26 выходили в нескольких книгах', () => {
    expect(parseVolumeFilter('25-II')).toEqual({ volume: 25, part: 'II' });
  });

  it('на пустой строке и мусоре не фильтрует', () => {
    expect(parseVolumeFilter('')).toBeNull();
    expect(parseVolumeFilter('том двенадцатый')).toBeNull();
    expect(parseVolumeFilter('-II')).toBeNull();
    expect(parseVolumeFilter('0')).toBeNull();
  });
});

describe('volumeFilterValue', () => {
  it('собирает значение обратно', () => {
    expect(volumeFilterValue(12)).toBe('12');
    expect(volumeFilterValue(25, 'II')).toBe('25-II');
  });
});
