import { useCallback, useMemo, useState } from 'react';
import { useSearchParams } from 'react-router-dom';
import type { Chapter, PageMapEntry, PageStatus, Work } from '../types';
import {
  countNodes,
  depthFromParam,
  emptyOutlineState,
  filterOutlineTree,
  outlineMetrics,
  outlineTree,
  searchTree,
  toggleNode,
  type OutlineNode,
  type OutlineState,
} from '../utils/volumeOutline';
import { VolumeOutlineRow } from './VolumeOutlineRow';
import { VolumeDepthControl } from './VolumeDepthControl';
import { PageCells } from './PageCells';
import { splitAuthor } from '../utils/chapterAuthor';
import { uncoveredRanges } from '../utils/pageCoverage';
import { plural } from '../utils/volumeLabel';
import { useMeasuredHeight } from '../utils/useMeasuredHeight';
import './VolumeOutline.css';

interface Props {
  work: Work;
  /** Главы верхнего уровня со вложенными детьми. */
  chapters: Chapter[];
  pages: PageMapEntry[];
  editable: boolean;
  highlight: PageStatus | null;
}

export const VolumeOutline: React.FC<Props> = ({ work, chapters, pages, editable, highlight }) => {
  const [query, setQuery] = useState('');
  const [author, setAuthor] = useState<string | null>(null);
  const [state, setState] = useState<OutlineState>(emptyOutlineState);
  const [searchParams, setSearchParams] = useSearchParams();

  // Мерки тома считаются один раз: от них производны и глубина, и предел, и
  // набор сегментов, и общее число строк.
  const metrics = useMemo(() => outlineMetrics(chapters), [chapters]);

  const depth = useMemo(
    () => depthFromParam(metrics, searchParams.get('depth')),
    [metrics, searchParams],
  );

  // Глубина живёт в адресе, поэтому меняться она может и кнопкой «назад».
  // Сброс ручных правок и фасета вешается на смену значения, а не на
  // обработчик нажатия: иначе «назад» оставлял бы раскрытия от другой глубины.
  const [prevDepth, setPrevDepth] = useState(depth);
  if (prevDepth !== depth) {
    setPrevDepth(depth);
    setState(emptyOutlineState());
    setAuthor(null);
  }

  const searching = query.trim() !== '';

  const tree = useMemo(() => outlineTree(chapters, depth, state), [chapters, depth, state]);
  const total = metrics.total;

  const hits = useMemo(
    () => (searching ? searchTree(chapters, query) : []),
    [chapters, query, searching],
  );

  // Автор считается по тому, что показано: на уровне — по видимым строкам, в
  // поиске — по найденному.
  const shownChapters = useMemo(
    () => (searching ? hits.map((hit) => hit.chapter) : flattenNodes(tree)),
    [hits, searching, tree],
  );

  const authors = useMemo(() => {
    const found = new Set<string>();
    for (const chapter of shownChapters) {
      const name = splitAuthor(chapter.title).author;
      if (name) found.add(name);
    }
    return [...found];
  }, [shownChapters]);

  const showAuthors = authors.length > 0;
  const showFacets = authors.length >= 2;

  const matchesAuthor = useCallback(
    (chapter: Chapter) => !author || splitAuthor(chapter.title).author === author,
    [author],
  );

  // Рекурсивно: фасет может совпасть глубже корня (в томе 6 — на втором
  // уровне), и резать по корню либо прячет совпавшего потомка вместе с
  // непричастным родителем, либо тащит наверх чужих детей заодно с автором,
  // у которого совпал только сам родитель.
  const visibleTree = useMemo(
    () => (author ? filterOutlineTree(tree, matchesAuthor) : tree),
    [author, matchesAuthor, tree],
  );

  // Считает показанное: до фасета — всё дерево, после — то, что от него
  // осталось. `hits`/`visibleHits` уже показанное само по себе — своей
  // отдельной переменной под поиск подсчёт не нужен.
  const shown = useMemo(() => countNodes(visibleTree), [visibleTree]);

  const visibleHits = useMemo(
    () => hits.filter((hit) => matchesAuthor(hit.chapter)),
    [hits, matchesAuthor],
  );

  const pagesInRange = useMemo(() => {
    const byNumber = new Map<number, PageMapEntry>();
    for (const page of pages) byNumber.set(page.page_number, page);
    return (start: number, end: number) => {
      const result: PageMapEntry[] = [];
      for (let n = start; n <= end; n += 1) {
        const page = byNumber.get(n);
        if (page) result.push(page);
      }
      return result;
    };
  }, [pages]);

  // Страницы вне оглавления считаются по настоящим верхним главам тома: это
  // свойство тома, а не текущего вида.
  const outside = useMemo(() => uncoveredRanges(pages, chapters), [pages, chapters]);

  const pickDepth = (value: number) => {
    // Тот же сегмент — историю не трогаем: шаг был бы пустым, а «назад»
    // отматывал бы его, ничего не меняя на экране.
    if (value === depth) return;

    const next = new URLSearchParams(searchParams);
    next.set('depth', String(value));
    // Push, а не replace: «назад» должно возвращать прежнюю глубину.
    setSearchParams(next);
  };

  const onToggle = (node: OutlineNode) => setState((prev) => toggleNode(node, depth, prev));

  const reset = () => {
    setQuery('');
    setAuthor(null);
  };

  const empty = searching ? visibleHits.length === 0 : visibleTree.length === 0;

  // Сбрасывать можно только то, что задано. В томе со страницами, но без
  // глав, и запрос, и фасет пусты — кнопка «Сбросить» была мёртвой.
  const canReset = searching || author !== null;
  const emptyMessage = searching
    ? `По запросу «${query.trim()}» ничего нет`
    : canReset
      ? 'Работы не найдены'
      : 'Том ещё не расписан по главам';

  const barRef = useMeasuredHeight<HTMLDivElement>('data-toc-bar-height');

  return (
    <section className="vol-toc" aria-labelledby="vol-toc-title">
      <div className="vol-toc-bar" ref={barRef}>
        <h2 className="vol-toc-heading" id="vol-toc-title">
          Содержание
        </h2>

        <label className="vol-toc-search">
          <span className="vol-toc-search-label">Поиск по работам</span>
          <input
            type="search"
            value={query}
            onChange={(event) => setQuery(event.target.value)}
            placeholder="название работы"
          />
        </label>

        <VolumeDepthControl
          metrics={metrics}
          depth={depth}
          shown={searching ? visibleHits.length : shown}
          total={searching ? undefined : total}
          disabled={searching}
          onPick={pickDepth}
        />
      </div>

      {showFacets && (
        <div className="vol-toc-facets">
          {authors.map((name) => (
            <button
              key={name}
              type="button"
              className={`vol-toc-facet${author === name ? ' is-on' : ''}`}
              aria-pressed={author === name}
              onClick={() => setAuthor(author === name ? null : name)}
            >
              {name}
            </button>
          ))}
        </div>
      )}

      {empty ? (
        <div className="vol-toc-empty">
          <p>{emptyMessage}</p>
          {canReset && (
            <button type="button" onClick={reset}>
              Сбросить
            </button>
          )}
        </div>
      ) : searching ? (
        <ul className="vol-toc-list">
          {visibleHits.map((hit) => (
            <li key={hit.chapter.id} className="vol-toc-hit">
              {hit.path.length > 0 && (
                <p className="vol-toc-path">
                  {hit.path.map((ancestor) => splitAuthor(ancestor.title).title).join(' › ')}
                </p>
              )}
              <ul className="vol-toc-list">
                <VolumeOutlineRow
                  node={{
                    chapter: hit.chapter,
                    level: 1,
                    hasChildren: false,
                    isOpen: false,
                    children: [],
                  }}
                  work={work}
                  pagesInRange={pagesInRange}
                  editable={editable}
                  highlight={highlight}
                  showAuthors={showAuthors}
                  onToggle={() => {}}
                />
              </ul>
            </li>
          ))}
        </ul>
      ) : (
        <ul className="vol-toc-list">
          {visibleTree.map((node) => (
            <VolumeOutlineRow
              key={node.chapter.id}
              node={node}
              work={work}
              pagesInRange={pagesInRange}
              editable={editable}
              highlight={highlight}
              showAuthors={showAuthors}
              onToggle={onToggle}
            />
          ))}
        </ul>
      )}

      {!searching && outside.length > 0 && (
        <div className="vol-outside">
          <h2 className="vol-toc-heading">Вне оглавления</h2>
          <p className="vol-outside-hint">
            Титул, примечания, указатели — страницы, которых нет в содержании тома.
          </p>
          <ul className="vol-outside-list">
            {outside.map((range) => (
              <li key={range.start}>
                <p className="vol-toc-range">
                  {range.start}—{range.end} ·{' '}
                  {plural(range.end - range.start + 1, ['страница', 'страницы', 'страниц'])}
                </p>
                <PageCells
                  work={work}
                  pages={pagesInRange(range.start, range.end)}
                  editable={editable}
                  highlight={highlight}
                />
              </li>
            ))}
          </ul>
        </div>
      )}
    </section>
  );
};

/** Главы всех видимых узлов — для подсчёта авторов на показанном. */
function flattenNodes(nodes: OutlineNode[]): Chapter[] {
  return nodes.flatMap((node) => [node.chapter, ...flattenNodes(node.children)]);
}
