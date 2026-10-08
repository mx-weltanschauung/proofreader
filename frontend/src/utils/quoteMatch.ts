/**
 * Точное совпадение цитаты в отрисованном тексте полосы.
 *
 * Не двойник searchHighlight.ts, и расхождение принципиальное: тот ищет
 * ЛЕММЫ по началу слова и живёт в пределах одного текстового узла, этот —
 * ТОЧНУЮ подстроку через границы узлов. У поиска нормализация приводит
 * регистр и ё→е; у цитаты нельзя ни того, ни другого — ключ точный.
 */

/**
 * Пробел любого вида — источник истины один, регэксп `\s`: в JS он уже
 * покрывает и обычный, и неразрывный (U+00A0), и все узкие/широкие пробелы
 * Unicode (U+2000–U+200A, U+202F, U+205F, U+3000), и переносы строк, и
 * управляющие \v\f, и BOM (U+FEFF). Раньше `isSpace` держал свой список из
 * пяти знаков ОТДЕЛЬНО от регэкспа в `normalizeQuote` — расхождение молчало,
 * пока в тексте не попадался, например, узкий U+202F (тот же знак уже ловил
 * `groupThousands.ts`): `normalizeQuote` схлопывал его в ключе цитаты, а
 * `flattenText` — нет, и совпадение через такой пробел не находилось никем
 * не замеченным промахом.
 */
const SPACE_RE = /\s/;

function isSpace(ch: string): boolean {
  return SPACE_RE.test(ch);
}

/**
 * Одна нормализация на обе стороны сравнения: пробельный ряд в один пробел,
 * края обрезаны. Больше ничего — иначе ключ перестанет быть точным.
 */
export function normalizeQuote(s: string): string {
  return s.replace(new RegExp(SPACE_RE.source + '+', 'g'), ' ').trim();
}

export interface FlatText {
  /** Нормализованный текст. */
  text: string;
  /** Узел каждого знака текста. */
  nodes: Text[];
  /** Смещение знака внутри своего узла. */
  offsets: number[];
  /** Номер полосы каждого знака — по ближайшему предку с data-page. */
  pages: (number | null)[];
}

/** Номер полосы узла: ближайший предок с data-page. */
export function pageOfNode(node: Node): number | null {
  const el = node.nodeType === Node.ELEMENT_NODE ? (node as HTMLElement) : node.parentElement;
  const holder = el?.closest<HTMLElement>('[data-page]');
  if (!holder) return null;
  const n = Number.parseInt(holder.dataset.page ?? '', 10);
  return Number.isInteger(n) ? n : null;
}

/**
 * Плоский текст поддерева и карта обратно в DOM.
 *
 * .katex пропускается той же причиной, что и в поиске: там и скрытый
 * исходник TeX, и разложенная по спанам вёрстка, где каждый знак — свой
 * узел. .page-marker — печатная колонцифра шва (stitchPages.ts, makeSeam):
 * она лежит текстовым узлом ВНУТРИ содержимого предыдущей страницы
 * (tail.append(seam)), скрыта в чтении CSS-правилом display:none и живому
 * читателю в Selection.toString() никогда не попадает — а наивный обход
 * дерева её увидит, если явно не отсеять, и цитата через шов получит
 * лишнюю цифру внутри текста.
 */
export function flattenText(root: HTMLElement): FlatText {
  const walker = document.createTreeWalker(root, NodeFilter.SHOW_TEXT, {
    acceptNode: (node) => {
      const parent = node.parentElement;
      if (!parent || parent.closest('script, style, .katex, .page-marker'))
        return NodeFilter.FILTER_REJECT;
      return NodeFilter.FILTER_ACCEPT;
    },
  });

  const chars: string[] = [];
  const nodes: Text[] = [];
  const offsets: number[] = [];
  const pages: (number | null)[] = [];
  let lastSpace = true; // ведущие пробелы съедаются

  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const node = n as Text;
    const page = pageOfNode(node);
    const data = node.data;
    for (let i = 0; i < data.length; i++) {
      const ch = data[i];
      if (isSpace(ch)) {
        if (lastSpace) continue;
        lastSpace = true;
        chars.push(' ');
      } else {
        lastSpace = false;
        chars.push(ch);
      }
      nodes.push(node);
      offsets.push(i);
      pages.push(page);
    }
  }

  while (chars.length > 0 && chars[chars.length - 1] === ' ') {
    chars.pop();
    nodes.pop();
    offsets.pop();
    pages.pop();
  }

  return { text: chars.join(''), nodes, offsets, pages };
}

