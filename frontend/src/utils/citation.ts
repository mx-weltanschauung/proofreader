/**
 * Подпись цитаты и обе грани буфера обмена.
 *
 * Форма подписи:
 *
 *   [Автор. ][Произведение // ]Издание, т. N[, ч. P], с. 233.
 *
 * Автор — `work.author`; сегодня пуст у всего корпуса (находка 1 спеки), и
 * это ОСНОВНОЙ случай, а не край: слот просто молчит, подставлять на его
 * место название издания или что-либо ещё не нужно — первым словом издания
 * автор уже назван читателю.
 *
 * Издание — `work.edition_title`; работа вне собрания (поле пусто) —
 * подписывается собственным названием тома.
 *
 * Разделитель между частями источника — запятая, и это не всё равно какая
 * деталь: издания корпуса оканчиваются точкой («…Сочинения, 2-е изд.»), и
 * склейка через ". " печатала бы двойную точку. Двойника серверной
 * joinSourceParts в TypeScript заводить нельзя (решение 13 спеки) — та
 * функция решает свою, более общую задачу склейки произвольных источников
 * и живёт в Go; здесь ровно один фиксированный порядок полей, и обобщённый
 * джойнер был бы данью симметрии, а не нуждой.
 *
 * Адреса во входе намеренно нет: подпись возвращается БЕЗ него, а
 * приклеивают его citationMarkdown/citationHtml — в html-грани адрес обязан
 * стать живой ссылкой <a href>, а не текстом подписи.
 */
export interface SignatureInput {
  author: string;
  workTitle: string;
  editionTitle: string;
  volumeTitle: string;
  volumeNumber?: number;
  volumePart?: string;
  /** Печатные колонцифры первой и последней полосы цитаты. */
  folios: (string | null)[];
  /** Номера тех же полос. */
  pageNumbers: number[];
  /** Журнальные координаты: подпись идёт по-журнальному, без «т. N». */
  issue?: { journal: string; year: number; label: string };
}

/**
 * Обозначение полосы (или диапазона полос) в подписи: печатная колонцифра,
 * а нет её — «б/н, полоса N» со сквозным номером. Диапазон печатает
 * колонцифры/номера именно КОНЦОВ, а не первую и последнюю подряд идущие
 * колонцифры, — расхождение подписи с адресом (который метит начало и
 * конец выделения) названо в решении 15 спеки и остаётся осознанно.
 */
export function folioLabel(folios: (string | null)[], pageNumbers: number[]): string {
  // Пустой вход — не край, а обычный «нечего подписывать» (например, ключ
  // цитаты не сумел определить страницы). `folios.every(...)` на пустом
  // массиве истинно вакуумно, а folios[0] превращается в undefined — раньше
  // это печатало «с. undefined» вместо пустой строки, которую source.filter
  // (Boolean) в citationSignature просто убрал бы.
  if (pageNumbers.length === 0) return '';

  const first = 0;
  const last = pageNumbers.length - 1;
  // Смотрим на folios строго по позициям, которые называет pageNumbers, —
  // а не folios.every(...) по ВСЕЙ длине folios: массивы приходят с разных
  // концов подготовки цитаты и не обязаны совпадать длиной, и разболтавшийся
  // «хвост» folios не должен решать за диапазон, который печатает pageNumbers.
  const firstFolio = folios[first] ?? null;
  const lastFolio = folios[last] ?? null;
  const allFolios =
    firstFolio !== null && firstFolio !== '' && lastFolio !== null && lastFolio !== '';

  if (pageNumbers.length <= 1) {
    return allFolios ? `с. ${firstFolio}` : `б/н, полоса ${pageNumbers[first]}`;
  }
  return allFolios
    ? `с. ${firstFolio}—${lastFolio}`
    : `б/н, полосы ${pageNumbers[first]}—${pageNumbers[last]}`;
}

