import { describe, it, expect, afterEach, vi } from 'vitest';
import { openNoteSheet } from './noteSheet';
import { closeActivePopover, getActivePopover, openPopover } from './popoverPlacement';

const sheet = () => document.querySelector('.note-sheet');

const anchorEl = () => {
  const a = document.createElement('a');
  a.href = '#fn:12';
  document.body.appendChild(a);
  return a;
};

describe('openNoteSheet', () => {
  afterEach(() => {
    closeActivePopover();
    document.body.innerHTML = '';
  });

  it('opens a sheet titled after the note, with the body the caller fills', () => {
    const handle = openNoteSheet(anchorEl(), 'Примечание 12');
    handle.body.textContent = 'Тело примечания';

    expect(sheet()?.textContent).toContain('Примечание 12');
    expect(sheet()?.textContent).toContain('Тело примечания');
  });

  it('closes on the close button', () => {
    openNoteSheet(anchorEl(), 'Примечание 12');

    document.querySelector<HTMLButtonElement>('.note-sheet-close')!.click();

    expect(sheet()).toBeNull();
    expect(getActivePopover()).toBeNull();
  });

  it('closes when the tap lands on the dimmed page behind it', () => {
    openNoteSheet(anchorEl(), 'Примечание 12');

    document.querySelector<HTMLElement>('.note-sheet-scrim')!.click();

    expect(sheet()).toBeNull();
  });

  it('stays open when the tap lands inside the sheet', () => {
    const handle = openNoteSheet(anchorEl(), 'Примечание 12');
    handle.body.textContent = 'Тело примечания';

    handle.body.click();

    expect(sheet()).not.toBeNull();
  });

  it('offers the jump as an ordinary link when the note sits on another page', () => {
    const handle = openNoteSheet(anchorEl(), 'Примечание 12');

    handle.setGoto('→ стр. 415', '/works/1/pages/415');

    const goto = document.querySelector<HTMLAnchorElement>('a.note-sheet-goto');
    expect(goto?.textContent).toBe('→ стр. 415');
    expect(goto?.getAttribute('href')).toBe('/works/1/pages/415');
  });

  it('runs the jump and closes itself when the note is at the foot of this page', () => {
    const handle = openNoteSheet(anchorEl(), 'Примечание 12');
    const jump = vi.fn();

    handle.setGoto('→ к примечанию внизу', jump);
    document.querySelector<HTMLElement>('.note-sheet-goto')!.click();

    expect(jump).toHaveBeenCalledOnce();
    // Scrolling to the note under a sheet that still covers it helps nobody.
    expect(sheet()).toBeNull();
  });

  it('takes the single popover slot, so a sheet and a panel never overlap', () => {
    openNoteSheet(anchorEl(), 'Примечание 12');
    const panel = document.createElement('div');
    openPopover(panel, anchorEl());

    expect(sheet()).toBeNull();
    expect(getActivePopover()).toBe(panel);
  });
});
