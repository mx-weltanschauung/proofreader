import type { Chapter } from '../types';

function childrenOf(chapter: Chapter): Chapter[] {
  return chapter.children ?? [];
}

/** Дальше какого уровня сегменты не перечисляются поимённо. */
export const SEGMENT_CAP = 4;

/**
 * Значения сегментов органа глубины. Набор строится по тому: `1…min(предел, 4)`,
 * плюс текущее значение и умолчание тома, если они выпали за четвёрку, плюс сам предел («всё»).
 *
 * Обрезка на четырёх бьёт по двум томам из пятидесяти (3 и 47 МиЭ), и там
 * слепой прыжок на одиннадцатый уровень всё равно хуже ручного раскрытия
 * нужной ветки. Пустой массив — управлять нечем: у тома один уровень.
 */
export function depthSegments(metrics: OutlineMetrics, current: number): number[] {
  const limit = metrics.max;
  if (limit <= 1) return [];

  const values = new Set<number>();
  for (let depth = 1; depth <= Math.min(limit, SEGMENT_CAP); depth += 1) values.add(depth);
  if (current >= 1 && current <= limit) values.add(current);
  // Умолчание тома — среди сегментов всегда: это тот вид, с которого том
  // открылся, и вернуться к нему надо уметь не только кнопкой «назад».
  if (metrics.fallback >= 1 && metrics.fallback <= limit) values.add(metrics.fallback);
  values.add(limit);

  return [...values].sort((a, b) => a - b);
}

/** Узлов на каждом уровне дерева: индекс 0 — верхний уровень тома. */
export function levelCounts(chapters: Chapter[]): number[] {
  const counts: number[] = [];
  const walk = (items: Chapter[], level: number) => {
    if (items.length === 0) return;
    counts[level] = (counts[level] ?? 0) + items.length;
    for (const item of items) walk(childrenOf(item), level + 1);
  };
  walk(chapters, 0);
  return counts;
}

/**
 * Сколько уровней в дереве — см. `OutlineMetrics.max`.
 *
 * Тонкая обёртка над `outlineMetrics`; в боевом коде не зовётся, оставлена
 * как названный спекой контракт.
 */
export function maxDepth(chapters: Chapter[]): number {
  return levelCounts(chapters).length;
}

/**
 * Мерки дерева тома, посчитанные одним обходом.
 *
 * Прежде их считали врозь, и дерево обходилось четырежды за рендер:
 * `depthFromParam` звал `defaultDepth` и `maxDepth` (каждый — свой
 * `levelCounts`), счётчик строил второе полное дерево ради подсчёта узлов,
 * орган глубины звал `depthSegments` и `maxDepth`. На 197 узлах это
 * незаметно, но считать одно и то же четыре раза незачем ни на каком числе.
 */
export interface OutlineMetrics {
  /** Узлов на каждом уровне: индекс 0 — верхний уровень тома. */
  levels: number[];
  /** Сколько уровней в дереве. Пустое дерево — 0. */
  max: number;
  /** Глубина, на которой содержание открывается впервые. */
  fallback: number;
  /** Узлов в дереве всего — во столько строк обходится «всё». */
  total: number;
}

/** Мерки дерева тома, посчитанные одним обходом — см. `OutlineMetrics`. */
export function outlineMetrics(chapters: Chapter[], min = 12): OutlineMetrics {
  const levels = levelCounts(chapters);
  const max = levels.length;

  // Наименьшая глубина, где видно не меньше `min` строк. Если столько строк
  // в томе нет вовсе — весь том; на пустом дереве — единица, глубина 0
  // бессмысленна.
  let shown = 0;
  let fallback = max === 0 ? 1 : max;
  for (let depth = 1; depth <= max; depth += 1) {
    shown += levels[depth - 1];
    if (shown >= min) {
      fallback = depth;
      break;
    }
  }

  return { levels, max, fallback, total: levels.reduce((sum, n) => sum + n, 0) };
}

/**
 * Глубина, на которой содержание открывается впервые — см. `outlineMetrics`.
 *
 * Это правило обобщает прежний автоспуск («на уровне ровно один узел —
 * спускаемся»): тот срабатывал ровно на одном томе из пятидесяти, а мало
 * строк на верхнем уровне бывает и по другим причинам. В томе 3 МиЭ верхних
 * глав три, и показывать их втроём так же бессмысленно, как одну.
 *
 * Тонкая обёртка над `outlineMetrics`; в боевом коде не зовётся, оставлена
 * как названный спекой контракт.
 */
export function defaultDepth(chapters: Chapter[], min = 12): number {
  return outlineMetrics(chapters, min).fallback;
}

/**
 * Глубина по параметру адреса `?depth=`. Мусор, ноль, отрицательное и число
 * больше предельной глубины откатываются на глубину по умолчанию — молча, без
 * сообщения об ошибке: пустого экрана читатель видеть не должен.
 */
export function depthFromParam(metrics: OutlineMetrics, param: string | null): number {
  const fallback = metrics.fallback;
  if (param === null || param.trim() === '') return fallback;

  const value = Number(param);
  if (!Number.isInteger(value)) return fallback;
  if (value < 1 || value > metrics.max) return fallback;
  return value;
}

/**
 * Уровень работ тома: первый, на котором узлов больше одного. Нужен шапке
 * тома, которая описывает том, а не текущий вид содержания, и потому не
 * может считать работы по выбранной читателем глубине.
 */
