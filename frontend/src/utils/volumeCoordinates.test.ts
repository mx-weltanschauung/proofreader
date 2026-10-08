import { describe, it, expect } from 'vitest';
import type { Work } from '../types';
import { volumeCoordinates } from './volumeCoordinates';

describe('volumeCoordinates', () => {
  it('подписывает том, полутом и работу без номера', () => {
    expect(volumeCoordinates({ volume_number: 4 } as Work)).toBe('т. 4');
    expect(volumeCoordinates({ volume_number: 25, volume_part: 'I' } as Work)).toBe('т. 25, I');
    // Работа без номера тома — справочный том собрания, например указатель.
    expect(volumeCoordinates({} as Work)).toBe('справочный том');
  });
});
