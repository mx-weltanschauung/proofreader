import { defaultSchema } from 'rehype-sanitize';
import type { Options as Schema } from 'rehype-sanitize';

// The moderation-queue preview renders reader-submitted markdown, which may
// contain raw HTML (the `rehype-raw` step that `@uiw/react-markdown-preview`
// always runs turns that into live DOM nodes — see MarkdownEditor's
// `sanitizeUntrustedContent` prop). `rehype-sanitize`'s GitHub-style
// `defaultSchema` blocks that, but it also knows nothing about the MathML +
// SVG markup `rehype-katex` renders formulas into, so applying it naively
// strips every formula down to nothing.
//
// This schema starts from `defaultSchema` and adds exactly the tags/
// attributes KaTeX is observed to emit, verified by rendering superscripts,
// fractions, matrices, sqrt/sum, and colored text through the real
// remark-math + rehype-katex pipeline and inspecting the resulting hast
// tree (see task-11-report.md, "Fix round 2"). The MathML tag list is
// widened slightly beyond what was observed (the full MathML3 presentation
// set) since other formulas KaTeX supports (e.g. `\underbrace`, `\mmlToken`)
// can emit siblings that weren't exercised by the sample set — these tags
// carry no scripting surface, so allow-listing the full set costs nothing.
//
// Круг правок 4: замер 76 формул через настоящий конвейер (скрипт
// `scripts/measure-katex-styles.mjs`) показал, что схема круга 2 всё же
// теряла часть вывода — тег `line` целиком (обводка \cancel и \not),
// `style` у `svg` (ширина радикала и стрелок) и десяток атрибутов MathML
// (`mathvariant`, `notation`, `accent`, `stretchy`, `linethickness` и др.),
// то есть скрытое дерево для экранных читалок и копирования формулы. Всё
// перечисленное добавлено ниже.
//
// ЗНАЧЕНИЕ `style` эта схема НЕ проверяет и проверить не может:
// hast-util-sanitize умеет сверить значение атрибута с точной строкой или
// регуляркой (причём через неякорный `.test()`), а `style` — свободный CSS
// из нескольких деклараций, где нужно выбросить часть, а не всё. Разделение
// ролей поэтому такое: схема решает, КАКИЕ теги вообще вправе нести
// `style`, а белый список значений держит `rehypeAllowlistStyles`, который
// ставится следом за санитайзером.
/**
 * Классы, которые KaTeX ставит на `span`, — белый список для схемы
 * санитайзера (`markdownSanitizeSchema.ts`).
 *
 * Зачем список вообще. Схема разрешала на `span` голый `className`, то
 * есть любое значение: hast-util-sanitize проверяет имя атрибута, а не
 * значение. Замер на живом конвейере: `<span class="note-sheet-scrim">` из
 * markdown читателя доезжал до вывода, а `hooks/noteSheet.css` описывает
 * этот класс как `position: fixed; inset: 0; z-index: 1000` — правило
 * глобальное и без префикса. То есть читатель мог накрыть экран модерации
 * полотном во весь экран, внутри origin читальни и без единой строки
 * скрипта: подмена интерфейса против того, кто разбирает его же правку.
 *
 * Список СНЯТ ЗАМЕРОМ (`npm run measure:katex-styles`), а не собран по
 * памяти, и объединяет два источника:
 *
 *  1. что KaTeX реально выдаёт на наборе формул `src/test/katexSampleFormulas.ts`
 *     (87 разных классов, все — на `span`, других тегов с `className` в
 *     выводе нет);
 *  2. что стилизует сам `katex.min.css` (140 классов) — там перечислены
 *     семейства, до которых набор формул не добрался: `size5`…`size11`,
 *     `reset-size1`…`reset-size11`, `delim-size1/4`, `cd-*` у
 *     коммутативных диаграмм, `sout`, `angl`, `tag`, `eqn-num`.
 *
 * Первый источник говорит, что KaTeX ставит на практике; второй — каков его
 * словарь вообще. Только первого мало: формула вне набора теряла бы класс и
 * ехала бы молча.
 *
 * Проверено, что список не открывает дверь обратно: из 150 имён с классами
 * CSS читальни (600 имён в 67 файлах) пересекается ровно одно — `overlay`,
 * и то лишь в составном селекторе `.chapter-tree-node.overlay`, который на
 * голом `class="overlay"` не срабатывает.
 *
 * Пересобрать после обновления katex/rehype-katex: `npm run measure:katex-styles`,
 * раздел «объединение обоих замеров». Сторож — `markdownSanitizeSchema.test.ts`:
 * ни один класс, который KaTeX выдаёт на наборе формул, не должен теряться.
 *
 * Список лежит в этом же файле, а не рядом отдельным модулем, нарочно:
 * `scripts/measure-katex-styles.mjs` импортирует схему прямо через
 * `node --experimental-strip-types`, а тот требует расширения в путях —
 * относительный `./katexClassNames` ломает замер, которым список и снят.
 */
