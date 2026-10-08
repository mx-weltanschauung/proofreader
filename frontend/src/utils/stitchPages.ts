import { pageAnchorId } from './pageAnchor';

/** Страница читалки в том виде, в каком её склейка принимает и отдаёт. */
export interface StitchablePage {
  pageNumber: number;
  html: string;
}

/** Страница после склейки. */
export interface StitchedPage extends StitchablePage {
  /**
   * Начало полосы уехало в абзац предыдущей: маркер номера и якорь страницы
   * теперь на шве, и рисовать свои секция не должна — иначе в документе
   * окажутся два элемента с одним id.
   */
  seamed: boolean;
}

/** Как оформить шов: адрес и подпись маркера номера страницы. */
export interface SeamMarker {
  href: (pageNumber: number) => string;
  label: (pageNumber: number) => string;
}

/**
 * Конец законченной фразы: точка (или её родня) и, возможно, закрывающая
 * кавычка либо скобка следом.
 */
const SENTENCE_END = /[.!?…]\s*["»”)\]]?\s*$/;

/** Строчная буква — русская или латинская, за необязательной открывающей парой. */
const CONTINUES = /^["«„([]?\s*[а-яёa-z]/;

/**
 * Стоит ли считать стык полос продолжением одного абзаца.
 *
 * Печатный абзацный отступ в OCR не сохраняется, поэтому «точка на конце и
 * прописная в начале» неразличима: это может быть и новый абзац, и новое
 * предложение того же. Такие стыки остаются как есть — зазор после законченной
 * фразы читается как обычный абзац, тогда как склеенные в один абзац две
 * разные мысли уже не разделить обратно. Замер по тому 62: из 665 стыков
 * уверенных 403, неоднозначных 116.
 */
function continuesParagraph(tail: string, head: string): boolean {
  if (!tail || !head) return false;
  if (!SENTENCE_END.test(tail)) return true;
  return CONTINUES.test(head);
}

/** Частицы, которые пишутся через дефис: «кто-то», «где-либо», «пойдем-ка». */
const HYPHEN_PARTICLES = new Set(['то', 'либо', 'нибудь', 'ка', 'таки']);

/** Слова текста — по ним отличают перенос от печатного дефиса. */
const WORD = /[\p{L}\p{M}]+/gu;

/**
 * Словарь загруженного текста без обрывков, оставшихся от переносов.
 *
 * Половинки разорванного слова выкидываются: иначе «предста» с конца полосы
 * само же и докажет, что оно самостоятельное слово, и перенос никогда не
 * срастётся.
 */
function vocabulary(texts: string[]): Set<string> {
  const words = new Set<string>();
  texts.forEach((text, i) => {
    const tokens = text.match(WORD) ?? [];
    const first = i > 0 && texts[i - 1].trimEnd().endsWith('-') ? 1 : 0;
    const last = text.trimEnd().endsWith('-') ? tokens.length - 1 : tokens.length;
    for (let k = first; k < last; k++) words.add(tokens[k].toLowerCase());
  });
  return words;
}

/**
 * Дефис на конце полосы — перенос или печатный дефис составного слова?
 *
 * Отличить их по одной паре обрывков нельзя, поэтому судим по всему
 * загруженному тексту.
 *
 * Решает прежде всего сросшаяся форма: если «марксистских» в томе есть, то
 * «марксист-ских» — перенос, сколько бы раз «марксист» и «ских» ни попадались
 * порознь. Замер по тому 62 (39 переносов через границу полос): сросшаяся
 * форма нашлась у 32, и на одних только половинах правило ошибалось дважды —
 * «марксист-ских» и «Париж-ской».
 *
 * Оставшиеся случаи: справа прописная или дефисная частица («кто-то»,
 * «где-либо») — дефис печатный; обе половины длинные и каждая ходит по тексту
 * сама по себе — тоже печатный («оппортунистами-меньшевиками», единственный
 * такой в томе). Всё прочее — перенос: их подавляющее большинство.
 */
function isPrintedHyphen(left: string, right: string, words: Set<string>): boolean {
  const leftWord = (left.match(WORD) ?? []).pop() ?? '';
  const rightWord = (right.match(WORD) ?? []).shift() ?? '';
  if (!leftWord || !rightWord) return false;
  if (words.has((leftWord + rightWord).toLowerCase())) return false;
  if (/^\p{Lu}/u.test(rightWord)) return true;
  if (HYPHEN_PARTICLES.has(rightWord.toLowerCase())) return true;
  if (leftWord.length < 4 || rightWord.length < 4) return false;
  return words.has(leftWord.toLowerCase()) && words.has(rightWord.toLowerCase());
}

/**
 * Убирает знак переноса с конца абзаца.
 *
 * Ищется последний непустой текстовый узел, а не последний ребёнок: перенос
 * нередко приходится на слово внутри курсива или ссылки, и тогда дефис лежит
 * не в самом абзаце, а глубже.
 */
function dropTrailingHyphen(paragraph: Element): void {
  const walker = document.createTreeWalker(paragraph, NodeFilter.SHOW_TEXT);
  let last: Text | null = null;
  let node = walker.nextNode();
  while (node) {
    if ((node.nodeValue ?? '').trim() !== '') last = node as Text;
    node = walker.nextNode();
  }
  if (!last) return;
  last.nodeValue = (last.nodeValue ?? '').trimEnd().slice(0, -1);
}

/**
 * Служебная часть документа, а не текст страницы.
 *
 * Сервер обёртку документа больше не шлёт: `pkg/markdown` отдаёт XHTML-фрагмент
 * (см. docs/superpowers/specs/2026-09-04-markdown-fragment-contract-design.md).
 * Фильтр оставлен сторожем, а не как рабочий механизм: раньше `<title>` и
 * `<meta>` переживали вставку через innerHTML и становились детьми контейнера,
 * и склейка брала первым элементом текста их, а не абзац. Один ответ от
 * бэкенда, отставшего от master, вернул бы ровно это — и читалка сломалась бы
 * на ровном месте, хотя ни одна строка фронта не менялась.
 */
const DOCUMENT_CHROME = new Set(['TITLE', 'META', 'LINK', 'BASE', 'STYLE', 'SCRIPT']);

function parse(html: string): HTMLElement {
  const host = document.createElement('div');
  host.innerHTML = html;
  return host;
}

/** Первый и последний элементы текста страницы — мимо служебной обёртки. */
function contentEdges(host: HTMLElement): { first: Element | null; last: Element | null } {
  const content = Array.from(host.children).filter((el) => !DOCUMENT_CHROME.has(el.tagName));
  return { first: content[0] ?? null, last: content[content.length - 1] ?? null };
}

/**
 * Шов: обёртка над текстом, переехавшим в абзац предыдущей полосы.
 *
 * Обёртка, а не пустая метка, потому что якорь страницы уезжает сюда вместе с
 * текстом, а `useVisiblePage` определяет текущую страницу по тому, попадает ли
 * прямоугольник секции в полосу чтения. Строчный span даёт объединение своих
 * строк — ровно ту область склеенного абзаца, которая принадлежит новой
 * полосе.
 */
function makeSeam(pageNumber: number, marker: SeamMarker): HTMLElement {
  const seam = document.createElement('span');
  seam.className = 'chapter-page-section page-seam';
  seam.id = pageAnchorId(pageNumber);
  seam.setAttribute('data-page', String(pageNumber));
  // Прыжок по оглавлению переводит на секцию фокус (ChapterView), а строчный
  // span его без tabindex не примет.
  seam.setAttribute('tabindex', '-1');

  const link = document.createElement('a');
  link.className = 'page-marker';
  link.setAttribute('href', marker.href(pageNumber));
  link.setAttribute('aria-label', marker.label(pageNumber));
  link.textContent = String(pageNumber);
  seam.append(link);

  return seam;
}

/**
 * Знаки, которые в русском наборе прилипают к предыдущему слову: пробела перед
 * ними нет.
 *
 * Список понадобился после прогонов склейки переносов через границу полосы
 * (`fix_page_break_hyphen`): разорванные слова срослись прямо в базе, буквы
 * хвоста уехали с полосы N+1 на полосу N. Раньше такой стык шёл по ветке
 * переноса — дефис снимался, пробел не ставился; теперь полоса дефисом не
 * кончается, а следующая начинается прямо со знака, и `tail.append(' ')`
 * вставлял перед ним пробел.
 *
 * Две величины, и путать их нельзя. **447** — накопленный счёт по ОБОИМ
 * журналам (`runs/all` и `runs/142`, все запуски вместе): именно столько
 * склеек лежит в базе, и именно этот счёт важен читалке. **294** — один
 * прогон 02.09.2026 (276 по корпусу + 18 на работе 142); остальные 153 —
 * от 28.08.2026. Журнал накопительный по замыслу (`pending_records` добивает
 * пары, оборванные любым прошлым запуском), поэтому счёт журнала больше счёта
 * отдельного прогона, и это не расхождение.
 *
 * Замер по журналам (пересчитано 02.09.2026): из 447 склеек следующая полоса
 * начинается НЕ с буквы в 160 случаях. Из них прилипающих к слову — 145:
 * «,» 54, «»» 52, «.» 20, «:» 5, «;» 4, дефис 4, «?» 3, «)» 3. Оставшиеся
 * 15 в список не входят, и почему — абзацем ниже.
 *
 * Дефис входит сюда по живым данным, а не по общему правилу: во всех четырёх
 * случаях корпуса он держит составное слово — «какие-нибудь» (работа 142,
 * стр. 273), «какие-то» (работа 142, стр. 597), «Либерально-монархическая»
 * (работа 78, стр. 168), «общественно-политической» (работа 140, стр. 641).
 *
 * Чего в списке нет и почему (те самые 15): тире «—» (6 случаев) отделяется
 * пробелом с обеих сторон — «в лице Вандерлипа — сторонника…» (работа 112,
 * стр. 98); открывающие кавычка ««» (2) и скобка «(» (3) тоже требуют
 * пробела слева — «публицистов II Интернационала (1889—1914)» (работа 96,
 * стр. 102); цифра (2) — это просто следующее слово («всего 34 475», работа
 * 142, стр. 688). Двух оставшихся знаков правило тоже не касается: звёздочка
 * (работа 128, стр. 171) — разметка курсива,
 * до `textContent` доходит уже буква; обратная косая (работа 142, стр. 199) —
 * обломок формулы, отдельное слово: «а славянское \rho вовсе не одинокое».
 */
const STICKS_TO_WORD = /^[-,.;:!?…»”›)\]]/;

