import { afterEach, describe, expect, it, vi } from 'vitest';
import { deviceClass, sendHit } from './hit';

describe('deviceClass', () => {
  it('режет ширину на три корзины по границам 600 и 1024', () => {
    expect(deviceClass(320)).toBe('narrow');
    expect(deviceClass(599)).toBe('narrow');
    expect(deviceClass(600)).toBe('medium');
    expect(deviceClass(1023)).toBe('medium');
    expect(deviceClass(1024)).toBe('wide');
  });
});

describe('sendHit: устройство', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('кладёт класс ширины окна в тело', () => {
    vi.stubGlobal('innerWidth', 375);
    const fetchMock = vi.fn().mockResolvedValue({});
    vi.stubGlobal('fetch', fetchMock);
    sendHit('/works/1', '');
    const init = fetchMock.mock.calls[0][1] as { body: string };
    expect(JSON.parse(init.body)).toEqual({ path: '/works/1', ref: '', device: 'narrow' });
  });

  it('без ширины окна шлёт маячок с пустым классом и не падает', () => {
    vi.stubGlobal('innerWidth', undefined);
    const fetchMock = vi.fn().mockResolvedValue({});
    vi.stubGlobal('fetch', fetchMock);
    expect(() => sendHit('/works/1', '')).not.toThrow();
    const init = fetchMock.mock.calls[0][1] as { body: string };
    expect(JSON.parse(init.body).device).toBe('');
  });
});

describe('sendHit', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    vi.unstubAllGlobals();
  });

  it('не падает и шлёт маячок без токена, если хранилище бросает', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockImplementation(() => {
      throw new Error('SecurityError');
    });
    const fetchMock = vi.fn().mockResolvedValue({});
    vi.stubGlobal('fetch', fetchMock);

    expect(() => sendHit('/works/1', '')).not.toThrow();
    expect(fetchMock).toHaveBeenCalledTimes(1);
    const init = fetchMock.mock.calls[0][1] as { headers: Record<string, string> };
    expect(init.headers.Authorization).toBeUndefined();
  });

  it('кладёт токен в заголовок, когда хранилище отвечает', () => {
    vi.spyOn(Storage.prototype, 'getItem').mockReturnValue('tok');
    const fetchMock = vi.fn().mockResolvedValue({});
    vi.stubGlobal('fetch', fetchMock);
    sendHit('/works/1', '');
    const init = fetchMock.mock.calls[0][1] as { headers: Record<string, string> };
    expect(init.headers.Authorization).toBe('Bearer tok');
  });
});