/** Подпись цитаты без адреса — его приклеивает вызывающая грань буфера. */
export function citationSignature(i: SignatureInput): string {
  const head: string[] = [];
  const author = i.author.trim();
  const work = i.workTitle.trim();
  // Подпись бывает уже с точкой на конце — инициал после фамилии у
  // «Большевика» («Троицкий, А.»): вторая точка была бы опечаткой.
  if (author) head.push(author.endsWith('.') ? author : `${author}.`);
  if (work) head.push(`${work} //`);

  if (i.issue) {
    const folio = folioLabel(i.folios, i.pageNumbers);
    const tail = [
      i.issue.journal.trim(),
      String(i.issue.year),
      `№ ${i.issue.label}`,
      folio ? folio[0].toUpperCase() + folio.slice(1) : '',
    ].filter(Boolean);
    return [...head, `${tail.join('. ')}.`].join(' ');
  }

  const source: string[] = [i.editionTitle.trim() || i.volumeTitle.trim()];
  if (i.volumeNumber !== undefined && i.volumeNumber !== null) {
    source.push(`т. ${i.volumeNumber}`);
    if (i.volumePart) source.push(`ч. ${i.volumePart}`);
  }
  source.push(folioLabel(i.folios, i.pageNumbers));

  return [...head, `${source.filter(Boolean).join(', ')}.`].join(' ');
}

/**
 * Маркер стыка полос в самом тексте цитаты — на каждом стыке, кроме
 * первого (первая полоса уже названа в подписи).
 */
export function seamMarker(folio: string | null, pageNumber: number): string {
  return folio ? `[с. ${folio}]` : `[полоса ${pageNumber}]`;
}

/**
 * text/plain-грань буфера: markdown-цитата, пустая строка, подпись, адрес
 * СВОЕЙ строкой.
 *
 * Отбивка адреса от подписи — не вкусовщина, а жалоба читателя: подпись
 * кончается сокращением с точкой («…, с. 370.»), и приклеенный к ней через
 * пробел адрес не взять ни двойным щелчком, ни выделением строки, не захватив
 * хвост подписи. Строка целиком — ровно то, чем адрес и является: единственный
 * носитель состояния во всей цитате. Html-грань отбивает его тем же смыслом
 * через <br>, чтобы обе грани буфера говорили одно и то же.
 */
export function citationMarkdown(text: string, signature: string, url: string): string {
  const quoted = text
    .split('\n')
    .map((line) => `> ${line}`.replace(/\s+$/, ''))
    .join('\n');
  return `${quoted}\n\n${citationLinkMarkdown(signature, url)}`;
}

/**
 * text/plain-грань для ветки «Ссылка» (без выделения): подпись и адрес, без
 * цитаты.
 *
 * Отдельная функция, а не строка по месту в CiteButton.tsx, по той же
 * причине, что и citationLinkHtml ниже: форма подписи с адресом — одна на обе
 * ветки, и вторая её копия разъехалась бы с первой при первой же правке.
 * Именно так и вышло: адрес переехал на свою строку в citationMarkdown, а
 * собранная по месту строка ветки «Ссылка» осталась бы с прежней склейкой.
 */
export function citationLinkMarkdown(signature: string, url: string): string {
  return `${signature}\n${url}\n`;
}

function esc(s: string): string {
  return s
    .replace(/&/g, '&amp;')
    .replace(/</g, '&lt;')
    .replace(/>/g, '&gt;')
    .replace(/"/g, '&quot;');
}

/**
 * text/html-грань буфера: blockquote с текстом и подписью, ссылка живая —
 * она единственный носитель состояния, а не украшение.
 */
export function citationHtml(text: string, signature: string, url: string): string {
  const body = text
    .split('\n')
    .map((line) => esc(line))
    .join('<br>');
  const link = esc(url);
  return `<blockquote><p>${body}</p><p>${esc(signature)}<br><a href="${link}">${link}</a></p></blockquote>`;
}

/**
 * text/html-грань буфера для ветки «Ссылка» (без выделения): подпись и
 * рабочий адрес, без цитаты.
 *
 * Отдельная функция, а не собранный по месту тег в CiteButton.tsx: подпись и
 * адрес обязаны пройти через тот же esc(), что и citationHtml — иначе имя
 * автора со знаком `<`/`&` (или адрес со спецсимволом в слаге) сломало бы
 * разметку письма/документа, куда цитата вставляется. Двух копий экранирования
 * быть не должно (фикс-раунд 1, мелкое б).
 */
export function citationLinkHtml(signature: string, url: string): string {
  const link = esc(url);
  return `<p>${esc(signature)}<br><a href="${link}">${link}</a></p>`;
}
