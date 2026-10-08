import type { ConceptEntry } from '../types';
import { pageRange } from './pageRange';

/**
 * Якорь записи потока. Ключ — адрес, а не страница: два адреса на одну
 * страницу (разные подрубрики) — обычное дело у этого указателя, и якорь по
 * странице увёл бы обе ссылки панели в одно место.
 */
export function entryAnchorId(referenceId: number): string {
  return `frag-ref-${referenceId}`;
}

/** Адрес записи печатными номерами — теми, что стоят в указателе. */
export function entryAddressLabel(entry: ConceptEntry): string {
  const volume = entry.volume_part
    ? `т. ${entry.volume_number}, ${entry.volume_part}`
    : `т. ${entry.volume_number}`;
  return `${volume} · с. ${pageRange(entry.printed_start, entry.printed_end)}`;
}

/** Один заголовок группы в потоке: уровень пути, название и ключ для React. */
export interface RubricHeading {
  level: number;
  title: string;
  key: string;
}

/**
 * Уровни пути, сменившиеся по сравнению с предыдущей записью, сверху вниз.
 *
 * Сравнение с соседом, а не группировка в дерево: записи приходят порциями и
 * только дописываются в хвост, так что заголовок обязан вставать там, где
 * группа сменилась, независимо от того, в какой порции это случилось.
 *
 * Смена уровня означает смену и всех уровней ниже: перейдя от «II съезд ->
 * значение съезда» к «III съезд -> значение съезда», печатаем ОБА заголовка,
 * хотя лист совпал, — иначе новый съезд начался бы без объявления, а лист
 * «значение съезда» стоит под семью съездами сразу.
 */
export function changedLevels(
  // Дефолт — на случай отставшего локального бэкенда, который ещё не шлёт
  // path: тип поля обязательный, но в этом проекте уже обжигались на том,
  // что docker-бэкенд отстаёт от master и фронт ломается на ровном месте.
  path: readonly string[] = [],
  prev: readonly string[] = [],
): RubricHeading[] {
  let from = path.length;
  for (let i = 0; i < path.length; i++) {
    if (path[i] !== prev[i]) {
      from = i;
      break;
    }
  }
  // Путь стал КОРОЧЕ предыдущего, оставшись его началом: собственные адреса
  // раздела пошли после его аспектов. Заголовков «сменилось» ноль, но группа
  // сменилась, и без повтора подписи раздела записи легли бы под последним
  // напечатанным заголовком аспекта — то есть под чужим.
  if (from === path.length && prev.length > path.length && path.length > 0) {
    from = path.length - 1;
  }
  return path.slice(from).map((title, k) => ({
    level: from + k,
    title,
    // Ключ — весь префикс, а не название: «значение съезда» повторяется под
    // семью съездами, и одинаковые ключи React перепутал бы между собой.
    key: path.slice(0, from + k + 1).join('\u0000'),
  }));
}
