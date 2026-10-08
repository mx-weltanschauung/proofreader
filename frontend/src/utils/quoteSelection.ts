import { flattenText, normalizeQuote, pageOfNode } from './quoteMatch';

export interface SelectedQuote {
  /**
   * Полосы в порядке ПЕРЕХОДОВ внутри выделения, не отсортированный и не
   * уникальный список: [5,6] у обычного выделения через стык, но [6,5,6],
   * если выделение началось внутри шва (см. тест «начинается в шве»). Этот
   * порядок нужен ровно для одного — знать, где в `text` печатать маркер.
   * Адрес цитаты — pages[0] (полоса, где выделение НАЧАЛОСЬ); диапазон для
   * подписи — концы pageSpan, а не концы этого массива.
   */
  pages: number[];
  /** Отсортированный список уникальных полос, задетых выделением. */
  pageSpan: number[];
  /**
   * Текст цитаты ДЛЯ ЧЕЛОВЕКА: границы абзацев сохранены переводом строки
   * (фикс-раунд 1, п. 5). В отличие от head/firstPageText ниже, эта строка
   * никогда не участвует в сравнении — только уходит в буфер, поэтому она
   * вправе нести `\n`, который normalizeQuote (нужный СРАВНЕНИЮ) стирает
   * наравне с пробелом.
   */
  text: string;
  head: string;
  firstPageText: string;
  /**
   * Зеркало head: последний непрерывный кусок выделения, лежащий на полосе,
   * где оно КОНЧАЕТСЯ. Из него режется якорь конца (`quoteTail`), как из
   * head — ключ начала.
   */
  tail: string;
  /**
   * Весь текст полосы, на которой выделение кончается, — та полоса, по
   * которой считается уникальность якоря конца. У выделения внутри одной
   * полосы совпадает с firstPageText; через стык это разные полосы.
   */
  lastPageText: string;
}

/**
 * Весь текст одной полосы в живом DOM.
 *
 * Полоса живёт в двух местах: её собственная секция и — если начало уехало в
 * абзац предыдущей — шов внутри той секции. Оба несут data-page со своим
 * номером, поэтому берём знаки, у которых ближайший data-page равен нужному:
 * иначе текст полосы N вобрал бы в себя шов полосы N+1, лежащий внутри неё.
 *
 * Склейка шва и собственной секции держится на ВНЕШНЕМ инварианте, который
 * этот модуль не контролирует: между двумя блочными элементами страницы
 * (двумя <p>) в HTML, который отдаёт сервер, должен остаться настоящий
 * текстовый узел (перенос строки или пробел) — тогда он читается как символ
 * страницы, ближайший предок которой и несёт data-page, и склейка получает
 * разделяющий пробел даром. Без этого узла куски слипаются без пробела
 * («начало шестойостаток шестой»). Проверено вызовом настоящего stitchPages:
 * перенос строки между абзацами его переживает и остаётся текстовым узлом
 * внутри собственной секции полосы (quoteSelection.test.ts).
 *
 * Цена: полный проход по ВСЕМУ root, вторым проходом поверх flattenText
 * самого выделения. На синтетической главе в 742 полосы (jsdom)
 * readSelection целиком — 802 мс, из них этот проход — 366 мс. Звать это на
 * конце выделения (mouseup/selectionchange, уже отпущенном), а не на каждое
 * промежуточное событие выделения — иначе чтение будет заикаться на каждом
 * движении мыши.
 *
 * Номеров поэтому принимается СПИСОК: цитате нужны две полосы — та, где
 * выделение началось, и та, где кончилось, — и брать их двумя вызовами
 * значило бы платить этот проход дважды за один жест.
 */
export function pageTexts(root: HTMLElement, pageNumbers: number[]): Map<number, string> {
  const parts = new Map<number, string[]>();
  for (const n of pageNumbers) parts.set(n, []);

  const flat = flattenText(root);
  for (let i = 0; i < flat.text.length; i++) {
    const page = flat.pages[i];
    if (page === null) continue;
    parts.get(page)?.push(flat.text[i]);
  }

  const out = new Map<number, string>();
  for (const [n, chars] of parts) out.set(n, normalizeQuote(chars.join('')));
  return out;
}

