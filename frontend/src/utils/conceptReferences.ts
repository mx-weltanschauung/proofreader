import type { ConceptReference } from '../types';
import { pageRange } from './pageRange';

/** Подпись адреса печатными номерами — теми, что стоят в указателе. */
export function referenceLabel(ref: ConceptReference): string {
  const volume = ref.volume_part
    ? `т. ${ref.volume_number}, ${ref.volume_part}`
    : `т. ${ref.volume_number}`;
  return `${volume} · с. ${pageRange(ref.page_start, ref.page_end)}`;
}

/**
 * Путь подрубрик адреса от корня к листу. `rubric_path` — то, что прислал
 * сервер (задача 8/9); когда его нет (адрес совсем без подрубрики или ответ
 * ещё старой формы), опираемся на плоский `rubric` — пустая строка даёт
 * пустой путь, то есть безрубричную группу, как было всегда.
 */
export function pathOf(ref: ConceptReference): string[] {
  if (ref.rubric_path && ref.rubric_path.length > 0) return ref.rubric_path;
  return ref.rubric ? [ref.rubric] : [];
}

/** Одна подрубрика статьи: свои адреса плюс вложенные подрубрики глубже. */
export interface RubricGroup {
  /** Название подрубрики на этом уровне; пустая строка — адреса без подрубрики. */
  rubric: string;
  /** Путь от корня до этого уровня включительно — нужен для якоря. */
  path: string[];
  /** Адреса, у которых путь заканчивается ровно на этом уровне. */
  refs: ConceptReference[];
  /** Более глубокие подрубрики (задача 9: «съезд» → «аспект»). */
  children: RubricGroup[];
}

/**
 * Раскладывает адреса по подрубрикам, с вложенностью. Порядок групп на
 * каждом уровне — порядок первого появления, внутри группы — order_number:
 * и то и другое воспроизводит печатную статью, а не переизобретает её.
 *
 * Группировка ТОЛЬКО по листу (как было до задачи 9) не годится: у статьи
 * «КПСС — съезды» лист «значение съезда» стоит под семью разными съездами —
 * слить их по листу значило бы перепутать адреса разных съездов в одну
 * группу. Группируем по полному пути; глубина в базе сегодня максимум 2, но
 * запрос рекурсивный, поэтому функция рекурсивная тоже и не завязана на
 * конкретную глубину.
 */
export function groupByRubric(refs: ConceptReference[]): RubricGroup[] {
  return buildLevel(
    refs.map((ref) => ({ ref, path: pathOf(ref) })),
    0,
    [],
  );
}

function buildLevel(
  items: { ref: ConceptReference; path: string[] }[],
  depth: number,
  parentPath: string[],
): RubricGroup[] {
  const order: string[] = [];
  const own = new Map<string, ConceptReference[]>();
  const deeper = new Map<string, { ref: ConceptReference; path: string[] }[]>();

  for (const item of items) {
    const title = item.path[depth] ?? '';
    if (!order.includes(title)) order.push(title);
    if (item.path.length > depth + 1) {
      const bucket = deeper.get(title);
      if (bucket) bucket.push(item);
      else deeper.set(title, [item]);
    } else {
      const bucket = own.get(title);
      if (bucket) bucket.push(item.ref);
      else own.set(title, [item.ref]);
    }
  }

  return order.map((title) => {
    const path = title === '' ? parentPath : [...parentPath, title];
    return {
      rubric: title,
      path,
      refs: (own.get(title) ?? []).sort((a, b) => a.order_number - b.order_number),
      children: buildLevel(deeper.get(title) ?? [], depth + 1, path),
    };
  });
}

/** Накрывает ли `prefix` путь `path`. Пустой префикс накрывает всё. */
export function hasPathPrefix(path: readonly string[], prefix: readonly string[]): boolean {
  if (prefix.length > path.length) return false;
  return prefix.every((part, i) => path[i] === part);
}

/**
 * Подпись подрубрики одной строкой. Путь короче двух звеньев — плоский
 * случай, рисуется одним листом, как было всегда. Путь длиннее — родитель
 * впереди листа через тире: «значение съезда» стоит под семью разными
 * съездами, и без родителя семь записей одного понятия неразличимы.
 */
export function rubricPathLabel(ref: { rubric: string; rubric_path?: readonly string[] }): string {
  const path = ref.rubric_path;
  return path && path.length > 1 ? path.join(' — ') : ref.rubric;
}

/** Одна опция фильтра: подпись и путь, который уезжает в `?rubric_path=`. */
export interface RubricOption {
  label: string;
  path: string[];
}

/** Группа опций. Пустая подпись — опции верхнего уровня, без `<optgroup>`. */
export interface RubricOptionGroup {
  label: string;
  options: RubricOption[];
}

/**
 * Опции фильтра подрубрик: корни в печатном порядке, у корня с детьми —
 * группа, первой опцией «весь раздел».
 *
 * Строится из groupByRubric, а не из плоского списка листьев: плоский список
 * схлопывал одноимённые подрубрики разных родителей (у статьи «КПСС — съезды»
 * 45 подрубрик из 204 не имели своей опции вовсе), а корень без собственных
 * адресов в него не попадал — трёх съездов из двенадцати выбрать было нельзя.
 *
 * Статья без вложенности получает ровно то, что получала: одну группу с
 * пустой подписью, то есть плоский список без единого <optgroup>.
 */
export function rubricOptions(refs: ConceptReference[]): RubricOptionGroup[] {
  const out: RubricOptionGroup[] = [];
  let flat: RubricOption[] = [];
  const flush = () => {
    if (flat.length > 0) {
      out.push({ label: '', options: flat });
      flat = [];
    }
  };

  for (const group of groupByRubric(refs)) {
    // Безрубричные адреса опции не получают: их показывает «все», а отдельной
    // опцией «без подрубрики» экран никогда не располагал.
    if (group.rubric === '') continue;
    if (group.children.length === 0) {
      flat.push({ label: group.rubric, path: group.path });
      continue;
    }
    // Порядок корней — печатный, поэтому накопленные плоские опции
    // выкладываются ПЕРЕД группой, а не в конец списка.
    flush();
    out.push({
      label: group.rubric,
      options: [{ label: 'весь раздел', path: group.path }, ...descendants(group)],
    });
  }
  flush();
  return out;
}

/**
 * Все потомки корня одним уровнем: `<optgroup>` в HTML не вкладываются.
 * На сегодняшней глубине 2 подпись — один лист; глубже — хвост пути целиком,
 * иначе два разных внука с одинаковым именем снова стали бы неразличимы.
 */
function descendants(root: RubricGroup): RubricOption[] {
  const out: RubricOption[] = [];
  const walk = (node: RubricGroup) => {
    for (const child of node.children) {
      out.push({ label: child.path.slice(root.path.length).join(' — '), path: child.path });
      walk(child);
    }
  };
  walk(root);
  return out;
}
