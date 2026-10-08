import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import {
  FINISHED_TAIL_S,
  POSITION_LIMIT,
  forgetPosition,
  savePosition,
  savedPosition,
} from './positions';

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

describe('память позиций', () => {
  it('запоминает и отдаёт место дорожки', () => {
    savePosition('track:3', 291.5, 760);
    expect(savedPosition('track:3')).toBe(291.5);
    expect(savedPosition('track:4')).toBeNull();
  });

  // До загрузки файла позиция 0: пауза в этот миг не должна стирать вчерашнее.
  it('первая секунда не перезаписывает запомненное', () => {
    savePosition('track:3', 291.5, 760);
    savePosition('track:3', 0.4, 760);
    expect(savedPosition('track:3')).toBe(291.5);
  });

  it('хвост дорожки — дослушано: место забывается', () => {
    savePosition('track:3', 291.5, 760);
    savePosition('track:3', 760 - FINISHED_TAIL_S + 0.5, 760);
    expect(savedPosition('track:3')).toBeNull();
  });

  it('длительность неизвестна (NaN) — место пишется как есть', () => {
    savePosition('track:3', 700, Number.NaN);
    expect(savedPosition('track:3')).toBe(700);
  });

  it('forgetPosition стирает одну дорожку', () => {
    savePosition('track:3', 10, 100);
    savePosition('track:4', 20, 100);
    forgetPosition('track:3');
    expect(savedPosition('track:3')).toBeNull();
    expect(savedPosition('track:4')).toBe(20);
  });

  it(`больше ${POSITION_LIMIT} записей — вытесняется самая давняя`, () => {
    for (let i = 0; i <= POSITION_LIMIT; i++) savePosition(`track:${i}`, 10, 100, 1000 + i);
    expect(savedPosition('track:0')).toBeNull();
    expect(savedPosition('track:1')).toBe(10);
    expect(savedPosition(`track:${POSITION_LIMIT}`)).toBe(10);
  });

  it('мусор в хранилище — памяти нет, без падения', () => {
    localStorage.setItem('audio-pos', '{не json');
    expect(savedPosition('track:3')).toBeNull();
    localStorage.setItem(
      'audio-pos',
      JSON.stringify({ 'track:3': { s: 'x', t: 1 }, 'track:4': [] }),
    );
    expect(savedPosition('track:3')).toBeNull();
    savePosition('track:5', 12, 100);
    expect(savedPosition('track:5')).toBe(12);
  });

  it('хранилище недоступно — ни чтение, ни запись не бросают', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('SecurityError');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('SecurityError');
    });
    expect(() => savePosition('track:3', 12, 100)).not.toThrow();
    expect(savedPosition('track:3')).toBeNull();
  });
});
