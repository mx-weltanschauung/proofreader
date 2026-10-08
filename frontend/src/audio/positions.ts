/** Память места у каждой дорожки: читатель закрыл вкладку на 4:51 — назавтра
 *  ▶ у той же дорожки играет с 4:51. Живёт в localStorage одного браузера;
 *  в приватном режиме хранилище бросает — тогда памяти просто нет. */

const STORAGE_KEY = 'audio-pos';

/** Больше записей не держим: вытесняются самые давние. */
export const POSITION_LIMIT = 200;

/** Последние секунды дорожки — уже «дослушано»: место забывается. */
export const FINISHED_TAIL_S = 5;

interface Entry {
  /** Секунды от начала дорожки. */
  s: number;
  /** Когда записано (мс), для вытеснения давних. */
  t: number;
}

type Positions = Record<string, Entry>;

function read(): Positions {
  try {
    const raw: unknown = JSON.parse(localStorage.getItem(STORAGE_KEY) ?? '{}');
    if (!raw || typeof raw !== 'object' || Array.isArray(raw)) return {};
    const out: Positions = {};
    for (const [key, value] of Object.entries(raw as Record<string, unknown>)) {
      const e = value as Partial<Entry> | null;
      if (
        e &&
        typeof e.s === 'number' &&
        Number.isFinite(e.s) &&
        e.s > 0 &&
        typeof e.t === 'number'
      ) {
        out[key] = { s: e.s, t: e.t };
      }
    }
    return out;
  } catch {
    return {};
  }
}

function write(positions: Positions): void {
  try {
    localStorage.setItem(STORAGE_KEY, JSON.stringify(positions));
  } catch {
    // Приватный режим или переполнение: место не запомнится, звук играет.
  }
}

/** Запомненное место дорожки (секунды) или null. */
export function savedPosition(key: string): number | null {
  return read()[key]?.s ?? null;
}

export function forgetPosition(key: string): void {
  const positions = read();
  if (!(key in positions)) return;
  delete positions[key];
  write(positions);
}

/** Запомнить место. Первая секунда не пишется: до загрузки файла позиция — 0,
 *  и пауза в этот миг стёрла бы место, запомненное вчера. Хвост дорожки —
 *  дослушано: место забывается, следующий ▶ играет с начала. */
export function savePosition(
  key: string,
  seconds: number,
  duration: number,
  now = Date.now(),
): void {
  if (!Number.isFinite(seconds) || seconds < 1) return;
  if (Number.isFinite(duration) && duration > 0 && seconds >= duration - FINISHED_TAIL_S) {
    forgetPosition(key);
    return;
  }
  const positions = read();
  positions[key] = { s: seconds, t: now };
  const keys = Object.keys(positions);
  if (keys.length > POSITION_LIMIT) {
    keys
      .sort((a, b) => positions[a].t - positions[b].t)
      .slice(0, keys.length - POSITION_LIMIT)
      .forEach((k) => delete positions[k]);
  }
  write(positions);
}