export const KATEX_CLASS_NAMES: readonly string[] = [
  'accent',
  'accent-body',
  'accent-full',
  'accentunder',
  'amsrm',
  'angl',
  'anglpad',
  'arraycolsep',
  'base',
  'boldsymbol',
  'boxpad',
  'brace-center',
  'brace-left',
  'brace-right',
  'cancel-lap',
  'cancel-pad',
  'cd-arrow-pad',
  'cd-label-left',
  'cd-label-right',
  'cd-vert-arrow',
  'clap',
  'col-align-c',
  'col-align-l',
  'col-align-r',
  'cyrillic_fallback',
  'delim-size1',
  'delim-size4',
  'delimcenter',
  'delimsizing',
  'eqn-num',
  'fbox',
  'fcolorbox',
  'fix',
  'fleqn',
  'fontsize-ensurer',
  'frac-line',
  'halfarrow-left',
  'halfarrow-right',
  'hbox',
  'hdashline',
  'hide-tail',
  'hline',
  'inner',
  'katex',
  'katex-display',
  'katex-error',
  'katex-html',
  'katex-mathml',
  'katex-version',
  'large-op',
  'leqno',
  'llap',
  'mainrm',
  'mathbb',
  'mathbf',
  'mathboldfrak',
  'mathboldsf',
  'mathcal',
  'mathfrak',
  'mathit',
  'mathitsf',
  'mathnormal',
  'mathrm',
  'mathscr',
  'mathsf',
  'mathsfit',
  'mathtt',
  'mbin',
  'mclose',
  'mfrac',
  'minner',
  'mml-eqn-num',
  'mop',
  'mopen',
  'mord',
  'mover',
  'mpunct',
  'mrel',
  'mspace',
  'msupsub',
  'mtable',
  'mtight',
  'mtr-glue',
  'mult',
  'munder',
  'newline',
  'nulldelimiter',
  'op-limits',
  'op-symbol',
  'overlay',
  'overline',
  'overline-line',
  'pstrut',
  'reset-size1',
  'reset-size10',
  'reset-size11',
  'reset-size2',
  'reset-size3',
  'reset-size4',
  'reset-size5',
  'reset-size6',
  'reset-size7',
  'reset-size8',
  'reset-size9',
  'rlap',
  'root',
  'rule',
  'size1',
  'size10',
  'size11',
  'size2',
  'size3',
  'size4',
  'size5',
  'size6',
  'size7',
  'size8',
  'size9',
  'sizing',
  'small-op',
  'sout',
  'sqrt',
  'stretchy',
  'strut',
  'svg-align',
  'tag',
  'text',
  'textbb',
  'textbf',
  'textboldfrak',
  'textboldsf',
  'textfrak',
  'textit',
  'textitsf',
  'textrm',
  'textscr',
  'textsf',
  'texttt',
  'thinbox',
  'underline',
  'underline-line',
  'vbox',
  'vertical-separator',
  'vlist',
  'vlist-r',
  'vlist-s',
  'vlist-t',
  'vlist-t2',
  'x-arrow',
  'x-arrow-pad',
];

