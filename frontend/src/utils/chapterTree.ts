import type { Chapter } from '../types';

/** Глубокий обход в порядке чтения: сначала глава, затем её дети. */
export function flattenChapters(chapters: Chapter[]): Chapter[] {
  const result: Chapter[] = [];
  const walk = (items: Chapter[]) => {
    for (const item of items) {
      result.push(item);
      if (item.children && item.children.length > 0) walk(item.children);
    }
  };
  walk(chapters);
  return result;
}

export function findChapterById(chapters: Chapter[], id: number): Chapter | null {
  for (const chapter of chapters) {
    if (chapter.id === id) return chapter;
    if (chapter.children && chapter.children.length > 0) {
      const found = findChapterById(chapter.children, id);
      if (found) return found;
    }
  }
  return null;
}

/** Глава внутри аппарата: помечена она сама или любой предок. Признак ставится
 *  руками через PUT на корень и детям не проставляется, а раскладка озвучки
 *  (tools/tts, build._узлы) наследует его вниз. Главы нет в дереве (оно ещё не
 *  пришло) — решает её собственный флаг. */
export function chapterInApparatus(chapters: Chapter[], chapter: Chapter): boolean {
  return chapter.is_apparatus || chapterPath(chapters, chapter.id).some((c) => c.is_apparatus);
}

/** Цепочка от корня до главы включительно; пустой массив, если главы в дереве нет. */
export function chapterPath(chapters: Chapter[], id: number): Chapter[] {
  for (const chapter of chapters) {
    if (chapter.id === id) return [chapter];
    if (chapter.children && chapter.children.length > 0) {
      const tail = chapterPath(chapter.children, id);
      if (tail.length > 0) return [chapter, ...tail];
    }
  }
  return [];
}

export interface SiblingNeighbours {
  prev: Chapter | null;
  next: Chapter | null;
}

/**
 * Соседи главы внутри её списка сестёр: детей родителя, а для главы верхнего
 * уровня — корневого списка. На краю списка — null: наверх, к соседям
 * родителя, навигация стрелками не выходит, для этого есть цепочка предков.
 *
 * Порядок берётся такой, какой пришёл из API: запрос глав сортирует по
 * order_number (internal/repository/chapter_repository.go:74).
 */
export function siblingNeighbours(chapters: Chapter[], id: number): SiblingNeighbours {
  const path = chapterPath(chapters, id);
  if (path.length === 0) return { prev: null, next: null };

  const parent = path.length > 1 ? path[path.length - 2] : null;
  const siblings = parent ? (parent.children ?? []) : chapters;
  const index = siblings.findIndex((c) => c.id === id);

  return {
    prev: index > 0 ? siblings[index - 1] : null,
    next: index >= 0 && index < siblings.length - 1 ? siblings[index + 1] : null,
  };
}
