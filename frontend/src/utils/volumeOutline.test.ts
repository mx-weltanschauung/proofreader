import { describe, it, expect } from 'vitest';
import type { Chapter } from '../types';
import {
  countNodes,
  defaultDepth,
  depthFromParam,
  depthSegments,
  emptyOutlineState,
  filterOutlineTree,
  levelCounts,
  maxDepth,
  outlineMetrics,
  outlineTree,
  toggleNode,
  searchTree,
  type OutlineNode,
  type OutlineState,
  worksLevel,
} from './volumeOutline';
import { chapter } from '../test/chapterFixture';

// Том 14 Плеханова: одна часть на весь том, под ней девять работ, у первой —
// три письма третьим уровнем. Уровни: 1, 9, 3.
const VOLUME_43: Chapter[] = [
  chapter(1875, 'Искусство и литература', [
    chapter(1876, 'Письма без адреса', [
      chapter(1885, 'Письмо первое'),
      chapter(1886, 'Письмо второе'),
      chapter(1887, 'Письмо третье'),
    ]),
    chapter(1877, 'Пролетарское движение'),
    chapter(1878, 'Французская драматическая литература'),
    chapter(1879, 'Искусство и общественная жизнь'),
    chapter(1880, 'Предисловие к 3-му изданию'),
    chapter(1881, 'Генрик Ибсен'),
    chapter(1882, 'Сын доктора Стокмана'),
    chapter(1883, 'Идеология мещанина'),
    chapter(1884, 'О книге Философова'),
  ]),
];

// Плоский широкий корень — так устроены 38 томов из 50.
const FLAT: Chapter[] = [chapter(1, 'Первая'), chapter(2, 'Вторая'), chapter(3, 'Третья')];

// Уровни 1,1,1,1,1,12,1: дюжина строк набирается впервые на шестом уровне —
// туда и падает умолчание, — а предел тома седьмой. Такого тома в корпусе
// сейчас нет; форма взята с тома 3 МиЭ, где уровней одиннадцать, а верхние
// узкие.
const DEEP_DEFAULT: Chapter[] = [
  chapter(1, 'Первый', [
    chapter(2, 'Второй', [
      chapter(3, 'Третий', [
        chapter(4, 'Четвёртый', [
          chapter(5, 'Пятый', [
            chapter(10, 'Шестой с ребёнком', [chapter(100, 'Седьмой')]),
            ...Array.from({ length: 11 }, (_, i) => chapter(11 + i, `Шестой ${i}`)),
          ]),
        ]),
      ]),
    ]),
  ]),
];

describe('levelCounts', () => {
  it('считает узлы на каждом уровне', () => {
    expect(levelCounts(VOLUME_43)).toEqual([1, 9, 3]);
  });

  it('на плоском томе — один уровень', () => {
    expect(levelCounts(FLAT)).toEqual([3]);
  });

  it('на пустом дереве — пусто', () => {
    expect(levelCounts([])).toEqual([]);
  });
});

describe('maxDepth', () => {
  it('том 43 — три уровня', () => {
    expect(maxDepth(VOLUME_43)).toBe(3);
  });

  it('плоский том — один', () => {
    expect(maxDepth(FLAT)).toBe(1);
  });

  it('пустое дерево — ноль', () => {
    expect(maxDepth([])).toBe(0);
  });
});

describe('outlineMetrics', () => {
  it('считает мерки тома 43 одним обходом', () => {
    expect(outlineMetrics(VOLUME_43)).toEqual({
      levels: [1, 9, 3],
      max: 3,
      fallback: 3,
      total: 13,
    });
  });

  it('на плоском томе — один уровень, умолчание на нём же', () => {
    expect(outlineMetrics(FLAT)).toEqual({ levels: [3], max: 1, fallback: 1, total: 3 });
  });

  it('на пустом дереве умолчание — единица, а не ноль: глубина 0 бессмысленна', () => {
    expect(outlineMetrics([])).toEqual({ levels: [], max: 0, fallback: 1, total: 0 });
  });

  /*
   * Счётчик строк раньше строил ВТОРОЕ полное дерево (`countNodes(outlineTree(
   * chapters, maxDepth(chapters), emptyOutlineState()))`) ради одного числа.
   * `total` заменяет его суммой по уровням. Проверка сторожит саму замену:
   * если эти величины разойдутся, счётчик начнёт врать молча.
   */
  it('total совпадает с подсчётом узлов полностью раскрытого дерева', () => {
    for (const tree of [VOLUME_43, FLAT, []]) {
      const metrics = outlineMetrics(tree);
      expect(metrics.total).toBe(countNodes(outlineTree(tree, metrics.max, emptyOutlineState())));
    }
  });
});

