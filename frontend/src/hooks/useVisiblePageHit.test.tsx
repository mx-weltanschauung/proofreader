import { afterEach, expect, it, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useVisiblePageHit } from './useVisiblePageHit';
import { sendVisiblePage } from '../services/hit';

vi.mock('../services/hit', () => ({ sendVisiblePage: vi.fn() }));

afterEach(() => {
  vi.useRealTimers();
  vi.restoreAllMocks();
  vi.mocked(sendVisiblePage).mockClear();
});

it('отправляет полосу один раз за день и повторяет в новые московские сутки', () => {
  vi.useFakeTimers();
  vi.setSystemTime(new Date('2026-09-29T12:00:00Z'));
  const { rerender } = renderHook(({ page, ready }) => useVisiblePageHit(849, page, ready), {
    initialProps: { page: 1, ready: true },
  });
  expect(sendVisiblePage).toHaveBeenCalledTimes(1);
  rerender({ page: 1, ready: true });
  expect(sendVisiblePage).toHaveBeenCalledTimes(1);
  rerender({ page: 2, ready: true });
  expect(sendVisiblePage).toHaveBeenCalledTimes(2);
  vi.setSystemTime(new Date('2026-09-29T21:01:00Z'));
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'));
  });
  expect(sendVisiblePage).toHaveBeenCalledTimes(3);
});

it('ждёт видимой вкладки и готового текста', () => {
  const hidden = vi.spyOn(document, 'hidden', 'get').mockReturnValue(true);
  const { rerender } = renderHook(({ page, ready }) => useVisiblePageHit(850, page, ready), {
    initialProps: { page: 1, ready: false },
  });
  rerender({ page: 2, ready: true });
  expect(sendVisiblePage).not.toHaveBeenCalled();
  hidden.mockReturnValue(false);
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'));
  });
  expect(sendVisiblePage).toHaveBeenCalledWith(850, 2);
  rerender({ page: 3, ready: false });
  act(() => {
    document.dispatchEvent(new Event('visibilitychange'));
  });
  expect(sendVisiblePage).toHaveBeenCalledTimes(1);
});
