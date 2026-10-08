// Ошибки от axios приходят как unknown, а разбирали их по всему фронтенду
// через `catch (err: any)` — 29 таких мест. Разбор живёт здесь, чтобы any
// не возвращался в компоненты.

function responseOf(err: unknown): Record<string, unknown> | undefined {
  if (typeof err !== 'object' || err === null) return undefined;
  const response = (err as { response?: unknown }).response;
  if (typeof response !== 'object' || response === null) return undefined;
  return response as Record<string, unknown>;
}

/** Код ответа сервера, если ошибка пришла с ответом. */
export function apiErrorStatus(err: unknown): number | undefined {
  const status = responseOf(err)?.status;
  return typeof status === 'number' ? status : undefined;
}

/** Текст ошибки из тела ответа; при любой другой форме — fallback. */
export function apiErrorMessage(err: unknown, fallback: string): string {
  const data = responseOf(err)?.data;
  if (typeof data !== 'object' || data === null) return fallback;
  const message = (data as { message?: unknown }).message;
  return typeof message === 'string' && message.length > 0 ? message : fallback;
}