/** Одна полоса — тот же проход, что и у pageTexts, ради одного номера. */
export function pageText(root: HTMLElement, pageNumber: number): string {
  return pageTexts(root, [pageNumber]).get(pageNumber) ?? '';
}

/**
 * Диапазон целиком лежит внутри одного непечатаемого читателю элемента —
 * колонцифры шва/маркера (.page-marker) или формулы KaTeX (.katex).
 *
 * flattenText отсеивает оба класса ПО ПРЕДКУ, но только в живом дереве:
 * Range.cloneContents, когда весь диапазон лежит внутри одного элемента, не
 * переносит в клон сам этот элемент — переносит голый текстовый узел без
 * class и data-page. Поэтому фильтр в клоне не за что зацепиться, и двойной
 * клик по цифре «265» в <a class="page-marker">265</a> дал бы SelectedQuote с
 * text="265" — а markQuote честно нашёл бы и подсветил ДРУГОЕ «265» на той же
 * полосе (например, «в 265 году»), не то место, что читатель выделил. Раз
 * маркер стоит прямо в потоке абзаца (шов) или перед секцией, а не скрыт
 * user-select:none, двойной клик по цифре — обычное читательское движение, а
 * не редкий случай. Проверяется на живом range, ДО клонирования.
 */
export function fullyInsideExcluded(range: Range): boolean {
  const container = range.commonAncestorContainer;
  const el =
    container.nodeType === Node.ELEMENT_NODE ? (container as HTMLElement) : container.parentElement;
  return !!el?.closest('.page-marker, .katex');
}

/** Теги, отделяющие один абзац цитаты от другого (фикс-раунд 1, п. 5). */
const PARAGRAPH_TAGS = 'p, li, blockquote, h1, h2, h3, h4, h5, h6, td, th';

/**
 * Ближайший блочный предок знака клона — по нему ловится граница абзаца.
 *
 * Специально БЕЗ div: обёрточные `.page-html-content`/`.chapter-page-section`
 * — тоже div, и попади они в список, транзит между швом (лежащим внутри <p>
 * предыдущей полосы, stitchPages.ts) и собственной секцией той же полосы дал
 * бы ложную границу на каждом стыке. Настоящий абзац в вёрстке сервера всегда
 * <p> (или li/blockquote/заголовок/ячейка таблицы) — этого достаточно.
 */
function nearestBlock(node: Text): Element | null {
  return node.parentElement?.closest(PARAGRAPH_TAGS) ?? null;
}

/**
 * Мягкая нормализация ГОТОВОГО текста цитаты для человека.
 *
 * В отличие от normalizeQuote (нужного СРАВНЕНИЮ — head, firstPageText, ключ
 * адреса), эта функция не трогает сам перевод строки — схлопывает и обрезает
 * только обычные пробелы внутри строки. Схлопывание всё же нужно: на границе
 * абзаца, склеенной швом, рядом ложатся ДВА пробела — родной пробел-склейка
 * шва (stitchPages.ts, `tail.append(' ')`) и пробел маркера полосы
 * (` ${marker(page)} `), — а на самой строке normalizeQuoteForHuman переводы
 * строк не трогает, поэтому схлопывать их вместо неё некому.
 */
function normalizeQuoteForHuman(s: string): string {
  return s
    .split('\n')
    .map((line) => line.replace(/[^\S\n]+/g, ' ').trim())
    .join('\n')
    .trim();
}

/**
 * Что читатель выделил: полосы, текст с маркерами стыков и голова на первой
 * полосе, из которой режется ключ адреса.
 *
 * Полоса НАЧАЛА берётся из живого DOM, а переходы — из клона выделения:
 * Range.cloneContents вбирает частично задетых предков только внутри общего
 * предка диапазона, и у выделения в пределах одного абзаца в клоне не будет
 * ни одного data-page вовсе.
 *
 * Требование к root, которое этот модуль не проверяет и проверить не может:
 * каждый его знак обязан иметь предка с data-page. Знаки без него молча
 * приписываются полосе НАЧАЛА (см. pageAt) и склеиваются с ней в `text` без
 * какого-либо разделителя. Сегодня это недостижимо (сноски главы в потоке
 * чтения — сосед контейнера полос, а не его часть), но станет живым, как
 * только задача 11 передаст сюда более широкий корень вроде <article>:
 * выделение из полосы в блок сносок дало бы text="текст пятойтело сноски" —
 * несуществующее слово, которого читатель не выделял.
 */
