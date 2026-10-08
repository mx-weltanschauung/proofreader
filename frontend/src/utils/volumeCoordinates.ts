import type { Work } from '../types';

/**
 * Координаты работы внутри собрания: «т. 4», «т. 25, I».
 *
 * Не путать с `volumeLabel` из соседнего модуля — там подпись корешка по
 * главной работе тома, то есть чем том содержательно является. Здесь —
 * его адрес в собрании, то, чем на него ссылается указатель.
 *
 * Работа без volume_number — справочный том собрания (так в нём лежит сам
 * указатель), и это не пустое место, а осмысленная роль. `spineNumber`
 * сокращает этот случай до «ук» ради узкого корешка; в списке томов есть
 * место назвать его словами.
 */
export function volumeCoordinates(work: Work): string {
  if (work.volume_number === undefined || work.volume_number === null) {
    return 'справочный том';
  }
  return work.volume_part
    ? `т. ${work.volume_number}, ${work.volume_part}`
    : `т. ${work.volume_number}`;
}
