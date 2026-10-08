import { describe, it, expect } from 'vitest';
import { pageNeighbours } from './pageNeighbours';

describe('pageNeighbours', () => {
  it('находит соседей и место в работе', () => {
    expect(pageNeighbours([1, 2, 3, 4], 3)).toEqual({
      prev: 2,
      next: 4,
      index: 3,
      total: 4,
    });
  });

  it('на краях диапазона соседа нет', () => {
    expect(pageNeighbours([1, 2, 3], 1).prev).toBeNull();
    expect(pageNeighbours([1, 2, 3], 3).next).toBeNull();
  });

  // Слепой инкремент упёрся бы в 404: в нумерации бывают пропуски.
  it('перешагивает через пропуск в нумерации', () => {
    expect(pageNeighbours([1, 2, 7, 8], 2).next).toBe(7);
    expect(pageNeighbours([1, 2, 7, 8], 7).prev).toBe(2);
  });

  it('на неизвестной странице ничего не выдумывает', () => {
    expect(pageNeighbours([1, 2, 3], 99)).toEqual({
      prev: null,
      next: null,
      index: 0,
      total: 3,
    });
  });

  it('на пустой карте молчит', () => {
    expect(pageNeighbours([], 1)).toEqual({ prev: null, next: null, index: 0, total: 0 });
  });
});
