import { describe, it, expect, beforeEach, vi, afterEach } from 'vitest';
import {
  HINTS_KEY,
  SEEN_LIMIT,
  readHints,
  writeHints,
  clearHints,
  isLearned,
  markSeen,
  markDone,
} from './hintsStorage';

beforeEach(() => localStorage.clear());
afterEach(() => vi.restoreAllMocks());

describe('хранилище подсказок', () => {
  it('пустое хранилище читается как пустое состояние', () => {
    expect(readHints()).toEqual({});
  });

  it('записанное состояние читается обратно', () => {
    writeHints({ 'page-numbers': { seen: 2, done: false } });
    expect(readHints()).toEqual({ 'page-numbers': { seen: 2, done: false } });
  });

  // Под ключом может оказаться что угодно: чужое расширение, прошлая версия,
  // оборванная запись. Чтение идёт при первом рендере провайдера — исключение
  // отсюда обрушило бы весь макет.
  it('битый JSON не роняет чтение', () => {
    localStorage.setItem(HINTS_KEY, '{не json');
    expect(readHints()).toEqual({});
  });

  it('не объект под ключом читается как пустое состояние', () => {
    localStorage.setItem(HINTS_KEY, '[1,2,3]');
    expect(readHints()).toEqual({});
  });

  it('незнакомые идентификаторы отбрасываются', () => {
    localStorage.setItem(
      HINTS_KEY,
      JSON.stringify({
        'page-numbers': { seen: 1, done: false },
        'нет-такой': { seen: 9, done: true },
      }),
    );
    expect(readHints()).toEqual({ 'page-numbers': { seen: 1, done: false } });
  });

  it('мусор в полях приводится к безопасным значениям', () => {
    localStorage.setItem(HINTS_KEY, JSON.stringify({ download: { seen: 'много', done: 'да' } }));
    expect(readHints()).toEqual({ download: { seen: 0, done: false } });
  });

  it('запрет хранилища не роняет ни чтение, ни запись', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('доступ запрещён');
    });
    vi.spyOn(Storage.prototype, 'setItem').mockImplementation(() => {
      throw new Error('доступ запрещён');
    });
    expect(readHints()).toEqual({});
    expect(() => writeHints({ download: { seen: 1, done: false } })).not.toThrow();
  });

  it('clearHints стирает ключ', () => {
    writeHints({ download: { seen: 1, done: false } });
    clearHints();
    expect(localStorage.getItem(HINTS_KEY)).toBeNull();
  });

  it('усвоенной считается закрытая крестиком или показанная трижды', () => {
    expect(isLearned({}, 'download')).toBe(false);
    expect(isLearned({ download: { seen: 0, done: true } }, 'download')).toBe(true);
    expect(isLearned({ download: { seen: SEEN_LIMIT, done: false } }, 'download')).toBe(true);
    expect(isLearned({ download: { seen: SEEN_LIMIT - 1, done: false } }, 'download')).toBe(false);
  });

  it('markSeen считает показы, markDone закрывает подсказку насовсем', () => {
    let state = markSeen({}, 'download');
    expect(state).toEqual({ download: { seen: 1, done: false } });
    state = markSeen(state, 'download');
    expect(state.download).toEqual({ seen: 2, done: false });
    state = markDone(state, 'download');
    expect(state.download).toEqual({ seen: 2, done: true });
  });
});
