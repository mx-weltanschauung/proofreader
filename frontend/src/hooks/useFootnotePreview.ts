import { useEffect } from 'react';
import { openNoteSheet } from './noteSheet';
import {
  applyPlacement,
  closeActivePopover,
  getActivePopover,
  openPopover,
} from './popoverPlacement';
import './useFootnotePreview.css';

// Leaving the marker does not close the preview at once: the reader needs a
// moment to move the pointer into the popover and scroll a long note.
const CLOSE_DELAY_MS = 180;

let closeTimer: number | undefined;

function cancelClose(): void {
  if (closeTimer !== undefined) {
    window.clearTimeout(closeTimer);
    closeTimer = undefined;
  }
}

function scheduleClose(pop: HTMLElement): void {
  cancelClose();
  closeTimer = window.setTimeout(() => {
    closeTimer = undefined;
    // Something else may have taken the single popover slot meanwhile.
    if (getActivePopover() === pop) closeActivePopover();
  }, CLOSE_DELAY_MS);
}

/** The note body as the reader should see it: without the back-link marker. */
function noteBodyHTML(target: HTMLElement): string {
  const clone = target.cloneNode(true) as HTMLElement;
  clone.querySelectorAll('a.fn-back, a.footnote-return').forEach((el) => el.remove());
  return clone.innerHTML;
}

/** The note the marker points at, or null when the body is not on the page. */
function noteTarget(a: HTMLAnchorElement): HTMLElement | null {
  const href = a.getAttribute('href') ?? '';
  if (!href.startsWith('#fn:')) return null;
  return document.getElementById(href.slice(1));
}

// A star footnote has no number to name it by; a numbered endnote does.
function isSubscript(a: HTMLAnchorElement): boolean {
  return a.closest('sup.footnote-ref')?.classList.contains('footnote-ref--subscript') === true;
}

// useFootnotePreview shows the note without leaving the line: a hover popover
// under the mouse, a bottom sheet under a finger. Either way the body is read
// straight from the aggregated <li id="fn:…"> already in the DOM.
export function useFootnotePreview(
  containerRef: React.RefObject<HTMLElement>,
  deps: unknown[],
): void {
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    const anchors = Array.from(container.querySelectorAll<HTMLAnchorElement>('sup.footnote-ref a'));
    if (anchors.length === 0) return;

    const bound: Array<[HTMLAnchorElement, string, EventListener]> = [];
    const bind = (a: HTMLAnchorElement, type: string, handler: EventListener) => {
      a.addEventListener(type, handler);
      bound.push([a, type, handler]);
    };

    for (const a of anchors) {
      // What last touched this marker. A finger fires emulated mouse events
      // right after its tap, so without this the hover panel would flash open
      // under the sheet the tap is about to raise.
      let lastPointerKind = '';
      const onPointerDown = (e: Event) => {
        lastPointerKind = (e as PointerEvent).pointerType;
      };

      // The popover this marker opened, if any. Without it onLeave would
      // schedule a close for whatever occupies the shared slot - including a
      // click-opened cross-reference this marker never touched.
      let opened: HTMLElement | null = null;
      const onEnter = () => {
        if (lastPointerKind === 'touch' || lastPointerKind === 'pen') return;
        const target = noteTarget(a);
        if (!target) return;

        cancelClose();
        const pop = document.createElement('div');
        pop.className = 'popover-panel footnote-preview-popover';
        pop.innerHTML = noteBodyHTML(target);

        // Hovering the popover itself keeps it alive, so a long note can be
        // scrolled instead of vanishing the moment the pointer moves.
        pop.addEventListener('mouseenter', cancelClose);
        pop.addEventListener('mouseleave', () => scheduleClose(pop));

        opened = pop;
        openPopover(pop, a);
        applyPlacement(pop, a);
      };
      const onLeave = () => {
        if (opened && getActivePopover() === opened) scheduleClose(opened);
      };

      const onClick = (e: Event) => {
        const touch = lastPointerKind === 'touch' || lastPointerKind === 'pen';
        // Every tap answers the next click afresh: a keyboard Enter fires no
        // pointerdown at all and must not inherit the last finger's verdict.
        lastPointerKind = '';
        if (!touch) return;
        const target = noteTarget(a);
        if (!target) return;

        // Two things to stop. The browser would otherwise follow #fn:N and
        // throw the reader to the foot of the page - the very jump the sheet
        // replaces. And useNoteXrefs listens for clicks on the document to
        // close whatever popover is open: this click lands outside the sheet
        // it is about to raise, so it would shut it in the same breath.
        e.preventDefault();
        e.stopPropagation();

        cancelClose();
        const subscript = isSubscript(a);
        const title = subscript ? 'Сноска' : `Примечание ${a.textContent ?? ''}`.trim();
        const sheet = openNoteSheet(a, title);
        sheet.body.innerHTML = noteBodyHTML(target);
        sheet.setGoto(subscript ? '↓ К сноске внизу' : '↓ К примечанию внизу', () => {
          target.scrollIntoView({ block: 'start' });
        });
      };

      bind(a, 'pointerdown', onPointerDown);
      bind(a, 'mouseenter', onEnter);
      bind(a, 'mouseleave', onLeave);
      bind(a, 'click', onClick);
    }

    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || !getActivePopover()) return;
      // Escape достался подсказке. Экран чтения выходит по тому же Escape
      // оконным обработчиком, а window — последняя остановка всплытия: без
      // остановки одно нажатие закрыло бы и подсказку, и экран. В потоке
      // цена такой ошибки — весь накопленный текст. useNoteXrefs гасит
      // всплытие ровно так же.
      e.stopPropagation();
      cancelClose();
      closeActivePopover();
    };
    document.addEventListener('keydown', onKey);

    return () => {
      bound.forEach(([a, type, handler]) => a.removeEventListener(type, handler));
      document.removeEventListener('keydown', onKey);
      cancelClose();
      closeActivePopover();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
