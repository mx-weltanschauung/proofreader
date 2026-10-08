import { describe, it, expect, beforeEach } from 'vitest';
import {
  resolveTheme,
  sanitizePrefs,
  loadPrefs,
  savePrefs,
  DEFAULT_PREFS,
  PREFS_KEY,
  FONT_SIZE_MIN,
  FONT_SIZE_MAX,
} from './readingPrefs';

describe('resolveTheme', () => {
  it('auto -> dark when prefersDark', () => expect(resolveTheme('auto', true)).toBe('dark'));
  it('auto -> light when not prefersDark', () => expect(resolveTheme('auto', false)).toBe('light'));
  it('explicit themes pass through', () => {
    expect(resolveTheme('oled', true)).toBe('oled');
    expect(resolveTheme('sepia', false)).toBe('sepia');
  });
});

describe('sanitizePrefs', () => {
  it('returns defaults for junk input', () => {
    expect(sanitizePrefs(null)).toEqual(DEFAULT_PREFS);
    expect(sanitizePrefs('nonsense')).toEqual(DEFAULT_PREFS);
    expect(sanitizePrefs(42)).toEqual(DEFAULT_PREFS);
    expect(sanitizePrefs([])).toEqual(DEFAULT_PREFS);
  });

  it('drops unknown enum values field by field', () => {
    const result = sanitizePrefs({ theme: 'chartreuse', fontFamily: 'fira' });
    expect(result.theme).toBe(DEFAULT_PREFS.theme);
    expect(result.fontFamily).toBe('fira');
  });

  it('keeps every valid value', () => {
    const all = {
      theme: 'oled',
      fontFamily: 'andika',
      fontSize: 21,
      lineHeight: 'loose',
      measure: 'wide',
      spacing: 'x-roomy',
      paragraph: 'spaced',
      align: 'justify',
      pageNumbers: false,
    };
    expect(sanitizePrefs(all)).toEqual(all);
  });

  it('turns page numbers on when the stored prefs predate the setting', () => {
    // Настройка заведена позже остальных: у читателя, сохранившего настройки
    // до неё, поля в localStorage нет — и номера обязаны включиться, а не
    // достаться ему выключенными от пустого значения.
    expect(sanitizePrefs({ theme: 'oled' }).pageNumbers).toBe(true);
  });

  it('keeps page numbers switched off, but only for a real false', () => {
    expect(sanitizePrefs({ pageNumbers: false }).pageNumbers).toBe(false);
    expect(sanitizePrefs({ pageNumbers: 'нет' }).pageNumbers).toBe(true);
    expect(sanitizePrefs({ pageNumbers: 0 }).pageNumbers).toBe(true);
  });

  it('clamps font size into range and rounds to whole pixels', () => {
    expect(sanitizePrefs({ fontSize: 3 }).fontSize).toBe(FONT_SIZE_MIN);
    expect(sanitizePrefs({ fontSize: 99 }).fontSize).toBe(FONT_SIZE_MAX);
    expect(sanitizePrefs({ fontSize: 18.7 }).fontSize).toBe(19);
    expect(sanitizePrefs({ fontSize: 'big' }).fontSize).toBe(DEFAULT_PREFS.fontSize);
    expect(sanitizePrefs({ fontSize: NaN }).fontSize).toBe(DEFAULT_PREFS.fontSize);
  });
});

describe('loadPrefs', () => {
  beforeEach(() => localStorage.clear());

  it('returns defaults on empty storage', () => {
    expect(loadPrefs(localStorage)).toEqual(DEFAULT_PREFS);
  });

  it('returns defaults on unparseable JSON', () => {
    localStorage.setItem(PREFS_KEY, '{not json');
    expect(loadPrefs(localStorage)).toEqual(DEFAULT_PREFS);
  });

  it('round-trips through savePrefs', () => {
    const prefs = { ...DEFAULT_PREFS, theme: 'dark' as const, fontSize: 20 };
    savePrefs(localStorage, prefs);
    expect(loadPrefs(localStorage)).toEqual(prefs);
  });

  it('migrates the three legacy keys', () => {
    localStorage.setItem('app-theme', 'sepia');
    localStorage.setItem('reading-font-family', 'sans');
    localStorage.setItem('reading-font-size', 'xlarge');
    const prefs = loadPrefs(localStorage);
    expect(prefs.theme).toBe('sepia');
    expect(prefs.fontFamily).toBe('fira');
    expect(prefs.fontSize).toBe(22);
  });

  it('maps legacy serif to literata and normal size to 18', () => {
    localStorage.setItem('reading-font-family', 'serif');
    localStorage.setItem('reading-font-size', 'normal');
    const prefs = loadPrefs(localStorage);
    expect(prefs.fontFamily).toBe('literata');
    expect(prefs.fontSize).toBe(18);
  });

  it('removes legacy keys once migrated', () => {
    localStorage.setItem('app-theme', 'dark');
    loadPrefs(localStorage);
    expect(localStorage.getItem('app-theme')).toBeNull();
  });

  it('prefers the new key over legacy keys', () => {
    savePrefs(localStorage, { ...DEFAULT_PREFS, theme: 'oled' });
    localStorage.setItem('app-theme', 'sepia');
    expect(loadPrefs(localStorage).theme).toBe('oled');
  });

  it('survives a storage that throws', () => {
    const hostile = {
      getItem() {
        throw new Error('denied');
      },
      setItem() {
        throw new Error('denied');
      },
      removeItem() {
        throw new Error('denied');
      },
    } as unknown as Storage;
    expect(loadPrefs(hostile)).toEqual(DEFAULT_PREFS);
    expect(() => savePrefs(hostile, DEFAULT_PREFS)).not.toThrow();
  });
});
