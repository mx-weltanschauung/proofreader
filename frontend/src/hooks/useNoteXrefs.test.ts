import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';

// The hook fetches the work's note index on click; the popover's keyboard
// behaviour does not depend on what comes back.
vi.mock('../services/notesIndex', () => ({
  getWorkNotesIndex: vi.fn(async () => new Map()),
}));

import { getWorkNotesIndex } from '../services/notesIndex';
import { useNoteXrefs } from './useNoteXrefs';
import { closeActivePopover, getActivePopover } from './popoverPlacement';

describe('useNoteXrefs keyboard access', () => {
  let container: HTMLDivElement;
  let anchor: HTMLAnchorElement;

  beforeEach(() => {
    container = document.createElement('div');
    container.innerHTML = '<p>Текст <a class="note-xref" data-note="7" href="#">7</a></p>';
    document.body.appendChild(container);
    anchor = container.querySelector<HTMLAnchorElement>('a.note-xref')!;
    renderHook(() => useNoteXrefs({ current: container }, 3, undefined, []));
  });

  afterEach(() => {
    closeActivePopover();
    document.body.innerHTML = '';
    vi.clearAllMocks();
  });

  it('moves focus into the popover so it can be scrolled with the keyboard', () => {
    anchor.click();
    const pop = getActivePopover();
    expect(pop).not.toBeNull();
    expect(document.activeElement).toBe(pop);
  });

  it('announces the popover as a labelled dialog', () => {
    anchor.click();
    const pop = getActivePopover()!;
    expect(pop.getAttribute('role')).toBe('dialog');
    expect(pop.getAttribute('aria-label')).toBe('Примечание 7');
  });

  it('closes on Escape and returns focus to the anchor', () => {
    anchor.click();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape' }));
    expect(getActivePopover()).toBeNull();
    expect(document.activeElement).toBe(anchor);
  });

  it('returns focus to the anchor when the popover is dismissed by a click outside', () => {
    anchor.click();
    document.body.click();
    expect(getActivePopover()).toBeNull();
    expect(document.activeElement).toBe(anchor);
  });

  it('consumes the Escape that closes the popover, and only that one', () => {
    const onWindowEscape = vi.fn();
    window.addEventListener('keydown', onWindowEscape);

    anchor.click();
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(getActivePopover()).toBeNull();
    expect(onWindowEscape).not.toHaveBeenCalled();

    // With nothing open, the key belongs to whoever else wants it.
    document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
    expect(onWindowEscape).toHaveBeenCalledTimes(1);

    window.removeEventListener('keydown', onWindowEscape);
  });
});

describe('useNoteXrefs на тач-экране', () => {
  let container: HTMLDivElement;
  let anchor: HTMLAnchorElement;

  const sheet = () => document.querySelector('.note-sheet');
  // jsdom не знает PointerEvent; хук смотрит на тип указателя.
  const down = (el: Element, pointerType: string) => {
    const e = new MouseEvent('pointerdown', { bubbles: true });
    Object.defineProperty(e, 'pointerType', { value: pointerType });
    el.dispatchEvent(e);
  };

  beforeEach(() => {
    vi.mocked(getWorkNotesIndex).mockResolvedValue(
      new Map([
        [7, { number: 7, body_html: '<p>Тело примечания 7</p>', target_page: 415 }],
      ]) as never,
    );
    container = document.createElement('div');
    container.innerHTML = '<p>Текст <a class="note-xref" data-note="7" href="#">7</a></p>';
    document.body.appendChild(container);
    anchor = container.querySelector<HTMLAnchorElement>('a.note-xref')!;
    renderHook(() => useNoteXrefs({ current: container }, 3, undefined, []));
  });

  afterEach(() => {
    closeActivePopover();
    document.body.innerHTML = '';
    vi.clearAllMocks();
  });

  it('по тапу открывает лист с телом примечания', async () => {
    down(anchor, 'touch');
    anchor.click();
    await vi.waitFor(() => expect(sheet()?.textContent).toContain('Тело примечания 7'));

    expect(document.querySelector('.note-xref-popover')).toBeNull();
  });

  it('уводит переход из листа на страницу примечания', async () => {
    down(anchor, 'touch');
    anchor.click();
    await vi.waitFor(() => expect(document.querySelector('.note-sheet-goto')).not.toBeNull());

    const goto = document.querySelector<HTMLAnchorElement>('a.note-sheet-goto')!;
    expect(goto.getAttribute('href')).toBe('/works/3/pages/415');
    expect(goto.textContent).toContain('415');
  });

  it('оставляет мыши прежнюю якорную панель', async () => {
    down(anchor, 'mouse');
    anchor.click();
    await vi.waitFor(() =>
      expect(document.querySelector('.note-xref-popover')?.textContent).toContain(
        'Тело примечания 7',
      ),
    );

    expect(sheet()).toBeNull();
  });
});
