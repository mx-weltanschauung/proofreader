export interface Neighbours {
  prev: number | null;
  next: number | null;
  /** Порядковый номер страницы в работе, 1-based; 0 — страницы нет в карте. */
  index: number;
  total: number;
}

/**
 * Соседние страницы берутся из карты, а не как `n ± 1`: нумерация страниц
 * работы бывает с пропусками, и слепой инкремент увёл бы в 404.
 */
export function pageNeighbours(numbers: number[], current: number): Neighbours {
  const at = numbers.indexOf(current);
  if (at === -1) return { prev: null, next: null, index: 0, total: numbers.length };
  return {
    prev: at > 0 ? numbers[at - 1] : null,
    next: at < numbers.length - 1 ? numbers[at + 1] : null,
    index: at + 1,
    total: numbers.length,
  };
}
