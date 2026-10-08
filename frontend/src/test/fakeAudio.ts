import { vi } from 'vitest';

/** Поддельный <audio> для тестов проигрывателя: jsdom не умеет play(), а тесту
 *  нужно самому решать, когда пришли метаданные, кончилась дорожка или упала
 *  загрузка. События шлёт fire(). */
export class FakeAudio extends EventTarget {
  private source = '';
  /** Как в браузере: новый src сбрасывает позицию и длительность. */
  get src(): string {
    return this.source;
  }
  set src(value: string) {
    this.source = value;
    this.currentTime = 0;
    this.duration = Number.NaN;
  }
  preload = '';
  currentTime = 0;
  duration = Number.NaN;
  playbackRate = 1;
  defaultPlaybackRate = 1;
  paused = true;
  play = vi.fn((): Promise<void> => {
    this.paused = false;
    return Promise.resolve();
  });
  pause = vi.fn((): void => {
    this.paused = true;
  });
  load = vi.fn();
  removeAttribute(name: string): void {
    if (name === 'src') this.source = '';
  }
  fire(type: string): void {
    this.dispatchEvent(new Event(type));
  }
}

export function asAudio(fake: FakeAudio): HTMLAudioElement {
  return fake as unknown as HTMLAudioElement;
}
