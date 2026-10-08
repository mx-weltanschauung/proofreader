/**
 * Фильтр по тому в URL — одно значение, а не два параметра: `?volume=12` или
 * `?volume=25-II`. Часть тома без номера ничего не значит, поэтому и хранить
 * их раздельно незачем.
 */
export interface VolumeFilter {
  volume: number;
  part?: string;
}

const VALUE = /^(\d+)(?:-(.+))?$/;

export function parseVolumeFilter(raw: string): VolumeFilter | null {
  const match = VALUE.exec(raw.trim());
  if (!match) return null;
  const volume = Number(match[1]);
  if (!Number.isInteger(volume) || volume <= 0) return null;
  return match[2] ? { volume, part: match[2] } : { volume };
}

export function volumeFilterValue(volumeNumber: number, part?: string): string {
  return part ? `${volumeNumber}-${part}` : `${volumeNumber}`;
}
