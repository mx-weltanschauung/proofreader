import type { UserRole } from '../types';

/** Роль по-русски. В API она остаётся administrator/editor. */
export function roleLabel(role: UserRole): string {
  return role === 'administrator' ? 'администратор' : 'редактор';
}
