import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import {
  computePlacement,
  applyPlacement,
  closeActivePopover,
  getActivePopover,
  openPopover,
} from './popoverPlacement';

// A roomy desktop viewport unless a case needs otherwise.
const VIEWPORT = { width: 1000, height: 800 };

describe('computePlacement', () => {
  it('sits below the anchor when the note fits there', () => {
    const p = computePlacement(
      { top: 100, bottom: 120, left: 200 },
      { width: 400, height: 200 },
      VIEWPORT,
    );
    expect(p.placement).toBe('below');
    expect(p.top).toBe(124);
    expect(p.left).toBe(200);
    expect(p.maxHeight).toBe(200);
  });

  it('flips above the anchor when there is more room up there', () => {
    const p = computePlacement(
      { top: 600, bottom: 620, left: 100 },
      { width: 400, height: 300 },
      VIEWPORT,
    );
    expect(p.placement).toBe('above');
    expect(p.maxHeight).toBe(300);
    expect(p.top).toBe(296); // 600 - 4 gap - 300 height
  });

  it('keeps the roomier side and trims the height when neither side fits', () => {
    const p = computePlacement(
      { top: 380, bottom: 400, left: 100 },
      { width: 400, height: 500 },
      VIEWPORT,
    );
    expect(p.placement).toBe('below'); // 388px below vs 368px above
    expect(p.maxHeight).toBe(388);
    expect(p.top).toBe(404);
  });

  it('never grows past half the viewport height', () => {
    const p = computePlacement(
      { top: 50, bottom: 70, left: 100 },
      { width: 400, height: 2000 },
      VIEWPORT,
    );
    expect(p.maxHeight).toBe(400);
  });

  it('pulls back from the right edge', () => {
    const p = computePlacement(
      { top: 100, bottom: 120, left: 900 },
      { width: 400, height: 100 },
      VIEWPORT,
    );
    expect(p.left).toBe(592); // 1000 - 400 - 8
  });

  it('never goes past the left edge, even when wider than the viewport', () => {
    const p = computePlacement(
      { top: 100, bottom: 120, left: 10 },
      { width: 600, height: 100 },
      { width: 500, height: 800 },
    );
    expect(p.left).toBe(8);
  });

  it('never returns a negative height in a window with no room at all', () => {
    const p = computePlacement(
      { top: 5, bottom: 95, left: 100 },
      { width: 400, height: 200 },
      { width: 1000, height: 100 },
    );
    expect(p.maxHeight).toBe(0);
  });
});

describe('applyPlacement', () => {
  it('clears a stale inline left/top before measuring the width', () => {
    const anchor = document.createElement('div');
    anchor.getBoundingClientRect = () =>
      ({
        top: 100,
        bottom: 120,
        left: 200,
        right: 200,
        width: 0,
        height: 20,
        x: 200,
        y: 100,
        toJSON() {},
      }) as DOMRect;

    const pop = document.createElement('div');
    document.body.appendChild(pop);

    // Simulate a previous applyPlacement call that left the popover shifted
    // far to the right and clamped in height.
    pop.style.left = '700px';
    pop.style.top = '50px';
    pop.style.maxHeight = '999px';

    let leftAtMeasurement: string | null = null;
    Object.defineProperty(pop, 'offsetWidth', {
      configurable: true,
      get() {
        leftAtMeasurement = pop.style.left;
        return 0;
      },
    });

    applyPlacement(pop, anchor);

    // The stale left must be gone by the time width is read...
    expect(leftAtMeasurement).toBe('');
    // ...and the final inline styles reflect this call's own computation,
    // not the values left behind by the previous one.
    expect(pop.style.left).not.toBe('700px');
    expect(pop.style.top).not.toBe('50px');
    expect(pop.style.maxHeight).not.toBe('999px');
  });
});

describe('popover registry', () => {
  beforeEach(() => {
    closeActivePopover();
    document.body.innerHTML = '';
  });

  it('mounts the popover and marks it active', () => {
    const pop = document.createElement('div');
    openPopover(pop, document.createElement('a'));
    expect(pop.isConnected).toBe(true);
    expect(getActivePopover()).toBe(pop);
  });

  it('drops the previous popover when a second one opens', () => {
    const first = document.createElement('div');
    const second = document.createElement('div');
    openPopover(first, document.createElement('a'));
    openPopover(second, document.createElement('a'));
    expect(first.isConnected).toBe(false);
    expect(getActivePopover()).toBe(second);
  });

  it('detaches and forgets the popover on close', () => {
    const pop = document.createElement('div');
    openPopover(pop, document.createElement('a'));
    closeActivePopover();
    expect(pop.isConnected).toBe(false);
    expect(getActivePopover()).toBeNull();
  });
});

describe('popover resize', () => {
  beforeEach(() => {
    vi.useFakeTimers();
    closeActivePopover();
    document.body.innerHTML = '';
  });

  afterEach(() => {
    closeActivePopover();
    vi.useRealTimers();
    document.body.innerHTML = '';
  });

  it('re-places the open popover when the window resizes', () => {
    const pop = document.createElement('div');
    const anchor = document.createElement('a');
    document.body.appendChild(anchor);
    openPopover(pop, anchor);

    // Whatever the first placement wrote, a resize must recompute it. jsdom
    // reports every rect as zero, so the recomputed offsets are the constants
    // computePlacement derives from a zero-sized anchor at the origin.
    pop.style.left = '999px';
    pop.style.top = '999px';
    window.dispatchEvent(new Event('resize'));
    vi.advanceTimersByTime(50);

    expect(pop.style.left).toBe('8px');
    expect(pop.style.top).toBe('4px');
  });

  it('leaves a self-placed popover where its own stylesheet puts it', () => {
    const pop = document.createElement('div');
    const anchor = document.createElement('a');
    document.body.appendChild(anchor);
    // The mobile note sheet is pinned to the bottom of the screen by CSS and
    // has no anchor to hang from: re-placing it on resize would tear it off
    // the edge and drop it at the origin.
    openPopover(pop, anchor, { selfPlaced: true });

    window.dispatchEvent(new Event('resize'));
    vi.advanceTimersByTime(50);

    expect(pop.style.left).toBe('');
    expect(pop.style.top).toBe('');
  });

  it('stops listening for resize once the popover closes', () => {
    const pop = document.createElement('div');
    const anchor = document.createElement('a');
    document.body.appendChild(anchor);
    openPopover(pop, anchor);

    const removeSpy = vi.spyOn(window, 'removeEventListener');
    closeActivePopover();

    // The listener has to be handed back, not merely rendered inert: one that
    // is never removed outlives every popover the page will ever open.
    expect(removeSpy).toHaveBeenCalledWith('resize', expect.any(Function));
    removeSpy.mockRestore();
  });

  it('leaves focus alone when the popover never held it', () => {
    const outside = document.createElement('button');
    document.body.appendChild(outside);
    outside.focus();

    const pop = document.createElement('div');
    const anchor = document.createElement('a');
    anchor.href = '#'; // jsdom only focuses an anchor that has one
    document.body.appendChild(anchor);
    openPopover(pop, anchor);
    closeActivePopover();

    expect(document.activeElement).toBe(outside);
  });
});