describe('defaultDepth', () => {
  it('широкий корень открывается первым уровнем', () => {
    expect(defaultDepth(FLAT, 2)).toBe(1);
  });

  it('том 43 раскрывается до третьего уровня: на первых двух строк мало', () => {
    // уровни 1, 9, 3 → нарастающим итогом 1, 10, 13; порог 12 берётся на третьем
    expect(defaultDepth(VOLUME_43)).toBe(3);
  });

  it('при пороге 5 хватает второго уровня', () => {
    expect(defaultDepth(VOLUME_43, 5)).toBe(2);
  });

  it('том мельче порога целиком раскрывается до предела', () => {
    expect(defaultDepth(FLAT)).toBe(1);
    expect(defaultDepth(VOLUME_43, 100)).toBe(3);
  });

  it('на пустом дереве — единица, а не ноль: глубина 0 бессмысленна', () => {
    expect(defaultDepth([])).toBe(1);
  });
});

describe('depthFromParam', () => {
  it('число в диапазоне проходит как есть', () => {
    expect(depthFromParam(outlineMetrics(VOLUME_43), '2')).toBe(2);
  });

  it('параметра нет — глубина по умолчанию', () => {
    expect(depthFromParam(outlineMetrics(VOLUME_43), null)).toBe(3);
  });

  it.each(['0', '-1', '4', 'три', '', '2.5'])(
    'мусор «%s» молча откатывается на глубину по умолчанию',
    (param) => {
      expect(depthFromParam(outlineMetrics(VOLUME_43), param)).toBe(3);
    },
  );
});

describe('worksLevel', () => {
  it('в томе под обёрткой работы лежат вторым уровнем', () => {
    expect(worksLevel(VOLUME_43)).toHaveLength(9);
  });

  it('на плоском томе работы и есть верхний уровень', () => {
    expect(worksLevel(FLAT)).toHaveLength(3);
  });

  it('если больше одного узла нет нигде, берётся верхний уровень', () => {
    const chain = [chapter(1, 'Одна', [chapter(2, 'Одна вложенная')])];
    expect(worksLevel(chain).map((c) => c.id)).toEqual([1]);
  });

  it('на пустом дереве — пусто', () => {
    expect(worksLevel([])).toEqual([]);
  });
});

function state(expanded: number[] = [], collapsed: number[] = []): OutlineState {
  return { expanded: new Set(expanded), collapsed: new Set(collapsed) };
}

/** Плоские id видимого дерева в порядке чтения — так проверять читаемее. */
function ids(nodes: OutlineNode[]): number[] {
  return nodes.flatMap((node) => [node.chapter.id, ...ids(node.children)]);
}

function find(nodes: OutlineNode[], id: number): OutlineNode {
  for (const node of nodes) {
    if (node.chapter.id === id) return node;
    const found = node.children.length > 0 ? findOrNull(node.children, id) : null;
    if (found) return found;
  }
  throw new Error(`узла ${id} нет в видимом дереве`);
}

function findOrNull(nodes: OutlineNode[], id: number): OutlineNode | null {
  for (const node of nodes) {
    if (node.chapter.id === id) return node;
    const found = findOrNull(node.children, id);
    if (found) return found;
  }
  return null;
}

