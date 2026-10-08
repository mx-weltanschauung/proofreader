import { create } from 'zustand';
import { forgetPosition, savePosition, savedPosition } from './positions';
import type { QueueItem } from './queue';
import { currentSiteName } from '../services/site';

/** Скорости столбика в полосе. Шаг 0,25 — привычный по проигрывателям книг. */
export const PLAYBACK_RATES = [0.75, 1, 1.25, 1.5, 1.75, 2] as const;

/** ⏮ дальше трёх секунд от начала — в начало дорожки, иначе предыдущая. */
export const PREV_RESTART_S = 3;

/** Шаг перемотки с экрана блокировки и наушников. */
export const SKIP_S = 15;

/** Как часто во время игры записывается место (секунды звука). */
const SAVE_EVERY_S = 5;

const RATE_KEY = 'audio-rate';

export type PlayerStatus = 'idle' | 'loading' | 'playing' | 'paused' | 'ended' | 'error';

export interface PlayerState {
  queue: QueueItem[];
  /** -1 — очередь пуста. */
  index: number;
  status: PlayerStatus;
  /** Секунды. */
  position: number;
  duration: number;
  rate: number;
  playQueue: (items: QueueItem[], start: number) => void;
  toggle: () => void;
  seek: (seconds: number) => void;
  next: () => void;
  prev: () => void;
  setRate: (rate: number) => void;
  close: () => void;
  retry: () => void;
}

function readRate(): number {
  try {
    const v = Number(localStorage.getItem(RATE_KEY));
    return (PLAYBACK_RATES as readonly number[]).includes(v) ? v : 1;
  } catch {
    return 1;
  }
}

function writeRate(rate: number): void {
  try {
    localStorage.setItem(RATE_KEY, String(rate));
  } catch {
    // Приватный режим: скорость живёт до перезагрузки.
  }
}

// Единственный звук читальни — вне React: смена страницы его не трогает.
let audio: HTMLAudioElement | null = null;
// Место, с которого начать, когда придут метаданные (до них currentTime не ставится).
let pendingSeek = 0;
// Позиция последней записи места — чтобы писать раз в SAVE_EVERY_S.
let lastSaved = 0;

function element(): HTMLAudioElement {
  if (!audio) setAudioElement(new Audio());
  return audio as HTMLAudioElement;
}

function current(): QueueItem | null {
  const s = usePlayer.getState();
  return s.queue[s.index] ?? null;
}

/** Записать место текущей дорожки. До метаданных currentTime — 0 (новый src
 *  его сбрасывает), а меньше секунды savePosition не пишет: вчерашнее место
 *  загрузка не сотрёт. */
function remember(): void {
  const item = current();
  if (!item || !audio) return;
  const duration = Number.isFinite(audio.duration) ? audio.duration : item.durationMs / 1000;
  savePosition(item.key, audio.currentTime, duration);
}

function mediaSession(): MediaSession | null {
  return typeof navigator !== 'undefined' && 'mediaSession' in navigator
    ? navigator.mediaSession
    : null;
}

function showMetadata(item: QueueItem | null): void {
  const ms = mediaSession();
  if (!ms) return;
  if (!item || typeof MediaMetadata === 'undefined') {
    ms.metadata = null;
    return;
  }
  ms.metadata = new MediaMetadata({
    title: item.title,
    artist: item.subtitle,
    album: currentSiteName(),
  });
}

function syncPositionState(): void {
  const ms = mediaSession();
  if (!ms?.setPositionState || !audio) return;
  const duration = audio.duration;
  if (!Number.isFinite(duration) || duration <= 0) return;
  try {
    ms.setPositionState({
      duration,
      playbackRate: audio.playbackRate,
      position: Math.min(audio.currentTime, duration),
    });
  } catch {
    // Браузер отверг значения — экран блокировки покажет без позиции.
  }
}

function bindMediaSession(): void {
  const ms = mediaSession();
  if (!ms) return;
  const p = () => usePlayer.getState();
  const actions: [MediaSessionAction, MediaSessionActionHandler][] = [
    // Во время загрузки звук уже «играет» — toggle() поставил бы паузу.
    ['play', () => p().status !== 'playing' && p().status !== 'loading' && p().toggle()],
    ['pause', () => (p().status === 'playing' || p().status === 'loading') && p().toggle()],
    ['previoustrack', () => p().prev()],
    ['nexttrack', () => p().next()],
    ['seekto', (d) => d.seekTime !== undefined && p().seek(d.seekTime)],
    ['seekbackward', (d) => p().seek(Math.max(0, p().position - (d.seekOffset ?? SKIP_S)))],
    [
      'seekforward',
      (d) => p().seek(Math.min(p().duration, p().position + (d.seekOffset ?? SKIP_S))),
    ],
  ];
  for (const [action, handler] of actions) {
    try {
      ms.setActionHandler(action, handler);
    } catch {
      // Действие этим браузером не поддерживается.
    }
  }
}

