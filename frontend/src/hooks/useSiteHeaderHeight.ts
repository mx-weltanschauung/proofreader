import { useEffect } from 'react';

const PROPERTY = '--site-header-height';

/** Publishes the measured header height for the layout to read. */
export function writeSiteHeaderHeight(height: number): void {
  document.documentElement.style.setProperty(PROPERTY, `${Math.round(height)}px`);
}

/**
 * Reads the property back in pixels, inline value first: that is what the
 * measurement writes, and jsdom's getComputedStyle does not resolve custom
 * properties. Returns 0 when it is unset or not a px value — callers use it
 * as an offset, and 0 is the harmless offset.
 */
export function readSiteHeaderHeightPx(): number {
  const inline = document.documentElement.style.getPropertyValue(PROPERTY);
  const raw = inline || getComputedStyle(document.documentElement).getPropertyValue(PROPERTY);
  const px = parseFloat(raw);
  return Number.isFinite(px) ? px : 0;
}

/**
 * Keeps --site-header-height equal to the real height of the sticky site
 * header. The chapter toolbar sticks below it; that offset used to be a
 * hand-tuned constant, which no breakpoint or wrapped header could track.
 */
export function useSiteHeaderHeight(ref: React.RefObject<HTMLElement>): void {
  useEffect(() => {
    const el = ref.current;
    if (!el) return;
    const measure = () => writeSiteHeaderHeight(el.offsetHeight);
    measure();
    const observer = new ResizeObserver(measure);
    observer.observe(el);
    return () => {
      observer.disconnect();
      // Back to the CSS fallback rather than a frozen last measurement.
      document.documentElement.style.removeProperty(PROPERTY);
    };
  }, [ref]);
}