describe('outlineTree', () => {
  it('глубина 1 показывает только верхний уровень', () => {
    expect(ids(outlineTree(VOLUME_43, 1, state()))).toEqual([1875]);
  });

  it('глубина 2 показывает верхний уровень и его детей', () => {
    const visible = ids(outlineTree(VOLUME_43, 2, state()));
    expect(visible).toHaveLength(10);
    expect(visible.slice(0, 3)).toEqual([1875, 1876, 1877]);
  });

  it('глубина 3 показывает всё дерево тома 43', () => {
    expect(ids(outlineTree(VOLUME_43, 3, state()))).toHaveLength(13);
  });

  it('узел в expanded раскрывается глубже базовой глубины', () => {
    const visible = ids(outlineTree(VOLUME_43, 2, state([1876])));
    expect(visible).toContain(1885);
    expect(visible).toHaveLength(13);
  });

  it('узел в collapsed прячет детей внутри базовой глубины', () => {
    const visible = ids(outlineTree(VOLUME_43, 3, state([], [1876])));
    expect(visible).not.toContain(1885);
    expect(visible).toHaveLength(10);
  });

  it('свёрнутый узел прячет всё поддерево, а не один уровень', () => {
    const visible = ids(outlineTree(VOLUME_43, 3, state([], [1875])));
    expect(visible).toEqual([1875]);
  });

  it('hasChildren отражает данные, isOpen — показ', () => {
    const [root] = outlineTree(VOLUME_43, 1, state());
    expect(root.hasChildren).toBe(true);
    expect(root.isOpen).toBe(false);
    expect(root.level).toBe(1);
  });

  it('у листа детей нет ни в данных, ни в показе', () => {
    const leaf = find(outlineTree(VOLUME_43, 2, state()), 1881);
    expect(leaf.hasChildren).toBe(false);
    expect(leaf.isOpen).toBe(false);
    expect(leaf.children).toEqual([]);
  });
});

describe('countNodes', () => {
  it('считает все видимые строки, а не только верхние', () => {
    expect(countNodes(outlineTree(VOLUME_43, 3, state()))).toBe(13);
    expect(countNodes(outlineTree(VOLUME_43, 1, state()))).toBe(1);
  });
});

describe('toggleNode', () => {
  it('раскрытый внутри базы уходит в collapsed', () => {
    const tree = outlineTree(VOLUME_43, 3, state());
    const next = toggleNode(find(tree, 1876), 3, state());
    expect([...next.collapsed]).toEqual([1876]);
    expect([...next.expanded]).toEqual([]);
  });

  it('свёрнутый глубже базы уходит в expanded', () => {
    const tree = outlineTree(VOLUME_43, 2, state());
    const next = toggleNode(find(tree, 1876), 2, state());
    expect([...next.expanded]).toEqual([1876]);
    expect([...next.collapsed]).toEqual([]);
  });

  it('повторное переключение возвращает исходное состояние', () => {
    const before = state();
    const tree = outlineTree(VOLUME_43, 3, before);
    const opened = toggleNode(find(tree, 1876), 3, before);
    const reopened = outlineTree(VOLUME_43, 3, opened);
    const back = toggleNode(find(reopened, 1876), 3, opened);
    expect([...back.collapsed]).toEqual([]);
    expect([...back.expanded]).toEqual([]);
  });

  it('не трогает исходные множества: состояние заменяется, а не правится', () => {
    const before = state();
    const tree = outlineTree(VOLUME_43, 3, before);
    toggleNode(find(tree, 1876), 3, before);
    expect(before.collapsed.size).toBe(0);
  });
});

describe('searchTree', () => {
  it('находит главу третьего уровня и отдаёт её путь', () => {
    const hits = searchTree(VOLUME_43, 'письмо второе');
    expect(hits).toHaveLength(1);
    expect(hits[0].chapter.id).toBe(1886);
    expect(hits[0].path.map((c) => c.id)).toEqual([1875, 1876]);
  });

  it('ищет без учёта регистра и по подстроке', () => {
    expect(searchTree(VOLUME_43, 'ИБСЕН').map((h) => h.chapter.id)).toEqual([1881]);
  });

  it('на пустом запросе не находит ничего', () => {
    expect(searchTree(VOLUME_43, '   ')).toEqual([]);
  });

  it('порядок — обход дерева в порядке чтения', () => {
    expect(searchTree(VOLUME_43, 'письмо').map((h) => h.chapter.id)).toEqual([1885, 1886, 1887]);
  });
});

