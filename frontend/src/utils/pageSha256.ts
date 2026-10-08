/**
 * Отпечаток текста полосы. По нему сервер сверяет основу при подаче: хэш, а не
 * метка времени, потому что важно «текст, который я правил, всё ещё тот».
 *
 * crypto.subtle доступен только в защищённом контексте (https или localhost) —
 * в читальне оба случая выполняются.
 */
export async function pageSha256(text: string): Promise<string> {
  const bytes = new TextEncoder().encode(text);
  const digest = await crypto.subtle.digest('SHA-256', bytes);
  return Array.from(new Uint8Array(digest))
    .map((b) => b.toString(16).padStart(2, '0'))
    .join('');
}