export type QuoteMatch = 'hit' | 'multiple' | 'partial' | 'miss';

/**
 * Адрес места в тексте — ПАРА якорей, а не одна строка.
 *
 * `start` (`?quote=`) метит, где цитата начинается, и он же ключ адреса:
 * кратчайший префикс, уникальный на полосе (quoteKey.ts). `end` (`&to=`) метит
 * где кончается, и без него подсветка равна самому ключу — то есть двум-трём
 * словам вместо процитированного предложения.
 *
 * `end` необязателен, и это не задел «на будущее»: без него приходят все
 * ссылки, розданные до этой работы, и внешние адреса вырезок предметного
 * указателя (ConceptFragmentBlock), у которых якоря конца нет вовсе —
 * `head_quote` хранит только голову.
 */
export interface QuoteSpan {
  start: string;
  end?: string;
}

/**
 * Позиции вхождений `q` в `text`, у которых знаки идут подряд и в исходном
 * плоском тексте — не только в `text`.
 *
 * Нужно только суженному по полосе поиску: `text` там — склейка кусков одной
 * полосы (голова в шве плюс, может быть, хвост дальше в документе), и между
 * кусками в исходном тексте лежит чужой материал — например, сноски
 * ПРЕДЫДУЩЕЙ полосы в потоке чтения (ReadingChunk.tsx рисует блок сносок
 * полосы между её текстом и следующей полосой). Совпадение, целиком лежащее
 * в одном куске, само по себе идёт подряд (indices[j+1] === indices[j] + 1
 * выполняется тождественно) и здесь ничего не теряет; отвергается только то,
 * что перепрыгнуло разрыв, — такую строку читатель никогда не видел единым
 * куском, и подсветка «совпадения» была бы честной подсказкой не туда, хуже
 * честного промаха. Без номера полосы (общий поиск по всему документу)
 * `indices` — тождественная последовательность 0..n-1, проверка проходит
 * всегда и поведение не меняется.
 */
function contiguousOccurrences(text: string, indices: number[], q: string): number[] {
  const hits: number[] = [];
  let from = 0;
  for (;;) {
    const at = text.indexOf(q, from);
    if (at < 0) break;
    let ok = true;
    for (let j = at; j < at + q.length - 1; j++) {
      if (indices[j + 1] !== indices[j] + 1) {
        ok = false;
        break;
      }
    }
    if (ok) hits.push(at);
    from = at + 1;
  }
  return hits;
}

/**
 * Оборачивает в <mark class="quote-hit"> ПРОЛЁТ от якоря начала до якоря
 * конца — или один якорь начала, если конца в адресе нет.
 *
 * По куску на каждый задетый текстовый узел: Range.surroundContents на
 * пересечении границ элементов бросает исключение, а цитата через курсив
 * такое пересечение и есть. Узлы обходятся с конца — оборачивание разрезает
 * узел, и ссылки на предыдущие от этого не портятся.
 *
 * Начало ищется в тексте, суженном полосой из адреса (ниже), а конец — вперёд
 * от начала по ПОЛНОМУ плоскому тексту, и сужение на него не переносится
 * намеренно: цитата через стык кончается на следующей полосе, а в главе
 * полосы склеены в один поток и читатель выделял их подряд. Границы пролёта
 * поэтому считаются в плоских координатах, где соседние знаки соседние по
 * построению, — проверять непрерывность внутри пролёта нечего.
 *
 * Конец, названный адресом, но не найденный, — это 'partial': начало
 * подсвечивается, и вызывающая сторона обязана сказать об этом вслух.
 * Молчание здесь неотличимо для читателя от кривой ссылки, а причина как
 * правило одна — полосу правили после того, как на неё сослались.
 *
 * Исход 'partial' перебивает 'multiple': сказать можно что-то одно, а
 * подсветка короче цитаты заметнее выбора вхождения.
 *
 * `pageNumber` — необязательное сужение области поиска до одной полосы:
 * head цитаты в вырезке предметного указателя мог повториться на другой
 * полосе того же тома, и без сужения такое совпадение честно вернуло бы
 * 'multiple', хотя адрес, с которого пришла ссылка, известен заранее.
 *
 * Ветка `pageNumber === undefined` (полнотекстовый поиск без сужения) в
 * production-коде недостижима: единственный вызывающий, useQuoteHighlight,
 * сам отсекает этот случай раньше — он не зовёт markQuote вовсе, если
 * номер полосы не определён. Параметр остался необязательным ради тестов
 * markQuote самого по себе (quoteMatch.test.ts), которым удобно проверять
 * склейку текста без обвязки страниц; убирать `?` означало бы переписывать
 * их все ради параметра, который они сознательно не используют.
 */