describe('filterOutlineTree', () => {
  // Автор стоит на детях, а не на корне — тот случай, для которого фасет и
  // заводился (том 6: пять корней с одним общим автором, тридцать четыре
  // ребёнка с тремя разными).
  const tree: OutlineNode[] = [
    {
      chapter: chapter(60, 'Разное'),
      level: 1,
      hasChildren: true,
      isOpen: true,
      children: [
        {
          chapter: chapter(61, 'К. Маркс. О Прудоне'),
          level: 2,
          hasChildren: false,
          isOpen: false,
          children: [],
        },
        {
          chapter: chapter(62, 'Ф. Энгельс. Барин Тидман'),
          level: 2,
          hasChildren: false,
          isOpen: false,
          children: [],
        },
      ],
    },
  ];

  const isEngels = (c: Chapter) => c.title.startsWith('Ф. Энгельс');

  it('узел без своего совпадения, но с совпавшим потомком, остаётся — с отфильтрованными детьми', () => {
    const filtered = filterOutlineTree(tree, isEngels);
    expect(filtered.map((n) => n.chapter.id)).toEqual([60]);
    expect(filtered[0].children.map((n) => n.chapter.id)).toEqual([62]);
  });

  it('узел, совпавший сам, остаётся целиком со всеми детьми — дальше не фильтруется', () => {
    const isRoot = (c: Chapter) => c.id === 60;
    const filtered = filterOutlineTree(tree, isRoot);
    expect(filtered[0].children.map((n) => n.chapter.id)).toEqual([61, 62]);
  });

  it('ничего не совпало нигде в поддереве — узел убирается целиком', () => {
    expect(filterOutlineTree(tree, (c) => c.id === 999)).toEqual([]);
  });

  it('несколько корней: остаётся только та ветка, где нашлось совпадение', () => {
    const forest: OutlineNode[] = [
      tree[0],
      {
        chapter: chapter(70, 'Ещё раздел'),
        level: 1,
        hasChildren: true,
        isOpen: true,
        children: [
          {
            chapter: chapter(71, 'Г. В. Плеханов. Письмо'),
            level: 2,
            hasChildren: false,
            isOpen: false,
            children: [],
          },
        ],
      },
    ];
    expect(filterOutlineTree(forest, isEngels).map((n) => n.chapter.id)).toEqual([60]);
  });
});

describe('depthSegments', () => {
  it('на мелком томе перечисляет все его уровни', () => {
    expect(depthSegments(outlineMetrics(VOLUME_43), 1)).toEqual([1, 2, 3]);
  });

  it('на глубоком томе обрезается на четырёх и добавляет предел', () => {
    const deep = [
      chapter(1, 'A', [
        chapter(2, 'B', [chapter(3, 'C', [chapter(4, 'D', [chapter(5, 'E', [chapter(6, 'F')])])])]),
      ]),
    ];
    expect(depthSegments(outlineMetrics(deep), 1)).toEqual([1, 2, 3, 4, 6]);
  });

  it('текущая глубина всегда среди сегментов, даже если выпала за четвёрку', () => {
    const deep = [
      chapter(1, 'A', [
        chapter(2, 'B', [chapter(3, 'C', [chapter(4, 'D', [chapter(5, 'E', [chapter(6, 'F')])])])]),
      ]),
    ];
    expect(depthSegments(outlineMetrics(deep), 5)).toEqual([1, 2, 3, 4, 5, 6]);
  });

  it('на томе из одного уровня управлять нечем', () => {
    expect(depthSegments(outlineMetrics(FLAT), 1)).toEqual([]);
  });

  it('на пустом дереве управлять нечем', () => {
    expect(depthSegments(outlineMetrics([]), 1)).toEqual([]);
  });

  it('умолчание тома среди сегментов, даже когда выпало за четвёрку', () => {
    const metrics = outlineMetrics(DEEP_DEFAULT);
    expect(metrics.fallback).toBe(6);
    expect(metrics.max).toBe(7);
    // Без умолчания было бы [1, 2, 3, 4, 7]: уйдя на «1», вернуться к тому
    // виду, с которого том открылся, можно было бы только кнопкой «назад».
    expect(depthSegments(metrics, 1)).toEqual([1, 2, 3, 4, 6, 7]);
  });
});

describe('emptyOutlineState', () => {
  /*
   * Умолчание было ОДНИМ общим объектом с изменяемыми множествами. Сейчас
   * `toggleNode` их копирует, так что беды не случалось, — но защита держалась
   * на дисциплине вызывающих. `Object.freeze` тут бессилен: данные Set лежат
   * во внутренних слотах, и `Object.freeze(new Set()).add(1)` проходит.
   * Поэтому не замораживаем, а не делимся.
   */
  it('каждый вызов отдаёт свою пару множеств', () => {
    const first = emptyOutlineState();
    const second = emptyOutlineState();
    expect(first).not.toBe(second);
    expect(first.expanded).not.toBe(second.expanded);
    expect(first.collapsed).not.toBe(second.collapsed);
  });

  it('правка одного не задевает следующий', () => {
    emptyOutlineState().expanded.add(1);
    expect(emptyOutlineState().expanded.size).toBe(0);
  });
});
