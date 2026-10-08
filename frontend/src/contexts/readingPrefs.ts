export type ThemeSetting =
  | 'light'
  | 'dark'
  | 'sepia'
  | 'auto'
  | 'oled'
  | 'high-contrast'
  | 'gray-dim';
export type ResolvedTheme = Exclude<ThemeSetting, 'auto'>;
export type FontFamilySetting = 'literata' | 'fira' | 'andika';
export type LineHeightSetting = 'tight' | 'normal' | 'loose' | 'x-loose';
export type MeasureSetting = 'narrow' | 'normal' | 'wide' | 'full';
export type SpacingSetting = 'normal' | 'roomy' | 'x-roomy';
export type ParagraphSetting = 'indent' | 'spaced';
export type AlignSetting = 'left' | 'justify';

export interface ReadingPrefs {
  theme: ThemeSetting;
  fontFamily: FontFamilySetting;
  /** Whole pixels, FONT_SIZE_MIN..FONT_SIZE_MAX. */
  fontSize: number;
  lineHeight: LineHeightSetting;
  measure: MeasureSetting;
  spacing: SpacingSetting;
  paragraph: ParagraphSetting;
  align: AlignSetting;
  /** Показывать ли номера полос на поле рядом с текстом. */
  pageNumbers: boolean;
}

export const FONT_SIZE_MIN = 15;
export const FONT_SIZE_MAX = 24;

export const DEFAULT_PREFS: ReadingPrefs = {
  theme: 'sepia',
  fontFamily: 'literata',
  fontSize: 18,
  lineHeight: 'normal',
  measure: 'normal',
  spacing: 'normal',
  paragraph: 'indent',
  align: 'left',
  pageNumbers: true,
};

export const PREFS_KEY = 'reading-prefs';

const LEGACY_THEME_KEY = 'app-theme';
const LEGACY_FONT_KEY = 'reading-font-family';
const LEGACY_SIZE_KEY = 'reading-font-size';

const THEMES: ThemeSetting[] = [
  'light',
  'dark',
  'sepia',
  'auto',
  'oled',
  'high-contrast',
  'gray-dim',
];
const FONTS: FontFamilySetting[] = ['literata', 'fira', 'andika'];
const LINE_HEIGHTS: LineHeightSetting[] = ['tight', 'normal', 'loose', 'x-loose'];
const MEASURES: MeasureSetting[] = ['narrow', 'normal', 'wide', 'full'];
const SPACINGS: SpacingSetting[] = ['normal', 'roomy', 'x-roomy'];
const PARAGRAPHS: ParagraphSetting[] = ['indent', 'spaced'];
const ALIGNS: AlignSetting[] = ['left', 'justify'];

export function resolveTheme(setting: ThemeSetting, prefersDark: boolean): ResolvedTheme {
  if (setting === 'auto') return prefersDark ? 'dark' : 'light';
  return setting;
}

function pick<T extends string>(allowed: T[], value: unknown, fallback: T): T {
  return allowed.includes(value as T) ? (value as T) : fallback;
}

function pickSize(value: unknown, fallback: number): number {
  if (typeof value !== 'number' || !Number.isFinite(value)) return fallback;
  return Math.min(FONT_SIZE_MAX, Math.max(FONT_SIZE_MIN, Math.round(value)));
}

/** Coerce anything into a valid ReadingPrefs, field by field. */
export function sanitizePrefs(raw: unknown): ReadingPrefs {
  const o =
    typeof raw === 'object' && raw !== null && !Array.isArray(raw)
      ? (raw as Record<string, unknown>)
      : {};
  return {
    theme: pick(THEMES, o.theme, DEFAULT_PREFS.theme),
    fontFamily: pick(FONTS, o.fontFamily, DEFAULT_PREFS.fontFamily),
    fontSize: pickSize(o.fontSize, DEFAULT_PREFS.fontSize),
    lineHeight: pick(LINE_HEIGHTS, o.lineHeight, DEFAULT_PREFS.lineHeight),
    measure: pick(MEASURES, o.measure, DEFAULT_PREFS.measure),
    spacing: pick(SPACINGS, o.spacing, DEFAULT_PREFS.spacing),
    paragraph: pick(PARAGRAPHS, o.paragraph, DEFAULT_PREFS.paragraph),
    align: pick(ALIGNS, o.align, DEFAULT_PREFS.align),
    // Строго булево, а не приведение к истинности: настройка заведена позже
    // остальных, и у читателя, сохранившего их раньше, поля просто нет —
    // умолчание обязано быть «показывать», а не то, во что превратится
    // undefined.
    pageNumbers: typeof o.pageNumbers === 'boolean' ? o.pageNumbers : DEFAULT_PREFS.pageNumbers,
  };
}

const LEGACY_FONTS: Record<string, FontFamilySetting> = {
  serif: 'literata',
  sans: 'fira',
};
const LEGACY_SIZES: Record<string, number> = {
  normal: 18,
  large: 20,
  xlarge: 22,
};

/** Read the three pre-2026-08 keys, if the new key is absent. */
function migrateLegacy(storage: Storage): Partial<ReadingPrefs> | null {
  const theme = storage.getItem(LEGACY_THEME_KEY);
  const font = storage.getItem(LEGACY_FONT_KEY);
  const size = storage.getItem(LEGACY_SIZE_KEY);
  if (theme === null && font === null && size === null) return null;

  const migrated: Partial<ReadingPrefs> = {};
  if (theme !== null) migrated.theme = theme as ThemeSetting;
  if (font !== null && font in LEGACY_FONTS) migrated.fontFamily = LEGACY_FONTS[font];
  if (size !== null && size in LEGACY_SIZES) migrated.fontSize = LEGACY_SIZES[size];

  storage.removeItem(LEGACY_THEME_KEY);
  storage.removeItem(LEGACY_FONT_KEY);
  storage.removeItem(LEGACY_SIZE_KEY);
  return migrated;
}

export function loadPrefs(storage: Storage): ReadingPrefs {
  try {
    const stored = storage.getItem(PREFS_KEY);
    if (stored !== null) {
      return sanitizePrefs(JSON.parse(stored));
    }
    const legacy = migrateLegacy(storage);
    if (legacy) {
      const prefs = sanitizePrefs({ ...DEFAULT_PREFS, ...legacy });
      savePrefs(storage, prefs);
      return prefs;
    }
  } catch {
    // Unparseable JSON, or a storage that denies access (private mode,
    // blocked cookies). Reading must still work — fall through to defaults.
  }
  return DEFAULT_PREFS;
}

export function savePrefs(storage: Storage, prefs: ReadingPrefs): void {
  try {
    storage.setItem(PREFS_KEY, JSON.stringify(prefs));
  } catch {
    // Storage full or denied. Preferences stay in memory for this session.
  }
}
