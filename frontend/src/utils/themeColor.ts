/**
 * Значение цветового токена активной темы. Тема ставится атрибутом
 * data-theme на <html>, поэтому computed style корневого элемента —
 * единственное место, где видно то, что реально применилось.
 */
export function themeColor(name: string): string {
  return getComputedStyle(document.documentElement).getPropertyValue(name).trim();
}

/** Токены, которые нужны тосту react-hot-toast. */
export interface ToasterColors {
  cardBg: string;
  textPrimary: string;
  cardBorder: string;
  shadow: string;
  success: string;
  error: string;
}

/** Читает весь набор токенов тоста одним вызовом. */
export function readToasterColors(): ToasterColors {
  return {
    cardBg: themeColor('--color-card-bg'),
    textPrimary: themeColor('--color-text-primary'),
    cardBorder: themeColor('--color-card-border'),
    shadow: themeColor('--color-shadow'),
    success: themeColor('--color-success'),
    error: themeColor('--color-error'),
  };
}