/** play() отклоняется, когда загрузку прервала новая (AbortError) — это не
 *  сбой. Иное (запрет автозапуска на iOS и т. п.) — стоим на паузе, а не
 *  «грузимся» вечно. Ошибку самого файла сообщает событие error. */
function onPlayRejected(err: unknown): void {
  if ((err as { name?: string } | null)?.name === 'AbortError') return;
  if (usePlayer.getState().status === 'loading') usePlayer.setState({ status: 'paused' });
}

function load(i: number): void {
  const s = usePlayer.getState();
  const item = s.queue[i];
  if (!item) return;
  const a = element();
  const start = savedPosition(item.key) ?? 0;
  pendingSeek = start;
  lastSaved = start;
  usePlayer.setState({
    index: i,
    status: 'loading',
    position: start,
    duration: item.durationMs / 1000,
  });
  a.src = item.url;
  a.defaultPlaybackRate = s.rate;
  a.playbackRate = s.rate;
  showMetadata(item);
  void a.play().catch(onPlayRejected);
}

/** Привязать звук к хранилищу. Вызывается один раз на элемент; тесты
 *  подставляют поддельный элемент (jsdom не умеет play()). */
export function setAudioElement(a: HTMLAudioElement): void {
  audio = a;
  a.preload = 'auto';
  const set = usePlayer.setState;
  a.addEventListener('loadedmetadata', () => {
    if (pendingSeek > 0) {
      a.currentTime = pendingSeek;
      pendingSeek = 0;
    }
    if (Number.isFinite(a.duration)) set({ duration: a.duration });
    syncPositionState();
  });
  a.addEventListener('timeupdate', () => {
    set({ position: a.currentTime });
    if (Math.abs(a.currentTime - lastSaved) >= SAVE_EVERY_S) {
      lastSaved = a.currentTime;
      remember();
    }
  });
  a.addEventListener('playing', () => set({ status: 'playing' }));
  a.addEventListener('waiting', () => {
    if (usePlayer.getState().status === 'playing') set({ status: 'loading' });
  });
  // Пауза не нашими руками: наушники вынули, система забрала звук. И во время
  // загрузки тоже — иначе крутилка вертелась бы над остановленным звуком.
  // Наша пауза (toggle) ставит paused раньше, смена src события pause не шлёт.
  a.addEventListener('pause', () => {
    const status = usePlayer.getState().status;
    if (status !== 'playing' && status !== 'loading') return;
    remember();
    set({ status: 'paused' });
  });
  a.addEventListener('ended', () => {
    const s = usePlayer.getState();
    const item = s.queue[s.index];
    if (item) forgetPosition(item.key);
    if (s.index < s.queue.length - 1) load(s.index + 1);
    else set({ status: 'ended', position: s.duration });
  });
  // После close() снятый src тоже даёт error — полосы уже нет, молчим.
  a.addEventListener('error', () => {
    if (usePlayer.getState().queue.length === 0) return;
    set({ status: 'error' });
  });
  window.addEventListener('pagehide', remember);
  bindMediaSession();
}

export const usePlayer = create<PlayerState>()((set, get) => ({
  queue: [],
  index: -1,
  status: 'idle',
  position: 0,
  duration: 0,
  rate: readRate(),

  playQueue: (items, start) => {
    if (!items[start]) return;
    remember();
    set({ queue: items });
    load(start);
  },

  toggle: () => {
    const s = get();
    if (s.index < 0) return;
    const a = element();
    if (s.status === 'playing' || s.status === 'loading') {
      a.pause();
      set({ status: 'paused' });
      remember();
      return;
    }
    if (s.status === 'ended') {
      forgetPosition(s.queue[s.index].key);
      load(s.index);
      return;
    }
    if (s.status === 'error') {
      load(s.index);
      return;
    }
    set({ status: 'loading' });
    void a.play().catch(onPlayRejected);
  },

  seek: (seconds) => {
    if (get().index < 0) return;
    const a = element();
    a.currentTime = seconds;
    set({ position: seconds });
    syncPositionState();
  },

  next: () => {
    const s = get();
    if (s.index < 0 || s.index >= s.queue.length - 1) return;
    remember();
    load(s.index + 1);
  },

  prev: () => {
    const s = get();
    if (s.index < 0) return;
    if (s.position > PREV_RESTART_S || s.index === 0) {
      get().seek(0);
      return;
    }
    remember();
    load(s.index - 1);
  },

  setRate: (rate) => {
    writeRate(rate);
    if (audio) {
      audio.defaultPlaybackRate = rate;
      audio.playbackRate = rate;
    }
    set({ rate });
    syncPositionState();
  },

  close: () => {
    remember();
    set({ queue: [], index: -1, status: 'idle', position: 0, duration: 0 });
    if (audio) {
      audio.pause();
      audio.removeAttribute('src');
      audio.load();
    }
    showMetadata(null);
  },

  retry: () => {
    const s = get();
    if (s.index >= 0) load(s.index);
  },
}));
