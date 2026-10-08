import { describe, it, expect, afterEach } from 'vitest';
import { renderHook } from '@testing-library/react';
import {
  readSiteHeaderHeightPx,
  useSiteHeaderHeight,
  writeSiteHeaderHeight,
} from './useSiteHeaderHeight';

const PROPERTY = '--site-header-height';

// jsdom reports every layout box as zero, so the height has to be planted.
function withHeight(px: number): HTMLElement {
  const el = document.createElement('header');
  Object.defineProperty(el, 'offsetHeight', { value: px, configurable: true });
  document.body.appendChild(el);
  return el;
}

describe('site header height', () => {
  afterEach(() => {
    document.documentElement.style.removeProperty(PROPERTY);
    document.body.innerHTML = '';
  });

  it('writes the measured height as a whole-pixel custom property', () => {
    writeSiteHeaderHeight(63.4);
    expect(document.documentElement.style.getPropertyValue(PROPERTY)).toBe('63px');
  });

  it('reads the property back as a number', () => {
    writeSiteHeaderHeight(48);
    expect(readSiteHeaderHeightPx()).toBe(48);
  });

  it('reads 0 when the property is unset', () => {
    expect(readSiteHeaderHeightPx()).toBe(0);
  });

  it('measures the element as soon as the hook mounts', () => {
    const el = withHeight(72);
    renderHook(() => useSiteHeaderHeight({ current: el }));
    expect(readSiteHeaderHeightPx()).toBe(72);
  });

  it('drops the property on unmount so the CSS fallback applies', () => {
    const el = withHeight(72);
    const { unmount } = renderHook(() => useSiteHeaderHeight({ current: el }));
    unmount();
    expect(document.documentElement.style.getPropertyValue(PROPERTY)).toBe('');
  });
});
