import type { PageMapEntry, PageStatus } from '../types';

/** Подпись статуса в интерфейсе: то же слово, но без подчёркиваний. */
export const PAGE_STATUS_LABEL: Record<PageStatus, string> = {
  вычитана: 'вычитана',
  вычитано_машиной: 'вычитано машиной',
  вычитывается: 'вычитывается',
  требует_внимания: 'требует внимания',
  есть_проблемы: 'есть проблемы',
  пустая_страница: 'пустая',
  не_вычитана: 'не вычитана',
};

/** Латинский суффикс класса: русские статусы в CSS-селекторы не пишем. */
export const PAGE_STATUS_MODIFIER: Record<PageStatus, string> = {
  вычитана: 'read',
  вычитано_машиной: 'machine',
  вычитывается: 'doing',
  требует_внимания: 'attention',
  есть_проблемы: 'problem',
  пустая_страница: 'blank',
  не_вычитана: 'raw',
};

/** Порядок в легенде: от готового к нетронутому. */
export const PAGE_STATUS_ORDER: PageStatus[] = [
  'вычитана',
  'вычитано_машиной',
  'вычитывается',
  'требует_внимания',
  'есть_проблемы',
  'пустая_страница',
  'не_вычитана',
];

/** Сколько страниц в каждом статусе. Статусы, которых в томе нет, в ответ не
 *  попадают — легенда не должна показывать нули. */
export function countByStatus(pages: PageMapEntry[]): Record<string, number> {
  const counts: Record<string, number> = {};
  for (const page of pages) {
    counts[page.status] = (counts[page.status] ?? 0) + 1;
  }
  return counts;
}
