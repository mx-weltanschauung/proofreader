import { HINT_IDS, type HintId } from './registry';

export const HINTS_KEY = 'reader.hints.v1';

/** После стольких показов выноска считается усвоенной и больше не приходит. */
export const SEEN_LIMIT = 3;

export interface HintState {
  seen: number;
  done: boolean;
}

export type HintsState = Partial<Record<HintId, HintState>>;

const KNOWN = new Set<string>(HINT_IDS);

/**
 * Чтение состояния подсказок.
 *
 * Любая беда — запрет хранилища, приватное окно, битый JSON, чужая запись под
 * тем же ключом — даёт пустое состояние, а не исключение: выноски удобство,
 * ради которого не роняют чтение (тот же приём, что в utils/volumeViewPref.ts).
 * Чтение идёт из инициализатора состояния провайдера, то есть во время рендера.
 */
export function readHints(): HintsState {
  let raw: string | null;
  try {
    raw = localStorage.getItem(HINTS_KEY);
  } catch {
    return {};
  }
  if (!raw) return {};

  let parsed: unknown;
  try {
    parsed = JSON.parse(raw);
  } catch {
    return {};
  }
  if (typeof parsed !== 'object' || parsed === null || Array.isArray(parsed)) return {};

  const state: HintsState = {};
  for (const [id, value] of Object.entries(parsed as Record<string, unknown>)) {
    if (!KNOWN.has(id)) continue;
    if (typeof value !== 'object' || value === null) continue;
    const { seen, done } = value as { seen?: unknown; done?: unknown };
    state[id as HintId] = {
      seen: typeof seen === 'number' && Number.isFinite(seen) && seen > 0 ? Math.floor(seen) : 0,
      done: done === true,
    };
  }
  return state;
}

export function writeHints(state: HintsState): void {
  try {
    localStorage.setItem(HINTS_KEY, JSON.stringify(state));
  } catch {
    // Хранилище недоступно или переполнено: показ выноски не та цена, ради
    // которой стоит ронять экран.
  }
}

export function clearHints(): void {
  try {
    localStorage.removeItem(HINTS_KEY);
  } catch {
    // См. writeHints.
  }
}

export function isLearned(state: HintsState, id: HintId): boolean {
  const entry = state[id];
  if (!entry) return false;
  return entry.done || entry.seen >= SEEN_LIMIT;
}

export function markSeen(state: HintsState, id: HintId): HintsState {
  const entry = state[id];
  return { ...state, [id]: { seen: (entry?.seen ?? 0) + 1, done: entry?.done ?? false } };
}

export function markDone(state: HintsState, id: HintId): HintsState {
  return { ...state, [id]: { seen: state[id]?.seen ?? 0, done: true } };
}