const mathMlTagNames = [
  'math',
  'semantics',
  'annotation',
  'annotation-xml',
  'mrow',
  'mi',
  'mn',
  'mo',
  'ms',
  'mtext',
  'mspace',
  'msqrt',
  'mroot',
  'mfrac',
  'msub',
  'msup',
  'msubsup',
  'munder',
  'mover',
  'munderover',
  'mmultiscripts',
  'mtable',
  'mtr',
  'mtd',
  'mlabeledtr',
  'maction',
  'mstyle',
  'mpadded',
  'mphantom',
  'mglyph',
  'menclose',
  'mprescripts',
  'none',
];

// ---------------------------------------------------------------------------
// Круг правок 5: автоматические сетевые обращения как класс
// ---------------------------------------------------------------------------
//
// Круги 3 и 4 закрывали один канал деанонимизации модератора — `url()` в
// `style`. Рядом всё это время стоял открытым канал попроще: markdown
// `![](https://чужой/x.png)` даёт `<img src>`, браузер модератора идёт на
// чужой хост сам, и сырой HTML для этого даже не нужен. Замер по рабочей
// базе: **0 картинок на 45 495 полос корпуса** — ни markdown-синтаксиса, ни
// тегов. Законных случаев нет ни одного, поэтому запрет ничего не стоит.
//
// Чинить один `img` было бы той же ошибкой, что чинить один `url()`: через
// круг нашёлся бы следующий экземпляр. Поэтому ниже объявлен КЛАСС — теги и
// атрибуты, которые лезут в сеть САМИ, при отрисовке, без действия
// человека, — и схема собирается вычитанием этого класса из эффективного
// набора. Список выведен обходом эффективного набора схемы (все теги
// `defaultSchema` + наши MathML/SVG), а не по памяти; что в нём нашлось —
// в task-11-report.md, «Fix round 5».
//
// Граница проведена по признаку «само или по клику»:
//   * `<a href>` НЕ трогаем. Ссылка ведёт в сеть по клику, адрес виден в
//     строке состояния, переход — осознанное действие редактора, а не
//     утечка при открытии карточки.
//   * `cite` у blockquote/del/ins/q оставлен: это ссылка-метаданные,
//     браузеры её не запрашивают ни при отрисовке, ни по клику.
//   * `useMap`/`action` из общего списка `*` оставлены: `map`, `area` и
//     `form` в схеме отсутствуют, ссылаться этим атрибутам не на что.

/**
 * Теги, само существование которых в дереве означает сетевой запрос при
 * отрисовке. Убираются из `tagNames` целиком.
 *
 * `img`, `picture`, `source` реально присутствовали в `defaultSchema`;
 * `mglyph` — в нашем же списке MathML (у него есть `src`, и он грузит
 * картинку; KaTeX его не выдаёт — проверено замером). Остальные в
 * `defaultSchema` не входят и перечислены на вырост: если очередная версия
 * `hast-util-sanitize` добавит `video` или `iframe`, вычитание сработает
 * само, а сторожевой тест всё равно потребует пересмотра списка.
 *
 * Меньшая часть набора запрашивает не ресурс, а управление: `base` и `meta`
 * (перенаправление), `foreignObject` (лазейка обратно в HTML внутри SVG).
 * Им в предпросмотре чужого текста тоже нечего делать, и держать их в одном
 * списке дешевле, чем заводить второй почти такой же.
 */
