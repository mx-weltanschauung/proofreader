// Зеркало romanParts из internal/api/work_volume.go: полутом I—III бывает у
// любого тома с номером; у тома без номера полутома нет.
const PARTS = ['I', 'II', 'III'];

export function partsForVolume(volumeNumber: number | null): string[] {
  if (volumeNumber === null) return [];
  return PARTS;
}
