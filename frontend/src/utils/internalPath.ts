/** Путь внутри читальни — или пустая строка.
 *
 *  Значение приходит из адресной строки, а рисуется ссылкой «Вернуться к
 *  чтению» на доверенной странице. Без проверки `?from=https://чужой.сайт`
 *  даёт настоящий увод наружу с невинной подписью. Те же правила, что у
 *  normalizeSourcePath на бэкенде (internal/api/public_form.go).
 */
export function internalPath(raw: string): string {
  const path = raw.trim();
  if (!path || path.length > 500) return '';
  // «//host» — это адрес по схеме страницы, уводит не хуже полного URL.
  if (!path.startsWith('/') || path.startsWith('//')) return '';
  // Обратный слэш браузеры местами читают как прямой.
  if (path.includes('\\')) return '';
  // Управляющие символы (включая перевод строки) и пробельные — как на
  // сервере. Диапазон не от пробела: слаги вроде "istoricheskiy-materializm"
  // содержат дефис, и его нельзя задеть.
  // eslint-disable-next-line no-control-regex
  if (/[\x00-\x1f\x7f\s]/.test(path)) return '';
  return path;
}
