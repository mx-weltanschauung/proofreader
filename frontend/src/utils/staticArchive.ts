// Подписи архива статической читальни в /help#offline
// (components/StaticArchiveSection.tsx).

const MONTHS = [
  'января',
  'февраля',
  'марта',
  'апреля',
  'мая',
  'июня',
  'июля',
  'августа',
  'сентября',
  'октября',
  'ноября',
  'декабря',
];

/** «2026-10-06» → «6 октября 2026 г.», тем же видом, что utils/russianDate.
 *  Не через неё: та печатает момент времени, а дата сборки — календарный
 *  день, и полночь UTC у читателя западнее Гринвича уехала бы на сутки назад. */
export function archiveDate(iso: string): string {
  const m = /^(\d{4})-(\d{2})-(\d{2})$/.exec(iso);
  if (!m) return iso;
  return `${Number(m[3])} ${MONTHS[Number(m[2]) - 1]} ${m[1]} г.`;
}

/** Размер в мебибайтах — так его покажет проводник: 418 756 548 Б → «399 МБ». */
export function archiveSize(bytes: number): string {
  return `${Math.round(bytes / 1024 / 1024)} МБ`;
}
