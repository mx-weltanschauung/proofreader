import type { VolumeSummary } from '../types';
import { editionStats } from './editionStats';
import { groupThousands } from './groupThousands';
import { nounForm, plural } from './volumeLabel';

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

  // Книги есть, номеров нет — тома заведены без координат. Ноль томов был бы
  // неправдой, поэтому называем книги.
  const counted =
    stats.volumes === 0
      ? plural(stats.books, ['книга', 'книги', 'книг'])
      : volumesPlanned
        ? `${stats.volumes} из ${volumesPlanned} ${nounForm(volumesPlanned, ['тома', 'томов', 'томов'])}`
        : plural(stats.volumes, ['том', 'тома', 'томов']);

  const pages = `${groupThousands(stats.pagesTotal)} ${nounForm(stats.pagesTotal, [
    'страница',
    'страницы',
    'страниц',
  ])}`;

  return `${counted}, ${pages}.`;
}
