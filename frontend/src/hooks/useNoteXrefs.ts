import { useEffect } from 'react';
import { getWorkNotesIndex } from '../services/notesIndex';
import type { NoteIndexEntry } from '../types';
import { openNoteSheet } from './noteSheet';
import {
  applyPlacement,
  closeActivePopover,
  getActivePopover,
  openPopover,
} from './popoverPlacement';
import { pagePath } from '../utils/paths';
import './useNoteXrefs.css';

/** Where a loaded note goes — an anchored panel or the bottom sheet. */
interface NoteSink {
  /** A status line: loading, missing, failed. */
  text(message: string): void;
  /** The note itself. */
  note(entry: NoteIndexEntry): void;
}

/**
 * Loads one note into whatever is showing it, giving up quietly if the reader
 * has closed or replaced it meanwhile.
 */
async function fillNote(
  container: HTMLElement,
  workId: number,
  note: string,
  sink: NoteSink,
): Promise<void> {
  try {
    const index = await getWorkNotesIndex(workId);
    if (getActivePopover() !== container) return; // closed/replaced while loading
    const entry = index.get(parseInt(note, 10));
    if (!entry) sink.text(`Примечание ${note} не найдено`);
    else sink.note(entry);
  } catch {
    if (getActivePopover() === container) sink.text('Не удалось загрузить примечание');
  }
}

export function useNoteXrefs(
  containerRef: React.RefObject<HTMLElement>,
  workId: number | undefined,
  workSlug: string | undefined,
  deps: unknown[],
): void {
  useEffect(() => {
    const container = containerRef.current;
    if (!container || workId == null) return;
    const anchors = Array.from(container.querySelectorAll<HTMLAnchorElement>('a.note-xref'));
    if (anchors.length === 0) return;

    const onDocClick = (e: MouseEvent) => {
      const pop = getActivePopover();
      if (pop && !pop.contains(e.target as Node)) {
        closeActivePopover();
      }
    };
    const onKey = (e: KeyboardEvent) => {
      if (e.key !== 'Escape' || !getActivePopover()) return;
      // This Escape belongs to the popover. Reading mode also exits on Escape
      // from a window-level handler, and window is the last stop in the bubble
      // path - without this the one keystroke would close both.
      e.stopPropagation();
      closeActivePopover();
    };

    const bound: Array<[HTMLAnchorElement, string, EventListener]> = [];
    const bind = (a: HTMLAnchorElement, type: string, handler: EventListener) => {
      a.addEventListener(type, handler);
      bound.push([a, type, handler]);
    };

    for (const a of anchors) {
      // What last touched this link. A panel hung off a word in mid-paragraph
      // covers the very line the reader is on; a finger gets the sheet instead.
      let lastPointerKind = '';
      const onPointerDown = (e: Event) => {
        lastPointerKind = (e as PointerEvent).pointerType;
      };

      const openSheet = (note: string) => {
        const sheet = openNoteSheet(a, `Примечание ${note}`);
        sheet.root.dataset.note = note;
        sheet.body.textContent = 'Загрузка…';
        return fillNote(sheet.root, workId, note, {
          text: (message) => {
            sheet.body.textContent = message;
          },
          note: (entry) => {
            sheet.body.innerHTML = entry.body_html;
            sheet.setGoto(
              `→ стр. ${entry.target_page}`,
              pagePath({ id: workId, slug: workSlug }, entry.target_page),
            );
          },
        });
      };

      const openPanel = (note: string) => {
        const pop = document.createElement('div');
        pop.className = 'popover-panel note-xref-popover';
        pop.dataset.note = note;
        pop.tabIndex = -1;
        pop.setAttribute('role', 'dialog');
        pop.setAttribute('aria-label', `Примечание ${note}`);
        pop.setAttribute('aria-live', 'polite');
        pop.textContent = 'Загрузка…';
        openPopover(pop, a);
        applyPlacement(pop, a);
        // The panel scrolls when the note is long, and a scrollable region is
        // only reachable with the arrow keys once it holds focus.
        pop.focus();

        return fillNote(pop, workId, note, {
          text: (message) => {
            pop.textContent = message;
            applyPlacement(pop, a);
          },
          note: (entry) => {
            pop.innerHTML =
              `<div class="note-xref-body">${entry.body_html}</div>` +
              `<a class="note-xref-goto" href="${pagePath({ id: workId, slug: workSlug }, entry.target_page)}">` +
              `→ стр. ${entry.target_page}</a>`;
            applyPlacement(pop, a);
          },
        });
      };

      const onClick = async (e: MouseEvent) => {
        e.preventDefault();
        e.stopPropagation();
        const touch = lastPointerKind === 'touch' || lastPointerKind === 'pen';
        // Every tap answers the next click afresh: a keyboard Enter fires no
        // pointerdown at all and must not inherit the last finger's verdict.
        lastPointerKind = '';

        const note = a.dataset.note ?? '';
        const wasSame = getActivePopover()?.dataset.note === note;
        closeActivePopover();
        if (wasSame) return;

        await (touch ? openSheet(note) : openPanel(note));
      };
      // addEventListener ждёт void-функцию, а обработчик асинхронный. Обёртка
      // отбрасывает промис явным void — сам обработчик забирает свои ошибки
      // внутренним catch, так что терять здесь нечего.
      const onClickSync = (e: Event) => {
        void onClick(e as MouseEvent);
      };
      bind(a, 'pointerdown', onPointerDown);
      bind(a, 'click', onClickSync);
    }
    document.addEventListener('click', onDocClick);
    document.addEventListener('keydown', onKey);

    return () => {
      bound.forEach(([a, type, handler]) => a.removeEventListener(type, handler));
      document.removeEventListener('click', onDocClick);
      document.removeEventListener('keydown', onKey);
      closeActivePopover();
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
