import type { SearchVolume } from '../types';

/**
 * Служебные работы (передние листы тома) сервер отдаёт в общем порядке по
 * счёту; на экране им место под родителем. Порядок остальных строк — как
 * пришёл; сирота, чьего родителя в выдаче нет, остаётся на своём месте.
 */
export function orderVolumes(volumes: SearchVolume[]): SearchVolume[] {
  const parents = new Set(volumes.filter((v) => v.parent_work_id === null).map((v) => v.work_id));
  const children = new Map<number, SearchVolume[]>();
  for (const v of volumes) {
    if (v.parent_work_id !== null && parents.has(v.parent_work_id)) {
      children.set(v.parent_work_id, [...(children.get(v.parent_work_id) ?? []), v]);
    }
  }
  const out: SearchVolume[] = [];
  for (const v of volumes) {
    if (v.parent_work_id !== null && parents.has(v.parent_work_id)) continue;
    out.push(v, ...(children.get(v.work_id) ?? []));
  }
  return out;
}