export function readSelection(
  root: HTMLElement,
  marker: (pageNumber: number) => string,
): SelectedQuote | null {
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return null;

  const range = selection.getRangeAt(0);
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return null;
  if (fullyInsideExcluded(range)) return null;

  const startPage = pageOfNode(range.startContainer);
  if (startPage === null) return null;

  const clone = document.createElement('div');
  clone.append(range.cloneContents());
  const flat = flattenText(clone);
  if (normalizeQuote(flat.text) === '') return null;

  // Знаки клона, у которых своего data-page нет, принадлежат полосе начала.
  const pageAt = (i: number): number => flat.pages[i] ?? startPage;

  const pages: number[] = [];
  let text = '';
  let head = '';
  // Хвост — зеркало головы, и собирается тем же правилом с другого конца:
  // каждый переход на новую полосу начинает его заново, поэтому к концу
  // обхода в нём остаётся последний непрерывный кусок последней полосы
  // перехода. Склеивать все знаки этой полосы по всему выделению нельзя по
  // той же причине, по какой голова кончается на первом разрыве: на
  // выделении, начавшемся в шве (pages = [6,5,6]), это склеило бы голову
  // полосы 6 с её остатком через лежащий между ними текст полосы 5 —
  // строку-фантом, которой на полосе нет, и якорь конца искался бы в ней.
  let tail = '';
  // Голова обязана кончиться на ПЕРВОМ разрыве, а не собирать все знаки
  // первой полосы по всему выделению. В потоке чтения (ReadingChunk.tsx)
  // сноски полосы рисуются между её текстом и следующей полосой, поэтому
  // голова полосы N (в шве внутри абзаца полосы N−1) и остаток полосы N
  // разделены сносками N−1 — те снова несут номер первой полосы. Без этого
  // флага голова склеила бы через чужой кусок текст, которого читатель не
  // видел подряд, и quoteKey искал бы в pageText строку-фантом.
  let headBroken = false;
  // Ближайший блочный предок последнего добавленного НЕпробельного знака —
  // ловит смену абзаца внутри одной и той же полосы (фикс-раунд 1, п. 5).
  // null до первого знака: началу цитаты не с чем сравнивать.
  let lastContentBlock: Element | null = null;
  for (let i = 0; i < flat.text.length; i++) {
    const page = pageAt(i);
    const ch = flat.text[i];
    if (pages.length === 0) {
      pages.push(page);
    } else if (page !== pages[pages.length - 1]) {
      pages.push(page);
      text += ` ${marker(page)} `;
      tail = '';
    } else if (
      ch !== ' ' &&
      lastContentBlock !== null &&
      nearestBlock(flat.nodes[i]) !== lastContentBlock
    ) {
      // Смена <p> внутри одной полосы — настоящая граница абзаца (например,
      // цитата из трёх абзацев). Перевод строки печатается ВМЕСТО пробела,
      // на котором стоял переход, — иначе, скажем, второй абзац полосы,
      // склеенный швом с концом первого, дал бы одну слитную строку.
      text = text.replace(/ $/, '') + '\n';
    }
    text += ch;
    tail += ch;
    if (ch !== ' ') lastContentBlock = nearestBlock(flat.nodes[i]);
    if (!headBroken) {
      if (page === pages[0]) head += ch;
      else headBroken = true;
    }
  }

  // pageSpan — отдельная величина от pages: не последовательность переходов,
  // а множество задетых полос, отсортированное по возрастанию. pages[0] и
  // pages[pages.length-1] для диапазона не годятся — при старте в шве
  // (pages=[6,5,6]) края этой последовательности дали бы подписи «с. 6—6» на
  // цитате, которая на самом деле тянется с 5 по 6.
  const pageSpan = [...new Set(pages)].sort((a, b) => a - b);
  const lastPage = pages[pages.length - 1];
  const texts = pageTexts(root, [pages[0], lastPage]);

  return {
    pages,
    pageSpan,
    text: normalizeQuoteForHuman(text),
    head: normalizeQuote(head),
    firstPageText: texts.get(pages[0]) ?? '',
    tail: normalizeQuote(tail),
    lastPageText: texts.get(lastPage) ?? '',
  };
}
