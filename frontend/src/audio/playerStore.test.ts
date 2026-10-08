import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { FakeAudio, asAudio } from '../test/fakeAudio';
import { savePosition, savedPosition } from './positions';
import { PREV_RESTART_S, setAudioElement, usePlayer } from './playerStore';
import type { QueueItem } from './queue';

function item(n: number, durationMs = 100_000): QueueItem {
  return {
    key: `track:${n}`,
    url: `/api/audio/${n}.opus`,
    downloadUrl: `/api/audio/${n}.opus?download=1`,
    title: `Часть ${n}`,
    subtitle: 'Капитал, т. 1 · синтез',
    href: '/works/4/chapters/1',
    durationMs,
  };
}

const Q = [item(1), item(2), item(3)];
let a: FakeAudio;
const st = () => usePlayer.getState();

/** Метаданные пришли, файл заиграл. */
function started(duration = 100): void {
  a.duration = duration;
  a.fire('loadedmetadata');
  a.fire('playing');
}

beforeEach(() => {
  localStorage.clear();
  a = new FakeAudio();
  setAudioElement(asAudio(a));
  usePlayer.setState({ queue: [], index: -1, status: 'idle', position: 0, duration: 0, rate: 1 });
});
afterEach(() => vi.restoreAllMocks());

describe('очередь', () => {
  it('playQueue ставит очередь и играет нажатую дорожку', () => {
    st().playQueue(Q, 1);
    expect(st().index).toBe(1);
    expect(st().status).toBe('loading');
    expect(st().duration).toBe(100);
    expect(a.src).toBe('/api/audio/2.opus');
    expect(a.play).toHaveBeenCalledTimes(1);
    started();
    expect(st().status).toBe('playing');
  });

  it('индекс вне очереди — ничего не происходит', () => {
    st().playQueue(Q, 5);
    expect(st().index).toBe(-1);
    expect(a.play).not.toHaveBeenCalled();
  });

  it('дорожка доиграла — следующая сама; последняя — ended, полоса остаётся', () => {
    st().playQueue(Q, 1);
    started();
    a.fire('ended');
    expect(st().index).toBe(2);
    expect(a.src).toBe('/api/audio/3.opus');
    started(80);
    a.fire('ended');
    expect(st().status).toBe('ended');
    expect(st().queue).toHaveLength(3);
    expect(st().position).toBe(80);
  });

  it('▶ после конца очереди играет последнюю сначала', () => {
    st().playQueue(Q, 2);
    started();
    a.currentTime = 99;
    a.fire('ended');
    st().toggle();
    expect(st().status).toBe('loading');
    expect(st().position).toBe(0);
    expect(a.play).toHaveBeenCalledTimes(2);
  });

  it('⏭ и ⏮: ⏮ дальше трёх секунд — в начало, ближе — предыдущая', () => {
    st().playQueue(Q, 0);
    started();
    st().next();
    expect(st().index).toBe(1);
    started();
    a.currentTime = PREV_RESTART_S + 1;
    a.fire('timeupdate');
    st().prev();
    expect(st().index).toBe(1);
    expect(a.currentTime).toBe(0);
    st().prev();
    expect(st().index).toBe(0);
  });

  it('⏭ на последней и ⏮ на первой не выходят за очередь', () => {
    st().playQueue(Q, 2);
    st().next();
    expect(st().index).toBe(2);
    st().playQueue(Q, 0);
    started();
    st().prev();
    expect(st().index).toBe(0);
    expect(a.currentTime).toBe(0);
  });

  it('другая очередь заменяет прежнюю целиком', () => {
    st().playQueue(Q, 2);
    const other = [item(10), item(11)];
    st().playQueue(other, 0);
    expect(st().queue).toBe(other);
    expect(a.src).toBe('/api/audio/10.opus');
  });

  it('✕ останавливает, очищает очередь и снимает src', () => {
    st().playQueue(Q, 0);
    started();
    st().close();
    expect(st().queue).toEqual([]);
    expect(st().index).toBe(-1);
    expect(st().status).toBe('idle');
    expect(a.pause).toHaveBeenCalled();
    expect(a.src).toBe('');
    expect(a.load).toHaveBeenCalled();
  });
});

