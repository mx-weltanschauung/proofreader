import type { VolumeSummary } from '../types';
import { editionStats } from './editionStats';
import { groupThousands } from './groupThousands';
import { nounForm } from './volumeLabel';

/**
 * Первая фраза главной для того, кто попал сюда впервые: чтó это за место и
 * какого оно размера.
 *
 * Числа стоят в начале предложения, а не в отдельных плашках: «78 томов» —
 * это содержание фразы, а не подпись к цифре, и в плашке оно перестало бы
 * быть сказанным. Второе предложение отвечает на вопрос, который остаётся
 * после первого: чем эта читальня отличается от папки сканов.
 *
 * Считается по данным, а не вписано руками: тома приезжают каждую неделю, и
 * вписанное число устарело бы к первому же прогону.
 */
export function corpusSentence(shelves: { volumes: VolumeSummary[] }[]): string {
  // Тем же счётом, что подписи собраний: иначе верх страницы и карточки
  // разошлись бы в числах (185 книг против 179 томов).
  const volumes = shelves.reduce((sum, s) => {
    const stats = editionStats(s.volumes);
    return sum + (stats.volumes > 0 ? stats.volumes : stats.books);
  }, 0);
  if (volumes === 0) return '';

  const pages = shelves.reduce(
    (sum, s) => sum + s.volumes.reduce((inner, v) => inner + v.pages_total, 0),
    0,
  );

  return (
    `${volumes} ${nounForm(volumes, ['том', 'тома', 'томов'])}, ` +
    `${groupThousands(pages)} ${nounForm(pages, ['страница', 'страницы', 'страниц'])}. ` +
    'Каждая страница снята со скана и разобрана заново; печатная нумерация ' +
    'сохранена — ссылаться можно как на бумажный том.'
  );
}