export function stitchPages(pages: StitchablePage[], marker: SeamMarker): StitchedPage[] {
  const hosts = pages.map((p) => parse(p.html));
  const words = vocabulary(hosts.map((h) => h.textContent ?? ''));
  const seamed = pages.map(() => false);

  // Абзац, в который уехала предыдущая полоса целиком. Текст в нём не
  // прерывается, поэтому следующая полоса продолжает его же — иначе каждая
  // страница из одного абзаца-продолжения (48 в томе 62) обрывала бы цепочку.
  // Пустая полоса — другое дело: там между текстом лежит непечатный лист, и
  // склейка через него запрещена, поэтому носитель хвоста заводится только
  // после состоявшейся склейки.
  let carried: Element | null = null;

  for (let i = 1; i < hosts.length; i++) {
    const tail: Element | null = carried ?? contentEdges(hosts[i - 1]).last;
    const head = contentEdges(hosts[i]).first;
    carried = null;
    if (!tail || !head || tail.tagName !== 'P' || head.tagName !== 'P') continue;

    const tailText = (tail.textContent ?? '').trimEnd();
    const headText = (head.textContent ?? '').trimStart();
    const hyphenated = tailText.endsWith('-');
    if (!hyphenated && !continuesParagraph(tailText, headText)) continue;

    const glued = hyphenated && !isPrintedHyphen(tailText, headText, words);
    if (glued) dropTrailingHyphen(tail);

    const seam = makeSeam(pages[i].pageNumber, marker);
    seam.append(...Array.from(head.childNodes));
    if (!hyphenated && !STICKS_TO_WORD.test(headText)) tail.append(' ');
    tail.append(seam);
    head.remove();
    seamed[i] = true;
    if (contentEdges(hosts[i]).first === null) carried = tail;
  }

  return pages.map((page, i) => ({ ...page, html: hosts[i].innerHTML, seamed: seamed[i] }));
}
