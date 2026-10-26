import type { VolumeSummary } from '../types';
import { editionStats } from './editionStats';
import { groupThousands } from './groupThousands';
import { nounForm, plural } from './volumeLabel';

/**
 * Сколько в собрании томов — первое число подписи полки. Оглавление главной
 * берёт число отсюда же, чтобы строка оглавления и заголовок полки не
 * разошлись. Книги без номеров тома называются книгами: ноль томов был бы
 * неправдой.
 */
export function volumeCountLabel(volumes: VolumeSummary[]): string {
  const stats = editionStats(volumes);
  if (stats.books === 0) return 'ни одного тома';
  if (stats.volumes === 0) return plural(stats.books, ['книга', 'книги', 'книг']);
  return plural(stats.volumes, ['том', 'тома', 'томов']);
}

/**
 * Одна фраза под заголовком собрания: сколько томов из скольких, сколько
 * страниц.
 *
 * `volumesPlanned` пуст у собрания, план которого неизвестен: тогда фраза
 * называет одно число вместо двух и не выдумывает знаменателя.
 */
export function editionCaption(volumes: VolumeSummary[], volumesPlanned?: number): string {
  const stats = editionStats(volumes);

  if (stats.books === 0) {
    return volumesPlanned ? `Пока ни одного тома из ${volumesPlanned}.` : 'Пока ни одного тома.';
  }

  // Книги есть, номеров нет — тома заведены без координат: volumeCountLabel
  // назовёт книги, и знаменатель плана к ним не приставляется.
  const counted =
    stats.volumes !== 0 && volumesPlanned
      ? `${stats.volumes} из ${volumesPlanned} ${nounForm(volumesPlanned, ['тома', 'томов', 'томов'])}`
      : volumeCountLabel(volumes);

  const pages = `${groupThousands(stats.pagesTotal)} ${nounForm(stats.pagesTotal, [
    'страница',
    'страницы',
    'страниц',
  ])}`;

  return `${counted}, ${pages}.`;
}
