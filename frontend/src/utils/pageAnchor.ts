/**
 * Якорь секции страницы в читалке: по нему прокручивают и оглавление главы, и
 * блок «вверх», и по нему же находит секции useVisiblePage.
 *
 * Живёт отдельным модулем, а не рядом с ReadingSurface: имя `chapter-page-<N>`
 * — договор между читалкой, ChapterTocDrawer и прыжком к подглаве, и собирать
 * его все должны одной функцией. Экспорт из файла с компонентом ломает
 * fast-refresh, а дубль строки в двух местах — ровно тот способ, которым такой
 * договор тихо расходится.
 */
export function pageAnchorId(pageNumber: number): string {
  return `chapter-page-${pageNumber}`;
}

/**
 * Обратный разбор: номер полосы из хэша адреса вида `#chapter-page-123`.
 * Только чтение — прыжком по хэшу занят useHashAnchor, дублировать его
 * незачем. Нужен ReadingSurface, чтобы сузить ?quote= до конкретной полосы
 * главы (см. useQuoteHighlight): без якоря в адресе — `undefined`, и
 * useQuoteHighlight в этом случае поиск не начинает вовсе — это не запасной
 * путь по всей главе, а отказ подтверждать место, которого адрес не назвал.
 */
export function pageNumberFromHash(hash: string): number | undefined {
  const match = /^#?chapter-page-(\d+)$/.exec(hash);
  if (!match) return undefined;
  const n = Number(match[1]);
  return Number.isFinite(n) ? n : undefined;
}
