import { describe, it, expect } from 'vitest';
import { editionStats, splitEditionVolumes } from './editionStats';
import type { VolumeSummary } from '../types';

function volume(over: Partial<VolumeSummary>): VolumeSummary {
  return {
    id: 1,
    title: 'Том',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '2026-01-01T00:00:00Z',
    pages_total: 0,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

describe('editionStats', () => {
  it('складывает тома и страницы по всему собранию', () => {
    const stats = editionStats([
      volume({ id: 1, volume_number: 1, pages_total: 100 }),
      volume({ id: 2, volume_number: 2, pages_total: 300 }),
    ]);

    expect(stats.volumes).toBe(2);
    expect(stats.books).toBe(2);
    expect(stats.pagesTotal).toBe(400);
  });

  it('на пустом собрании не делит на ноль', () => {
    expect(editionStats([])).toEqual({ volumes: 0, books: 0, pagesTotal: 0 });
  });

  it('том без страниц не портит счёт', () => {
    const stats = editionStats([volume({ volume_number: 1, pages_total: 0 })]);
    expect(stats.volumes).toBe(1);
    expect(stats.pagesTotal).toBe(0);
  });

  // У Маркса и Энгельса 25 I—II, 26 I—III и 46 I—II — семь книг на три тома,
  // плюс пробный указатель без номера. По книгам выходило «55 из 50».
  it('считает тома по номерам, а не книги', () => {
    const stats = editionStats([
      volume({ id: 1, volume_number: 25, volume_part: 'I', pages_total: 10 }),
      volume({ id: 2, volume_number: 25, volume_part: 'II', pages_total: 10 }),
      volume({ id: 3, volume_number: 26, volume_part: 'I', pages_total: 10 }),
      volume({ id: 4, volume_number: 26, volume_part: 'II', pages_total: 10 }),
      volume({ id: 5, volume_number: 26, volume_part: 'III', pages_total: 10 }),
      volume({ id: 6, pages_total: 14 }),
    ]);
    expect(stats.volumes).toBe(2);
    expect(stats.books).toBe(6);
    // Страницы — по всем книгам: проба указателя тоже лежит в читальне.
    expect(stats.pagesTotal).toBe(64);
  });
});

describe('splitEditionVolumes', () => {
  it('отделяет предваряющую работу от томов', () => {
    const preface = volume({ id: 99, role: 'edition_front_matter', precedes_volume: 1 });
    const tom1 = volume({ id: 1, volume_number: 1 });
    const tom2 = volume({ id: 2, volume_number: 2 });

    const { prefaces, shelf } = splitEditionVolumes([preface, tom1, tom2]);

    expect(prefaces).toEqual([preface]);
    expect(shelf).toEqual([tom1, tom2]);
  });

  it('без предваряющих работ отдаёт пустой prefaces и весь список в shelf', () => {
    const tom1 = volume({ id: 1, volume_number: 1 });
    const { prefaces, shelf } = splitEditionVolumes([tom1]);

    expect(prefaces).toEqual([]);
    expect(shelf).toEqual([tom1]);
  });
});
