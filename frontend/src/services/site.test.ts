import { afterEach, describe, expect, it, vi } from 'vitest';
import { renderHook, waitFor } from '@testing-library/react';
import { DEFAULT_SITE, resetSiteForTests, useLegal, useSite } from './site';

afterEach(() => {
  vi.restoreAllMocks();
  resetSiteForTests();
});

describe('сведения об экземпляре', () => {
  it('приходят из /api/site', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response(
        JSON.stringify({
          site_name: 'Тестовая',
          site_description: 'о собрании',
          support_url: '',
          channel_url: 'https://example.org/news',
          age_rating: '',
        }),
        { status: 200, headers: { 'content-type': 'application/json' } },
      ),
    );
    const { result } = renderHook(() => useSite());
    await waitFor(() => expect(result.current.siteName).toBe('Тестовая'));
    expect(result.current.channelUrl).toBe('https://example.org/news');
  });

  it('держат умолчание, если /api/site упал', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockRejectedValue(new Error('сеть'));
    const { result } = renderHook(() => useSite());
    await waitFor(() => expect(fetchSpy).toHaveBeenCalled());
    expect(result.current).toEqual(DEFAULT_SITE);
    expect(result.current.siteName).toBe('Читальня');
  });

  it('спрашиваются один раз на сессию', async () => {
    const fetchSpy = vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('{}'));
    renderHook(() => useSite());
    renderHook(() => useSite());
    await waitFor(() => expect(fetchSpy).toHaveBeenCalledTimes(1));
  });
});

describe('страница правовой информации', () => {
  it('файл экземпляра с отметкой nginx — показывается', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('<h2 id="age">Возраст</h2>', {
        status: 200,
        headers: { 'content-type': 'text/html', 'x-instance-file': '1' },
      }),
    );
    const { result } = renderHook(() => useLegal());
    await waitFor(() => expect(result.current.status).toBe('ready'));
    expect(result.current.html).toContain('id="age"');
  });

  it('оболочка SPA с кодом 200 считается отсутствием файла', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(
      new Response('<!doctype html><div id="root"></div>', {
        status: 200,
        headers: { 'content-type': 'text/html' },
      }),
    );
    const { result } = renderHook(() => useLegal());
    await waitFor(() => expect(result.current.status).toBe('missing'));
  });

  it('404 — файла нет', async () => {
    vi.spyOn(globalThis, 'fetch').mockResolvedValue(new Response('', { status: 404 }));
    const { result } = renderHook(() => useLegal());
    await waitFor(() => expect(result.current.status).toBe('missing'));
  });
});