describe('игра и пауза', () => {
  it('toggle: пауза и снова игра', () => {
    st().playQueue(Q, 0);
    started();
    st().toggle();
    expect(st().status).toBe('paused');
    expect(a.pause).toHaveBeenCalled();
    st().toggle();
    expect(a.play).toHaveBeenCalledTimes(2);
    a.fire('playing');
    expect(st().status).toBe('playing');
  });

  it('пауза системой (вынули наушники) — paused', () => {
    st().playQueue(Q, 0);
    started();
    a.fire('pause');
    expect(st().status).toBe('paused');
  });

  // Рецензия ветки: звонок или вынутые наушники во время загрузки — система
  // ставит паузу, а полоса крутилась бы вечно над остановленным звуком.
  it('пауза системой во время загрузки — paused, а не вечная крутилка', () => {
    st().playQueue(Q, 0);
    expect(st().status).toBe('loading');
    a.fire('pause');
    expect(st().status).toBe('paused');
  });

  it('перемотка ставит currentTime и позицию', () => {
    st().playQueue(Q, 0);
    started();
    st().seek(42);
    expect(a.currentTime).toBe(42);
    expect(st().position).toBe(42);
  });

  // Двойной клик по разным строкам: первая загрузка прерывается второй.
  it('play() прерван новой загрузкой (AbortError) — не сбой', async () => {
    a.play.mockReturnValueOnce(Promise.reject(new DOMException('прервано', 'AbortError')));
    st().playQueue(Q, 0);
    st().playQueue(Q, 1);
    await Promise.resolve();
    await Promise.resolve();
    expect(st().status).toBe('loading');
    expect(st().index).toBe(1);
  });

  // iOS не даёт играть без жеста: автопереход в фоне отклонён.
  it('play() запрещён (NotAllowedError) — пауза, а не вечная загрузка', async () => {
    a.play.mockReturnValueOnce(Promise.reject(new DOMException('нельзя', 'NotAllowedError')));
    st().playQueue(Q, 0);
    await Promise.resolve();
    await Promise.resolve();
    expect(st().status).toBe('paused');
  });

  it('ошибка файла — error; retry грузит заново', () => {
    st().playQueue(Q, 0);
    a.fire('error');
    expect(st().status).toBe('error');
    st().retry();
    expect(st().status).toBe('loading');
    expect(a.play).toHaveBeenCalledTimes(2);
  });

  // Снятый src в браузере даёт событие error — полоса не должна ожить.
  it('error после ✕ молчит', () => {
    st().playQueue(Q, 0);
    st().close();
    a.fire('error');
    expect(st().status).toBe('idle');
  });
});

describe('скорость', () => {
  it('setRate ставит обе скорости элементу и запоминается', () => {
    st().playQueue(Q, 0);
    st().setRate(1.5);
    expect(a.playbackRate).toBe(1.5);
    expect(a.defaultPlaybackRate).toBe(1.5);
    expect(localStorage.getItem('audio-rate')).toBe('1.5');
  });

  // Загрузка нового src сбрасывает playbackRate на defaultPlaybackRate.
  it('новая дорожка получает текущую скорость', () => {
    usePlayer.setState({ rate: 1.75 });
    st().playQueue(Q, 0);
    expect(a.playbackRate).toBe(1.75);
    expect(a.defaultPlaybackRate).toBe(1.75);
  });
});

