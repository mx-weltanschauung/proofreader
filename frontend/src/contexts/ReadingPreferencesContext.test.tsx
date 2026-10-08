import { describe, it, expect, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ReadingPreferencesProvider } from './ReadingPreferencesContext';
import { useReadingPrefs } from './readingPrefsContext';
import { DEFAULT_PREFS, loadPrefs } from './readingPrefs';

const Probe = () => {
  const { prefs, setPref, reset } = useReadingPrefs();
  return (
    <div>
      <span data-testid="size">{prefs.fontSize}</span>
      <button onClick={() => setPref('theme', 'oled')}>theme</button>
      <button onClick={() => setPref('measure', 'wide')}>measure</button>
      <button onClick={() => setPref('fontSize', 22)}>size</button>
      <button onClick={() => setPref('align', 'justify')}>align</button>
      <button onClick={reset}>reset</button>
    </div>
  );
};

const renderProbe = () =>
  render(
    <ReadingPreferencesProvider>
      <Probe />
    </ReadingPreferencesProvider>,
  );

describe('ReadingPreferencesProvider', () => {
  beforeEach(() => {
    localStorage.clear();
    for (const a of [...document.documentElement.attributes]) {
      if (a.name.startsWith('data-')) document.documentElement.removeAttribute(a.name);
    }
    document.documentElement.style.removeProperty('--reading-size');
  });

  it('writes every default preference to the root element', () => {
    renderProbe();
    const root = document.documentElement;
    expect(root.getAttribute('data-theme')).toBe('sepia');
    expect(root.getAttribute('data-reading-font')).toBe('literata');
    expect(root.getAttribute('data-reading-line')).toBe('normal');
    expect(root.getAttribute('data-reading-measure')).toBe('normal');
    expect(root.getAttribute('data-reading-spacing')).toBe('normal');
    expect(root.getAttribute('data-reading-paragraph')).toBe('indent');
    expect(root.getAttribute('data-reading-align')).toBe('left');
    expect(root.style.getPropertyValue('--reading-size')).toBe('18px');
  });

  it('reflects a changed preference on the root element', async () => {
    renderProbe();
    await userEvent.click(screen.getByText('measure'));
    expect(document.documentElement.getAttribute('data-reading-measure')).toBe('wide');
  });

  it('writes the font size as a px custom property', async () => {
    renderProbe();
    await userEvent.click(screen.getByText('size'));
    expect(document.documentElement.style.getPropertyValue('--reading-size')).toBe('22px');
  });

  it('persists changes to storage', async () => {
    renderProbe();
    await userEvent.click(screen.getByText('theme'));
    expect(loadPrefs(localStorage).theme).toBe('oled');
  });

  it('restores persisted preferences on mount', () => {
    localStorage.setItem('reading-prefs', JSON.stringify({ ...DEFAULT_PREFS, fontSize: 21 }));
    renderProbe();
    expect(screen.getByTestId('size')).toHaveTextContent('21');
  });

  it('reset returns every preference to its default', async () => {
    renderProbe();
    await userEvent.click(screen.getByText('align'));
    await userEvent.click(screen.getByText('theme'));
    await userEvent.click(screen.getByText('reset'));
    expect(document.documentElement.getAttribute('data-reading-align')).toBe('left');
    expect(document.documentElement.getAttribute('data-theme')).toBe('sepia');
  });

  it('resolves the auto theme against the OS preference', () => {
    const original = window.matchMedia;
    window.matchMedia = ((q: string) => ({
      matches: true,
      media: q,
      onchange: null,
      addEventListener: () => {},
      removeEventListener: () => {},
      addListener: () => {},
      removeListener: () => {},
      dispatchEvent: () => false,
    })) as unknown as typeof window.matchMedia;
    localStorage.setItem('reading-prefs', JSON.stringify({ ...DEFAULT_PREFS, theme: 'auto' }));
    renderProbe();
    expect(document.documentElement.getAttribute('data-theme')).toBe('dark');
    window.matchMedia = original;
  });
});
