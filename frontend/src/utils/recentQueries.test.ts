import { describe, it, expect, beforeEach, vi } from 'vitest';
import { readRecentQueries, rememberQuery, clearRecentQueries } from './recentQueries';

describe('recentQueries', () => {
  beforeEach(() => localStorage.clear());

  it('последний запрос идёт первым', () => {
    rememberQuery('партия');
    rememberQuery('стоимость');
    expect(readRecentQueries()).toEqual(['стоимость', 'партия']);
  });

  it('повтор поднимается наверх, а не задваивается', () => {
    rememberQuery('партия');
    rememberQuery('стоимость');
    rememberQuery('партия');
    expect(readRecentQueries()).toEqual(['партия', 'стоимость']);
  });

  it('хранится не больше десяти', () => {
    for (let i = 0; i < 15; i++) rememberQuery(`запрос ${i}`);
    expect(readRecentQueries()).toHaveLength(10);
  });

  it('очистка опустошает список', () => {
    rememberQuery('партия');
    clearRecentQueries();
    expect(readRecentQueries()).toEqual([]);
  });

  it('недоступное хранилище не роняет чтение', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('приватное окно');
    });
    expect(readRecentQueries()).toEqual([]);
    vi.restoreAllMocks();
  });

  it('мусор в хранилище не роняет чтение', () => {
    localStorage.setItem('reading-room.recent-queries', '{не json');
    expect(readRecentQueries()).toEqual([]);
  });
});
