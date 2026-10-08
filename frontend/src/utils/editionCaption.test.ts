import { describe, it, expect } from 'vitest';
import type { VolumeSummary } from '../types';
import { editionCaption } from './editionCaption';

function volume(over: Partial<VolumeSummary> = {}): VolumeSummary {
  return {
    id: 1,
    title: 'том',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '2026-01-01',
    volume_number: 1,
    pages_total: 600,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

describe('editionCaption', () => {
  it('называет оба числа, когда план собрания известен', () => {
    const volumes = [volume({ volume_number: 1 }), volume({ id: 2, volume_number: 2 })];
    expect(editionCaption(volumes, 55)).toBe('2 из 55 томов, 1 200 страниц.');
  });

  it('называет одно число, когда план неизвестен', () => {
    expect(editionCaption([volume()], undefined)).toBe('1 том, 600 страниц.');
  });

  it('разделяет тысячи в числе страниц неразрывным пробелом', () => {
    // 27719 страниц читается «двадцать семь тысяч», только если разделено;
    // пробел неразрывный, иначе число переносится посреди себя.
    expect(editionCaption([volume({ pages_total: 27719 })], undefined)).toContain('27 719');
  });

  it('не называет последнего загруженного тома', () => {
    const volumes = [
      volume({ id: 1, volume_number: 24, updated_at: '2026-09-01' }),
      volume({ id: 2, volume_number: 7, updated_at: '2026-09-03' }),
    ];
    expect(editionCaption(volumes, 50)).not.toMatch(/Последний/);
  });

  it('у собрания без томов называет план', () => {
    expect(editionCaption([], 24)).toBe('Пока ни одного тома из 24.');
  });

  it('у пустого собрания без плана говорит просто, что томов нет', () => {
    expect(editionCaption([], undefined)).toBe('Пока ни одного тома.');
  });

  // Собрание, тома которого заведены без координат: книги есть, номеров нет.
  // «Пока ни одного тома» было бы неправдой.
  it('без номеров называет книги, а не ноль томов', () => {
    const volumes = [
      volume({ id: 1, volume_number: undefined }),
      volume({ id: 2, volume_number: undefined }),
    ];
    expect(editionCaption(volumes, undefined)).toBe('2 книги, 1 200 страниц.');
  });
});
