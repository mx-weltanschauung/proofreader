import './popoverPlacement.css';

/** Gap between the anchor and the popover, in px. */
export const POPOVER_GAP = 4;
/** Minimum distance from the popover to any viewport edge, in px. */
export const VIEWPORT_MARGIN = 8;

export interface AnchorRect {
  top: number;
  bottom: number;
  left: number;
}

export interface Size {
  width: number;
  height: number;
}

export interface Viewport {
  width: number;
  height: number;
}

export interface Placement {
  /** Viewport coordinates; the caller adds scroll offsets. */
  top: number;
  left: number;
  maxHeight: number;
  placement: 'below' | 'above';
}

/**
 * Places a popover against its anchor without letting it leave the viewport.
 *
 * Everything is plain numbers in viewport coordinates so this can be tested
 * without a browser: jsdom reports every rect as zero.
 */
export function computePlacement(anchor: AnchorRect, content: Size, viewport: Viewport): Placement {
  const spaceBelow = viewport.height - anchor.bottom - POPOVER_GAP - VIEWPORT_MARGIN;
  const spaceAbove = anchor.top - POPOVER_GAP - VIEWPORT_MARGIN;
  // A popover taller than half the screen hides the text it explains.
  const ceiling = viewport.height / 2;

  const fitsBelow = content.height <= Math.min(spaceBelow, ceiling);
  const placement: 'below' | 'above' = fitsBelow || spaceAbove <= spaceBelow ? 'below' : 'above';

  const available = placement === 'below' ? spaceBelow : spaceAbove;
  const maxHeight = Math.max(0, Math.min(content.height, available, ceiling));

  const top =
    placement === 'below' ? anchor.bottom + POPOVER_GAP : anchor.top - POPOVER_GAP - maxHeight;

  // Align with the anchor, then pull back from whichever edge it would cross.
  const rightmost = viewport.width - content.width - VIEWPORT_MARGIN;
  const left = Math.max(VIEWPORT_MARGIN, Math.min(anchor.left, rightmost));

  return { top, left, maxHeight, placement };
}

/**
 * Measures the popover and the anchor, then places the popover in document
 * coordinates. Call again after the content changes: a popover that grew
 * taller may no longer fit where it was put.
 */
export function applyPlacement(pop: HTMLElement, anchor: Element): void {
  // Un-clamping maxHeight below resets scrollTop to 0, so a reader midway
  // through a long note would get thrown back to its first line on every
  // resize. Capture it here and restore it once the new clamp is in place.
  const scrollTop = pop.scrollTop;
  // Measure the natural box, not one clamped/shifted by a previous call: a
  // stale inline `left` would cap the shrink-to-fit width used below.
  pop.style.maxHeight = '';
  pop.style.left = '';
  pop.style.top = '';
  const rect = anchor.getBoundingClientRect();
  const { top, left, maxHeight } = computePlacement(
    { top: rect.top, bottom: rect.bottom, left: rect.left },
    { width: pop.offsetWidth, height: pop.scrollHeight },
    { width: window.innerWidth, height: window.innerHeight },
  );
  pop.style.top = `${window.scrollY + top}px`;
  pop.style.left = `${window.scrollX + left}px`;
  pop.style.maxHeight = `${maxHeight}px`;
  pop.scrollTop = scrollTop;
}

// One popover at a time, shared by every hook that opens one: a hover preview
// and a click-opened cross-reference must never overlap each other.
let active: HTMLElement | null = null;
let activeAnchor: Element | null = null;
let activeSelfPlaced = false;
let resizeFrame = 0;

function onResize(): void {
  cancelAnimationFrame(resizeFrame);
  resizeFrame = requestAnimationFrame(() => {
    if (active && activeAnchor && !activeSelfPlaced) applyPlacement(active, activeAnchor);
  });
}

export function getActivePopover(): HTMLElement | null {
  return active;
}

export interface OpenOptions {
  /**
   * The popover positions itself (the note sheet is pinned to the bottom of
   * the screen by its stylesheet). Such a popover still takes the single slot
   * - so a sheet and an anchored panel can never overlap - but placement,
   * including the one redone on resize, is left to CSS.
   */
  selfPlaced?: boolean;
}

export function openPopover(pop: HTMLElement, anchor: Element, opts: OpenOptions = {}): void {
  closeActivePopover();
  document.body.appendChild(pop);
  active = pop;
  activeAnchor = anchor;
  activeSelfPlaced = opts.selfPlaced === true;
  // A click-opened popover outlives a resize, and its placement is in document
  // coordinates: without this it keeps hanging at the old position, clamped to
  // the old viewport height.
  window.addEventListener('resize', onResize);
}

export function closeActivePopover(): void {
  window.removeEventListener('resize', onResize);
  cancelAnimationFrame(resizeFrame);
  // Whoever closes the popover, focus must not fall to <body>: removing a
  // focused element leaves no trace of where the user was. Only restore when
  // focus was actually inside - a hover preview never holds it.
  const anchor = activeAnchor;
  const hadFocus = active !== null && active.contains(document.activeElement);
  active?.remove();
  active = null;
  activeAnchor = null;
  activeSelfPlaced = false;
  if (hadFocus && anchor instanceof HTMLElement) anchor.focus();
}
