/**
 * Дата тем же русским видом, что печатает бэкенд (`pkg/book.RussianDate`) —
 * «12 сентября 2026 г.». Используется отдельно от дат в DocumentList,
 * MySuggestions, DocumentView и SuggestionQueue: те читают `toLocaleDateString`
 * без явных опций и меняться этой задачей не должны.
 *
 * Родной формат ru-RU (ICU) уже кладёт суффикс «г.» сам, в составе года —
 * проверено `formatToParts`: последний токен года несёт литерал " г.".
 * Наивное приписывание суффикса поверх удвоило бы его («…2026 г. г.»), а
 * рассчитывать на отсутствие суффикса у чужой реализации ICU — рискованно:
 * суффикс добавляется, только если форматтер сам его не поставил.
 */
const FORMATTER = new Intl.DateTimeFormat('ru-RU', {
  day: 'numeric',
  month: 'long',
  year: 'numeric',
});

export function russianDate(iso: string): string {
  const date = new Date(iso);
  // Кривая или пустая строка с сервера не должна ронять экран целиком:
  // Intl.DateTimeFormat.format на Invalid Date бросает RangeError, а на
  // маршруте страницы (PageView) границы ошибок нет — до неё дошёл бы весь
  // экран, а не только дата правки. Возвращаем исходную строку как есть:
  // это честнее пустого места и не выдаёт date за настоящую.
  if (Number.isNaN(date.getTime())) return iso;
  const formatted = FORMATTER.format(date);
  return formatted.endsWith('г.') ? formatted : `${formatted} г.`;
}