export function worksLevel(chapters: Chapter[]): Chapter[] {
  let level = chapters;
  while (level.length === 1) {
    const kids = childrenOf(level[0]);
    if (kids.length === 0) break;
    level = kids;
  }
  // Спуск мог упереться в лист, так и не найдя уровня с несколькими узлами
  // (цепочка одиночных вложений). Тогда работой считается верхний уровень:
  // спустившись, мы назвали бы работой внутренность единственной работы.
  return level.length > 1 ? level : chapters;
}

export interface OutlineNode {
  chapter: Chapter;
  /** 1 — верхний уровень тома. */
  level: number;
  /** Есть ли дети в данных — по нему рисуется треугольник. */
  hasChildren: boolean;
  /** Показаны ли дети сейчас. */
  isOpen: boolean;
  /** Уже отобранные видимые дети. */
  children: OutlineNode[];
}

/**
 * Ручные правки поверх базовой глубины. Два множества, а не одно, потому что
 * база подвижна: `expanded` — узлы, раскрытые глубже базы, `collapsed` — узлы
 * внутри базы, свёрнутые вручную. С одним множеством свёрнутый узел
 * возвращался бы сам при любом пересчёте.
 */
export interface OutlineState {
  expanded: Set<number>;
  collapsed: Set<number>;
}

/**
 * Пустое состояние ручных правок — новое на каждый вызов.
 *
 * Была общая константа-умолчание, и её множества никто не защищал: прямая
 * правка испортила бы умолчание сразу всем потребителям.
 * `Object.freeze` этого не решает — множество хранит данные во внутренних
 * слотах, и `Object.freeze(new Set()).add(1)` проходит молча. Единственная
 * настоящая защита — не делиться изменяемым объектом.
 */
export function emptyOutlineState(): OutlineState {
  return { expanded: new Set(), collapsed: new Set() };
}

function isOpenAt(id: number, level: number, depth: number, state: OutlineState): boolean {
  if (state.expanded.has(id)) return true;
  return level < depth && !state.collapsed.has(id);
}

/** Дерево видимых узлов в порядке чтения. */
export function outlineTree(
  chapters: Chapter[],
  depth: number,
  state: OutlineState,
): OutlineNode[] {
  const build = (items: Chapter[], level: number): OutlineNode[] =>
    items.map((chapter) => {
      const kids = childrenOf(chapter);
      const open = kids.length > 0 && isOpenAt(chapter.id, level, depth, state);
      return {
        chapter,
        level,
        hasChildren: kids.length > 0,
        isOpen: open,
        children: open ? build(kids, level + 1) : [],
      };
    });

  return build(chapters, 1);
}

/** Сколько строк видно — считая вложенные. */
export function countNodes(nodes: OutlineNode[]): number {
  return nodes.reduce((sum, node) => sum + 1 + countNodes(node.children), 0);
}

/**
 * Новое состояние после нажатия на треугольник. Четыре случая, и ни один не
 * может оставить противоречие «узел одновременно в expanded и collapsed»:
 * внутри базы правится только `collapsed`, глубже базы — только `expanded`.
 */
export function toggleNode(node: OutlineNode, depth: number, state: OutlineState): OutlineState {
  const expanded = new Set(state.expanded);
  const collapsed = new Set(state.collapsed);
  const insideBase = node.level < depth;

  if (node.isOpen) {
    if (insideBase) collapsed.add(node.chapter.id);
    else expanded.delete(node.chapter.id);
  } else {
    if (insideBase) collapsed.delete(node.chapter.id);
    else expanded.add(node.chapter.id);
  }

  return { expanded, collapsed };
}

/**
 * Дерево, отфильтрованное предикатом рекурсивно — а не только по корню.
 * Фасет автора живёт на любом уровне (в томе 6 — на втором), и резать одни
 * верхние узлы значит либо прятать совпавшего потомка вместе с непричастным
 * родителем, либо показывать под совпавшим родителем чужих детей.
 *
 * Узел остаётся, если совпадает сам (тогда поддерево остаётся целиком, как
 * оно показано сейчас) или совпадает кто-то из потомков (тогда внутри
 * остаются только отфильтрованные ветки).
 */
export function filterOutlineTree(
  nodes: OutlineNode[],
  matches: (chapter: Chapter) => boolean,
): OutlineNode[] {
  const result: OutlineNode[] = [];
  for (const node of nodes) {
    if (matches(node.chapter)) {
      result.push(node);
      continue;
    }
    const children = filterOutlineTree(node.children, matches);
    if (children.length > 0) result.push({ ...node, children });
  }
  return result;
}

export interface SearchHit {
  chapter: Chapter;
  /** Контейнеры от корня до найденной главы, без неё самой. */
  path: Chapter[];
}

/**
 * Сквозной поиск по заглавиям всех глав тома на любой глубине. Порядок —
 * обход дерева в порядке чтения, тот же, в каком главы идут в томе.
 */
export function searchTree(chapters: Chapter[], query: string): SearchHit[] {
  const needle = query.trim().toLowerCase();
  if (needle === '') return [];

  const hits: SearchHit[] = [];
  const walk = (items: Chapter[], path: Chapter[]) => {
    for (const chapter of items) {
      if (chapter.title.toLowerCase().includes(needle)) hits.push({ chapter, path });
      const kids = childrenOf(chapter);
      if (kids.length > 0) walk(kids, [...path, chapter]);
    }
  };
  walk(chapters, []);
  return hits;
}
