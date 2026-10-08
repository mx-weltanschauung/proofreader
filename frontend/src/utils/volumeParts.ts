// Зеркало multipartVolumes из internal/api/work_volume.go: полутома есть
// только у 25 (I—II) и 26 (I—III). Держать список здесь — дублирование,
// но альтернатива хуже: форма, из которой можно собрать запрос, заведомо
// отвергаемый сервером.
const MULTIPART_VOLUMES: Record<number, string[]> = {
  25: ['I', 'II'],
  26: ['I', 'II', 'III'],
};

export function partsForVolume(volumeNumber: number | null): string[] {
  if (volumeNumber === null) return [];
  return MULTIPART_VOLUMES[volumeNumber] ?? [];
}
