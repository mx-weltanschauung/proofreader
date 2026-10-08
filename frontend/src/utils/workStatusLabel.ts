import type { WorkStatus } from '../types';

/**
 * Подпись статуса работы. Значения (`draft`, `in_progress`, `completed`,
 * `archived`) — данные: они уходят в API и лежат в базе, переводится только
 * показанное человеку.
 */
const LABELS: Record<string, string> = {
  draft: 'Черновик',
  in_progress: 'В работе',
  completed: 'Завершена',
  archived: 'В архиве',
};

export function workStatusLabel(status: WorkStatus): string {
  return LABELS[status] ?? status;
}

export const WORK_STATUSES: WorkStatus[] = ['draft', 'in_progress', 'completed', 'archived'];
