import type { VolumeSummary } from '../types';

export interface EditionVolumes {
  /** Предисловия ко всему собранию или к его части — без номера тома. */
  prefaces: VolumeSummary[];
  /** Собственно тома — то, что действительно попадает на полку и в счётчик. */
  shelf: VolumeSummary[];
}

/**
 * Делит ответ `/editions/{id}/volumes` на предваряющие работы
 * (`role === 'edition_front_matter'`) и тома. Предваряющая работа не несёт
 * номера тома и не должна попадать ни в сводку (`editionStats`), ни на полку
 * корешков (`VolumeShelf`) — иначе счётчик собрания Маркса и Энгельса
 * посчитал бы «52 тома» вместо 50, а обрезанная полка на главной вытеснила
 * бы два настоящих тома двумя предисловиями. Единственная точка этого
 * разделения — используется и на странице собрания, и на главной.
 */
export function splitEditionVolumes(volumes: VolumeSummary[]): EditionVolumes {
  const prefaces = volumes.filter((v) => v.role === 'edition_front_matter');
  const shelf = volumes.filter((v) => v.role !== 'edition_front_matter');
  return { prefaces, shelf };
}

export interface EditionStats {
  /**
   * Различные номера томов. Книги-части одного тома (26 I—III) — один том,
   * книга без номера (пробный указатель) — ни одного: иначе у Маркса и
   * Энгельса выходило «55 из 50».
   */
  volumes: number;
  /** Все книги, с частями и без номера. */
  books: number;
  pagesTotal: number;
}

/** Сводка по собранию: сколько томов и книг, сколько страниц. */
export function editionStats(volumes: VolumeSummary[]): EditionStats {
  const numbers = new Set<number>();
  let pagesTotal = 0;
  for (const volume of volumes) {
    pagesTotal += volume.pages_total;
    if (volume.volume_number !== undefined && volume.volume_number !== null) {
      numbers.add(volume.volume_number);
    }
  }
  return { volumes: numbers.size, books: volumes.length, pagesTotal };
}
