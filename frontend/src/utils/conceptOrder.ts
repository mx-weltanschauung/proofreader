import type { ConceptOrder } from '../types';

/** Порядок по умолчанию — тот, которым устроена печатная статья указателя. */
export const DEFAULT_CONCEPT_ORDER: ConceptOrder = 'rubric';

/**
 * Разбирает значение порядка из URL. Неизвестное значение даёт дефолт: URL
 * правят руками и присылают ссылками, и мусор в нём не повод показать пустой
 * экран — ровно то же правило, что на сервере.
 */
export function parseConceptOrder(raw: string | null): ConceptOrder {
  return raw === 'page' ? 'page' : DEFAULT_CONCEPT_ORDER;
}