const NETWORK_FETCHING_TAG_NAMES = new Set([
  'img',
  'picture',
  'source',
  'video',
  'audio',
  'track',
  'iframe',
  'frame',
  'embed',
  'object',
  'applet',
  'link',
  'script',
  'style',
  'base',
  'meta',
  'portal',
  'mglyph', // MathML: <mglyph src=...> грузит картинку
  'image', // SVG: <image href=...>
  'use', // SVG: <use href=...> тянет внешний документ
  'feImage',
  'filter',
  'pattern',
  'textPath',
  'foreignObject',
]);

/**
 * Атрибуты, которые заставляют браузер сходить в сеть при отрисовке. Имена
 * сверяются регистронезависимо: hast пишет их в camelCase (`srcSet`,
 * `xlinkHref`, `codeBase`), а разметка читателя — как угодно.
 *
 * `href` в этот список НЕ входит: он вычитается точечно у всех тегов, кроме
 * `a` (см. ALLOWED_HREF_TAG_NAMES) — у `a` он безопасен и нужен, у
 * какого-нибудь `use` или `link` был бы загрузкой.
 */
const NETWORK_FETCHING_ATTRIBUTE_NAMES = new Set(
  [
    'src',
    'srcset',
    'poster',
    'data',
    'background',
    'lowsrc',
    'dynsrc',
    'longdesc',
    'manifest',
    'icon',
    'archive',
    'codebase',
    'classid',
    'profile',
    'ping',
    'formaction',
    'imagesrcset',
    'imagesizes',
    'xlink:href',
    'xlinkhref',
    'xlinkarcrole',
    'xlinkrole',
  ].map((name) => name.toLowerCase()),
);

/** Единственный тег, которому `href` оставлен: ссылка по клику. */
const ALLOWED_HREF_TAG_NAMES = new Set(['a']);

type AttributeDefinition = NonNullable<Schema['attributes']>[string][number];

function attributeName(definition: AttributeDefinition): string {
  return Array.isArray(definition) ? String(definition[0]) : String(definition);
}

function isNetworkFetchingAttribute(tagName: string, definition: AttributeDefinition): boolean {
  const name = attributeName(definition).toLowerCase();
  if (name === 'href') return !ALLOWED_HREF_TAG_NAMES.has(tagName);
  return NETWORK_FETCHING_ATTRIBUTE_NAMES.has(name);
}

function withoutNetworkAttributes(
  attributes: NonNullable<Schema['attributes']>,
): NonNullable<Schema['attributes']> {
  const cleaned: NonNullable<Schema['attributes']> = {};
  for (const [tagName, definitions] of Object.entries(attributes)) {
    if (NETWORK_FETCHING_TAG_NAMES.has(tagName)) continue;
    cleaned[tagName] = (definitions ?? []).filter(
      (definition) => !isNetworkFetchingAttribute(tagName, definition),
    );
  }
  return cleaned;
}

const tagNamesWithKatex = [
  ...(defaultSchema.tagNames ?? []),
  ...mathMlTagNames,
  'svg',
  'path',
  'line',
];

