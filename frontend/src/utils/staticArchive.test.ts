import { describe, it, expect } from 'vitest';
import { archiveDate, archiveSize } from './staticArchive';

describe('подписи архива', () => {
  it('дата словами, без сдвига по поясу', () => {
    expect(archiveDate('2026-10-06')).toBe('6 октября 2026 г.');
    expect(archiveDate('2027-01-31')).toBe('31 января 2027 г.');
    expect(archiveDate('вчера')).toBe('вчера');
  });

  it('размер в мебибайтах', () => {
    expect(archiveSize(418756548)).toBe('399 МБ');
    expect(archiveSize(1048576)).toBe('1 МБ');
  });
});
