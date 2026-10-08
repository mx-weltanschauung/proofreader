import type { Chapter } from '../types';

/**
 * Глава для проверок оглавления. Помощник жил двумя копиями — в
 * `volumeOutline.test.ts` и `VolumeDepthControl.test.tsx`, — и копии успели
 * разойтись сигнатурами: одна принимала заглавие, другая сочиняла его сама.
 *
 * Диапазон страниц по умолчанию вырожденный (`id…id`): проверкам дерева он
 * безразличен, а тем, кому важен, задаётся явно.
 *
 * `VolumeOutline.test.tsx` и `VolumeOutlineRow.test.tsx` держат свои
 * заготовки: там диапазон страниц идёт позиционно и участвует в проверках,
 * и перевод их сюда — правка ради правки.
 */
export function chapter(
  id: number,
  title = `Глава ${id}`,
  children?: Chapter[],
  pages: { start: number; end: number } = { start: id, end: id },
): Chapter {
  return {
    id,
    work_id: 43,
    title,
    type: 'chapter',
    order_number: id,
    start_page: pages.start,
    end_page: pages.end,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    ...(children ? { children } : {}),
  };
}
