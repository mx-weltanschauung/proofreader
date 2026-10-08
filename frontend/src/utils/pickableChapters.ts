import type { Chapter } from '../types';

/** Главы тома плоским списком с глубиной; аппарат с поддеревом выброшен. */
export function pickableChapters(
  tree: Chapter[],
  depth = 0,
): { chapter: Chapter; depth: number }[] {
  return tree.flatMap((c) =>
    c.is_apparatus ? [] : [{ chapter: c, depth }, ...pickableChapters(c.children ?? [], depth + 1)],
  );
}