export function markQuote(root: HTMLElement, span: QuoteSpan, pageNumber?: number): QuoteMatch {
  const q = normalizeQuote(span.start);
  if (q === '') return 'miss';

  const flat = flattenText(root);

  // Без номера полосы ищем по всему плоскому тексту; с номером — строим
  // текст-подмножество только из знаков нужной полосы и карту indices для
  // обратного пересчёта позиции в исходные nodes/offsets. Уникальность ключа
  // по-прежнему считается по СКЛЕЕННОМУ тексту полосы — это безопасно и
  // консервативно: строка, встретившаяся в склейке один раз, не может
  // встретиться дважды порознь.
  let text: string;
  let indices: number[];
  if (pageNumber === undefined) {
    text = flat.text;
    indices = flat.text.split('').map((_, i) => i);
  } else {
    indices = [];
    let restricted = '';
    for (let i = 0; i < flat.text.length; i++) {
      if (flat.pages[i] === pageNumber) {
        indices.push(i);
        restricted += flat.text[i];
      }
    }
    text = restricted;
  }

  // Совпадение годится, только если его знаки идут подряд и в исходном
  // тексте, а не только в склейке text — иначе оно перепрыгивает чужой
  // материал между кусками одной полосы (см. contiguousOccurrences).
  const hits = contiguousOccurrences(text, indices, q);
  if (hits.length === 0) return 'miss';
  const first = hits[0];
  const more = hits.length > 1;

  // Дальше — плоские координаты: вхождение начала непрерывно (проверено
  // выше), поэтому его знаки в плоском тексте лежат подряд от indices[first].
  const from = indices[first];
  let to = from + q.length;

  const end = normalizeQuote(span.end ?? '');
  let endMissing = false;
  if (end !== '') {
    // Вперёд ОТ НАЧАЛА пролёта, а не от его конца: якорь конца короткой
    // цитаты законно перекрывается с ключом начала («Если до» / «до 6 лет»),
    // и поиск от конца начала такой якорь бы потерял.
    const at = flat.text.indexOf(end, from);
    if (at < 0) endMissing = true;
    else to = Math.max(to, at + end.length);
  }

  const pieces: { node: Text; lo: number; hi: number }[] = [];
  for (let i = from; i < to; i++) {
    const last = pieces[pieces.length - 1];
    if (last && last.node === flat.nodes[i]) last.hi = flat.offsets[i] + 1;
    else pieces.push({ node: flat.nodes[i], lo: flat.offsets[i], hi: flat.offsets[i] + 1 });
  }

  for (let i = pieces.length - 1; i >= 0; i--) {
    const { node, lo, hi } = pieces[i];
    const range = document.createRange();
    range.setStart(node, lo);
    range.setEnd(node, hi);
    const mark = document.createElement('mark');
    mark.className = 'quote-hit';
    range.surroundContents(mark);
  }

  if (endMissing) return 'partial';
  return more ? 'multiple' : 'hit';
}
