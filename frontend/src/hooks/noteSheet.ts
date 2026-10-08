import { closeActivePopover, getActivePopover, openPopover } from './popoverPlacement';
import './noteSheet.css';

export interface NoteSheetHandle {
  /** The whole sheet, scrim included; it holds the single popover slot. */
  root: HTMLElement;
  /** Fill this with the note itself — text now, or markup once it loads. */
  body: HTMLElement;
  /**
   * Puts the jump to the full note under the sheet. An href is followed as an
   * ordinary link (the note lives on another page); a callback runs with the
   * sheet already closed, since scrolling to a note the sheet covers is no
   * help at all.
   */
  setGoto(label: string, action: string | (() => void)): void;
}

/**
 * Opens the bottom sheet a touch reader gets instead of a hover panel: a
 * finger has no hover, and a tiny superscript in mid-paragraph is a poor thing
 * to hang a panel from.
 */
export function openNoteSheet(anchor: Element, title: string): NoteSheetHandle {
  const root = document.createElement('div');
  root.className = 'note-sheet-root';

  const scrim = document.createElement('div');
  scrim.className = 'note-sheet-scrim';
  // Closing the sheet only through its own slot: something else may have taken
  // the slot between the tap and this handler.
  scrim.addEventListener('click', () => {
    if (getActivePopover() === root) closeActivePopover();
  });

  const panel = document.createElement('div');
  panel.className = 'note-sheet';
  panel.setAttribute('role', 'dialog');
  panel.setAttribute('aria-label', title);
  panel.tabIndex = -1;

  const head = document.createElement('div');
  head.className = 'note-sheet-head';
  const titleEl = document.createElement('span');
  titleEl.className = 'note-sheet-title';
  titleEl.textContent = title;
  const close = document.createElement('button');
  close.type = 'button';
  close.className = 'note-sheet-close';
  close.setAttribute('aria-label', 'Закрыть');
  close.textContent = '✕';
  close.addEventListener('click', () => {
    if (getActivePopover() === root) closeActivePopover();
  });
  head.appendChild(titleEl);
  head.appendChild(close);

  const body = document.createElement('div');
  body.className = 'note-sheet-body';

  const foot = document.createElement('div');
  foot.className = 'note-sheet-foot';

  panel.appendChild(head);
  panel.appendChild(body);
  panel.appendChild(foot);
  root.appendChild(scrim);
  root.appendChild(panel);

  openPopover(root, anchor, { selfPlaced: true });
  // A long note scrolls inside the panel, and a scrollable region only answers
  // the arrow keys once it holds focus. Focus also comes back to the marker
  // when the sheet closes, so the reader is not dropped at the top of the page.
  panel.focus();

  const setGoto = (label: string, action: string | (() => void)): void => {
    foot.textContent = '';
    const link = document.createElement('a');
    link.className = 'note-sheet-goto';
    link.textContent = label;
    if (typeof action === 'string') {
      link.href = action;
    } else {
      link.href = '#';
      link.addEventListener('click', (e) => {
        e.preventDefault();
        if (getActivePopover() === root) closeActivePopover();
        action();
      });
    }
    foot.appendChild(link);
  };

  return { root, body, setGoto };
}
