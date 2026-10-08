/**
 * Подпись типа главы. Значения (`chapter`, `preface`, …) — данные: они уходят
 * в API и лежат в базе, переводится только показанное человеку.
 */
const LABELS: Record<string, string> = {
  chapter: 'глава',
  preface: 'предисловие',
  introduction: 'введение',
  appendix: 'приложение',
  epilogue: 'послесловие',
  part: 'часть',
  section: 'раздел',
  story: 'произведение',
};

export function chapterTypeLabel(type: string): string {
  return LABELS[type] ?? type;
}
