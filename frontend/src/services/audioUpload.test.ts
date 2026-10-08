import { describe, it, expect, vi } from 'vitest';
import { probeDuration, putWithProgress } from './audioUpload';

class FakeXhr {
  static last: FakeXhr;
  status = 0;
  headers: Record<string, string> = {};
  method = '';
  url = '';
  body: unknown;
  upload: {
    onprogress: ((e: { lengthComputable: boolean; loaded: number; total: number }) => void) | null;
  } = {
    onprogress: null,
  };
  onload: (() => void) | null = null;
  onerror: (() => void) | null = null;
  constructor() {
    FakeXhr.last = this;
  }
  open(method: string, url: string) {
    this.method = method;
    this.url = url;
  }
  setRequestHeader(k: string, v: string) {
    this.headers[k] = v;
  }
  send(body: unknown) {
    this.body = body;
  }
}

const make = () => new FakeXhr() as unknown as XMLHttpRequest;

describe('putWithProgress', () => {
  it('PUT с подписанным Content-Type, прогресс, успех на 2xx', async () => {
    const file = new Blob(['abc']);
    const progress = vi.fn();
    const done = putWithProgress('https://s3/x?sig', file, 'audio/mpeg', progress, make);
    const x = FakeXhr.last;
    expect([x.method, x.url, x.headers['Content-Type'], x.body]).toEqual([
      'PUT',
      'https://s3/x?sig',
      'audio/mpeg',
      file,
    ]);
    x.upload.onprogress?.({ lengthComputable: true, loaded: 1, total: 3 });
    x.status = 200;
    x.onload?.();
    await done;
    expect(progress).toHaveBeenCalledWith(1, 3);
  });

  it('не 2xx и обрыв — отказ', async () => {
    const bad = putWithProgress('u', new Blob(['a']), 'audio/mpeg', vi.fn(), make);
    FakeXhr.last.status = 403;
    FakeXhr.last.onload?.();
    await expect(bad).rejects.toThrow('403');
    const cut = putWithProgress('u', new Blob(['a']), 'audio/mpeg', vi.fn(), make);
    FakeXhr.last.onerror?.();
    await expect(cut).rejects.toThrow('оборвалась');
  });
});

describe('probeDuration', () => {
  function fakeAudio(duration: number, fail = false) {
    const el = {
      duration,
      preload: '',
      src: '',
      onloadedmetadata: null as (() => void) | null,
      onerror: null as (() => void) | null,
      removeAttribute: vi.fn(),
    };
    queueMicrotask(() => (fail ? el.onerror?.() : el.onloadedmetadata?.()));
    return el as unknown as HTMLAudioElement;
  }

  it('секунды из метаданных', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:x');
    URL.revokeObjectURL = vi.fn();
    await expect(probeDuration(new Blob(['a']), () => fakeAudio(62.5))).resolves.toBe(62.5);
    expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:x');
  });

  // Review Focus 2: длительность не определилась — отказ, а не 0 мс на сервер.
  it('NaN, Infinity и ошибка разбора — отказ', async () => {
    URL.createObjectURL = vi.fn(() => 'blob:x');
    URL.revokeObjectURL = vi.fn();
    await expect(probeDuration(new Blob(['a']), () => fakeAudio(NaN))).rejects.toThrow();
    await expect(probeDuration(new Blob(['a']), () => fakeAudio(Infinity))).rejects.toThrow();
    await expect(probeDuration(new Blob(['a']), () => fakeAudio(5, true))).rejects.toThrow();
  });

  // Тикет 07, п. 1: iOS Safari без жеста не грузит media и молчит — ни
  // loadedmetadata, ни error. Без потолка пачка висела бы с запертым полем.
  it('браузер молчит — отказ по таймауту, blob-URL освобождён', async () => {
    vi.useFakeTimers();
    try {
      URL.createObjectURL = vi.fn(() => 'blob:x');
      URL.revokeObjectURL = vi.fn();
      const silent = {
        duration: NaN,
        preload: '',
        src: '',
        onloadedmetadata: null,
        onerror: null,
        removeAttribute: vi.fn(),
      } as unknown as HTMLAudioElement;
      const p = probeDuration(new Blob(['a']), () => silent, 1000);
      const settled = vi.fn();
      p.catch(settled);
      await vi.advanceTimersByTimeAsync(999);
      expect(settled).not.toHaveBeenCalled();
      await vi.advanceTimersByTimeAsync(1);
      await expect(p).rejects.toThrow(
        'браузер не открыл файл за отведённое время — прикрепите его отдельно',
      );
      expect(URL.revokeObjectURL).toHaveBeenCalledWith('blob:x');
    } finally {
      vi.useRealTimers();
    }
  });
});
