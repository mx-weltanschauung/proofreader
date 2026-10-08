import { describe, it, expect } from 'vitest';
import type { Chapter } from '../types';
import {
  chapterInApparatus,
  chapterPath,
  findChapterById,
  flattenChapters,
  siblingNeighbours,
} from './chapterTree';

// Срез тома 4: две работы верхнего уровня вокруг «Нищеты философии», у которой
// три подглавы, а у второй подглавы — своя. Поля created_at/updated_at тестам
// не нужны, поэтому приведение через `as Chapter`, как в остальных тестах.
function chapter(id: number, title: string, children?: Chapter[]): Chapter {
  return {
    id,
    work_id: 4,
    title,
    type: 'chapter',
    order_number: id,
    start_page: id,
    end_page: id,
    children,
  } as Chapter;
}

const TREE: Chapter[] = [
  chapter(229, 'Протекционизм'),
  chapter(230, 'Нищета философии', [
    chapter(281, 'ПРЕДИСЛОВИЕ'),
    chapter(282, 'Глава первая', [chapter(293, '§ 1')]),
    chapter(283, 'Глава вторая'),
  ]),
  chapter(231, 'Закат Гизо'),
];

const ids = (chapters: Chapter[]) => chapters.map((c) => c.id);

describe('flattenChapters', () => {
  it('обходит дерево в порядке чтения: родитель, затем его дети', () => {
    expect(ids(flattenChapters(TREE))).toEqual([229, 230, 281, 282, 293, 283, 231]);
  });

  it('на пустом дереве возвращает пустой список', () => {
    expect(flattenChapters([])).toEqual([]);
  });
});

describe('findChapterById', () => {
  it('находит главу на любой глубине', () => {
    expect(findChapterById(TREE, 293)?.title).toBe('§ 1');
    expect(findChapterById(TREE, 229)?.title).toBe('Протекционизм');
  });

  it('возвращает null для отсутствующего id', () => {
    expect(findChapterById(TREE, 999)).toBeNull();
  });
});

describe('chapterPath', () => {
  it('возвращает цепочку от корня до главы включительно', () => {
    expect(ids(chapterPath(TREE, 293))).toEqual([230, 282, 293]);
  });

  it('для главы верхнего уровня возвращает её одну', () => {
    expect(ids(chapterPath(TREE, 229))).toEqual([229]);
  });

  it('для отсутствующего id возвращает пустую цепочку', () => {
    expect(chapterPath(TREE, 999)).toEqual([]);
  });
});

describe('siblingNeighbours', () => {
  it('для главы верхнего уровня даёт соседей по корневому списку', () => {
    const { prev, next } = siblingNeighbours(TREE, 230);
    expect(prev?.id).toBe(229);
    expect(next?.id).toBe(231);
  });

  // Главное свойство: сосед берётся среди сестёр, а не первым ребёнком.
  it('не выдаёт первого ребёнка за следующую главу', () => {
    expect(siblingNeighbours(TREE, 230).next?.id).not.toBe(281);
  });

  it('у первой сестры нет предыдущей', () => {
    const { prev, next } = siblingNeighbours(TREE, 281);
    expect(prev).toBeNull();
    expect(next?.id).toBe(282);
  });

  it('у последней сестры нет следующей — наверх навигация не выходит', () => {
    const { prev, next } = siblingNeighbours(TREE, 283);
    expect(prev?.id).toBe(282);
    expect(next).toBeNull();
  });

  it('у единственного ребёнка соседей нет', () => {
    expect(siblingNeighbours(TREE, 293)).toEqual({ prev: null, next: null });
  });

  it('для отсутствующего id соседей нет', () => {
    expect(siblingNeighbours(TREE, 999)).toEqual({ prev: null, next: null });
  });
});

describe('chapterInApparatus', () => {
  // Признак стоит только на корне — так его ставит PUT руками.
  const leaf = chapter(3, 'Лист');
  const mid = chapter(2, 'Середина', [leaf]);
  const root = { ...chapter(1, 'Примечания', [mid]), is_apparatus: true } as Chapter;
  const body = chapter(9, 'Тело');
  const tree = [root, body];

  it('наследует признак от любого предка', () => {
    expect(chapterInApparatus(tree, leaf)).toBe(true);
    expect(chapterInApparatus(tree, mid)).toBe(true);
    expect(chapterInApparatus(tree, root)).toBe(true);
  });

  it('глава вне аппарата — нет', () => {
    expect(chapterInApparatus(tree, body)).toBe(false);
  });

  it('дерева ещё нет — решает собственный флаг', () => {
    expect(chapterInApparatus([], { ...leaf, is_apparatus: true } as Chapter)).toBe(true);
    expect(chapterInApparatus([], leaf)).toBe(false);
  });
});