describe('память места', () => {
  it('недослушанная дорожка начинается с запомненного места после метаданных', () => {
    savePosition('track:2', 291, 760);
    st().playQueue(Q, 1);
    expect(st().position).toBe(291);
    expect(a.currentTime).toBe(0);
    a.duration = 760;
    a.fire('loadedmetadata');
    expect(a.currentTime).toBe(291);
  });

  it('во время игры место пишется раз в 5 с звука', () => {
    st().playQueue(Q, 0);
    started();
    a.currentTime = 4;
    a.fire('timeupdate');
    expect(savedPosition('track:1')).toBeNull();
    a.currentTime = 5.2;
    a.fire('timeupdate');
    expect(savedPosition('track:1')).toBe(5.2);
  });

  it('пауза, смена дорожки и уход со страницы записывают место', () => {
    st().playQueue(Q, 0);
    started();
    a.currentTime = 12;
    st().toggle();
    expect(savedPosition('track:1')).toBe(12);
    st().toggle();
    a.fire('playing');
    a.currentTime = 30;
    st().next();
    expect(savedPosition('track:1')).toBe(30);
    started();
    a.currentTime = 44;
    window.dispatchEvent(new Event('pagehide'));
    expect(savedPosition('track:2')).toBe(44);
  });

  // Пауза до метаданных: currentTime ещё 0 — вчерашнее место не трогаем.
  it('пауза во время загрузки не стирает запомненное', () => {
    savePosition('track:1', 291, 760);
    st().playQueue(Q, 0);
    st().toggle();
    expect(savedPosition('track:1')).toBe(291);
  });

  // Нажал ▶ у недослушанной и тут же другую — метаданные первой не пришли.
  it('переключение до загрузки не стирает место прежней дорожки', () => {
    savePosition('track:1', 291, 760);
    st().playQueue(Q, 0);
    st().playQueue(Q, 1);
    expect(savedPosition('track:1')).toBe(291);
  });

  it('дослушанная до конца дорожка забывается', () => {
    savePosition('track:1', 50, 100);
    st().playQueue(Q, 0);
    started();
    a.fire('ended');
    expect(savedPosition('track:1')).toBeNull();
  });
});

describe('экран блокировки', () => {
  it('метаданные дорожки и обработчики кнопок', () => {
    const handlers = new Map<string, MediaSessionActionHandler | null>();
    const session = {
      metadata: null as unknown,
      setActionHandler: vi.fn((name: string, h: MediaSessionActionHandler | null) => {
        handlers.set(name, h);
      }),
      setPositionState: vi.fn(),
    };
    Object.defineProperty(navigator, 'mediaSession', { value: session, configurable: true });
    vi.stubGlobal(
      'MediaMetadata',
      class {
        constructor(init: object) {
          Object.assign(this, init);
        }
      },
    );
    try {
      setAudioElement(asAudio(a));
      st().playQueue(Q, 0);
      expect(session.metadata).toMatchObject({
        title: 'Часть 1',
        artist: 'Капитал, т. 1 · синтез',
      });
      started();
      expect(session.setPositionState).toHaveBeenCalledWith({
        duration: 100,
        playbackRate: 1,
        position: 0,
      });
      handlers.get('nexttrack')?.({ action: 'nexttrack' });
      expect(st().index).toBe(1);
      started();
      handlers.get('seekto')?.({ action: 'seekto', seekTime: 30 });
      expect(a.currentTime).toBe(30);
      handlers.get('seekbackward')?.({ action: 'seekbackward' });
      expect(a.currentTime).toBe(15);
      handlers.get('pause')?.({ action: 'pause' });
      expect(st().status).toBe('paused');
      // «Играть» с экрана блокировки во время загрузки не должно ставить паузу.
      st().next();
      expect(st().status).toBe('loading');
      handlers.get('play')?.({ action: 'play' });
      expect(st().status).toBe('loading');
      st().close();
      expect(session.metadata).toBeNull();
    } finally {
      Reflect.deleteProperty(navigator, 'mediaSession');
      vi.unstubAllGlobals();
    }
  });
});