const attributesWithKatex: NonNullable<Schema['attributes']> = {
  ...defaultSchema.attributes,
  // `className` перечислен ЗНАЧЕНИЯМИ, а не голым именем. Голое имя
  // пропускало любой класс, и это была не теория: `<span
  // class="note-sheet-scrim">` из markdown читателя доезжал до вывода, а
  // правило этого класса в `hooks/noteSheet.css` — глобальное
  // `position: fixed; inset: 0; z-index: 1000`, то есть полотно во весь
  // экран поверх интерфейса модерации, внутри origin читальни и без
  // скрипта. hast-util-sanitize сверяет классы поштучно и выбрасывает
  // непрошедшие, а не атрибут целиком, поэтому вёрстка формул цела.
  //
  // Список — в KATEX_CLASS_NAMES выше в этом же файле (см. doc-блок над ним,
  // почему не отдельным модулем), снят замером, не по памяти.
  //
  // Значение `style` так проверить нельзя: там несколько деклараций, из
  // которых надо выбросить часть, а не всё, — этим занимается
  // rehypeAllowlistStyles следом за санитайзером.
  span: [
    ...(defaultSchema.attributes?.span ?? []),
    ['className', ...KATEX_CLASS_NAMES],
    'style',
    'ariaHidden',
  ],
  math: ['xmlns'],
  annotation: ['encoding'],
  mi: ['mathvariant'],
  mo: ['fence', 'stretchy', 'maxsize', 'minsize', 'lspace', 'rspace', 'separator', 'mathvariant'],
  mtext: ['mathvariant'],
  mfrac: ['linethickness'],
  mpadded: ['width', 'height', 'depth', 'lspace', 'voffset'],
  mspace: ['width', 'height', 'mathbackground'],
  mover: ['accent'],
  munder: ['accentunder'],
  munderover: ['accent', 'accentunder'],
  menclose: ['notation'],
  mstyle: ['displaystyle', 'mathcolor', 'scriptlevel', 'style'],
  mtable: ['columnalign', 'columnspacing', 'rowspacing', 'columnlines', 'rowlines'],
  // `style` у svg несёт ширину растягиваемых глифов — стрелки над
  // \vec{v} и \xrightarrow. Без него стрелка рисуется не по размеру.
  // Значение фильтрует rehypeAllowlistStyles, как и у span.
  svg: ['viewBox', 'preserveAspectRatio', 'width', 'height', 'xmlns', 'style'],
  path: ['d'],
  // Обводка \cancel — отдельный тег (у \not черта из составного символа,
  // тег line ей не нужен). Круг 2 терял line целиком.
  //
  // Только замеренное. `stroke` и `strokeLinecap` тут были — я добавил их в
  // круге 4 «инертными соседями», не замерив, и они оказались протечкой:
  // SVG-краска принимает `url(...)`, поэтому `<line stroke="url(https://
  // чужой/x.svg#g)">` из обычного markdown читателя тянул ресурс с чужого
  // хоста при отрисовке — ни клика, ни CSS-трюка. Замер 82 формул (76 плюс
  // всё семейство \cancel/\bcancel/\xcancel/\sout/\cancelto/\not): KaTeX
  // ставит на line ровно x1/y1/x2/y2/strokeWidth и ничего сверх, а цвет
  // приходит из katex.min.css (`.katex svg{stroke:currentColor}`), не из
  // атрибута. `strokeWidth` оставлен: он замерен (`0.046em`) и принимает
  // только длину, краску в него не записать.
  line: ['x1', 'y1', 'x2', 'y2', 'strokeWidth'],
};

/**
 * Sanitize schema for previewing untrusted (reader-submitted) markdown.
 * Extends the default GitHub-style schema with KaTeX's MathML/SVG output so
 * `$...$` formulas keep rendering after sanitization, then subtracts every
 * tag and attribute that would make the moderator's browser fetch a
 * reader-chosen URL on its own (see the block above).
 *
 * Формулы вычитание не задевает: KaTeX выдаёт `svg`, `path` и `line` и ни
 * одного `href`/`src`/`use` — проверено замером на 76 формулах
 * (`npm run measure:katex-styles`) и сторожевым тестом.
 */
export const markdownPreviewSanitizeSchema: Schema = {
  ...defaultSchema,
  tagNames: tagNamesWithKatex.filter((tagName) => !NETWORK_FETCHING_TAG_NAMES.has(tagName)),
  attributes: withoutNetworkAttributes(attributesWithKatex),
};

/**
 * Разбор схемы для сторожевых тестов: они обязаны видеть тот же класс, что
 * вычитает схема, иначе проверка «в наборе не осталось сетевых атрибутов»
 * сверялась бы сама с собой по другому списку.
 */
export const networkFetchingSurface = {
  tagNames: NETWORK_FETCHING_TAG_NAMES,
  attributeNames: NETWORK_FETCHING_ATTRIBUTE_NAMES,
  hrefAllowedOn: ALLOWED_HREF_TAG_NAMES,
};
