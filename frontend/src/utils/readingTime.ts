/** Видимые слова основного текста; дефис внутри слова не делит его пополам. */
export function countReadingWords(text: string): number {
  return text.match(/[\p{L}\p{N}]+(?:[-’'][\p{L}\p{N}]+)*/gu)?.length ?? 0;
}

/** Оценка при условном темпе 180 слов в минуту. */
export function formatReadingDuration(words: number): string | null {
  if (words <= 0) return null;
  if (words < 180) return 'меньше минуты';
  const minutes = Math.ceil(words / 180);
  if (minutes < 60) return `≈ ${minutes} мин`;
  const hours = Math.floor(minutes / 60);
  const rest = minutes % 60;
  return `≈ ${hours} ч${rest ? ` ${rest} мин` : ''}`;
}
