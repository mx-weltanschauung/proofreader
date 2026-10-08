import { useState } from 'react';
import { Link } from 'react-router-dom';
import type { PageMapEntry, PageStatus, Work } from '../types';
import type { OutlineNode } from '../utils/volumeOutline';
import { readinessLabel, readinessSegments } from '../utils/pageReadiness';
import { splitAuthor } from '../utils/chapterAuthor';
import { PageCells } from './PageCells';
import { chapterPath, readPath } from '../utils/paths';
import './VolumeOutlineRow.css';

interface Props {
  node: OutlineNode;
  work: Work;
  pagesInRange: (start: number, end: number) => PageMapEntry[];
  editable: boolean;
  highlight: PageStatus | null;
  /** Колонка автора рисуется на весь уровень или ни у кого: иначе правый край пляшет. */
  showAuthors: boolean;
  onToggle: (node: OutlineNode) => void;
}

/**
 * Отступ уровня. До четвёртого — полный, глубже сжимается: в томе 3 МиЭ дерево
 * доходит до одиннадцати уровней, и по 15px на каждый съело бы 150px слева от
 * заглавия. Со сжатием — 115px.
 */
function indent(level: number): number {
  return (Math.min(level, 4) - 1) * 15 + Math.max(0, level - 4) * 10;
}

export const VolumeOutlineRow: React.FC<Props> = ({
  node,
  work,
  pagesInRange,
  editable,
  highlight,
  showAuthors,
  onToggle,
}) => {
  const [cellsOpen, setCellsOpen] = useState(false);

  const { chapter } = node;
  const { author, title } = splitAuthor(chapter.title);
  const pages = pagesInRange(chapter.start_page, chapter.end_page);
  const segments = readinessSegments(pages);

  // Заголовок — только на двух верхних уровнях: там лежат работы тома при
  // обеих его формах (в томе с обёрткой — вторым уровнем, в плоском —
  // первым). Экран несёт <h2> «Содержание», поэтому счёт идёт с h3. Глубже
  // заголовок ничего не добавляет: структуру несут вложенные <ul>. Правка
  // чинит глубину, а не ширину — широкий плоский уровень (в 38 томах из 50,
  // включая том 15 МиЭ) как был сотней соседних h3, так и остаётся; это
  // осознанно. Граница та же, на которой заглавие уже меняет гарнитуру
  // (`is-deep`).
  const Title = node.level === 1 ? 'h3' : node.level === 2 ? 'h4' : 'p';

  return (
    <li className="vol-toc-item">
      <div className="vol-toc-row" style={{ paddingLeft: `${indent(node.level)}px` }}>
        <span className="vol-toc-tri">
          {node.hasChildren ? (
            <button
              type="button"
              className="vol-toc-tri-btn"
              aria-expanded={node.isOpen}
              // Имя с заглавием: на уровне таких кнопок десятки, и в списке
              // ссылок скринридера «Развернуть» без уточнения неразличимы.
              aria-label={`${node.isOpen ? 'Свернуть' : 'Развернуть'}: ${title}`}
              onClick={() => onToggle(node)}
            >
              <span aria-hidden="true">{node.isOpen ? '▾' : '▸'}</span>
            </button>
          ) : null}
        </span>

        <Title className={`vol-toc-title${node.level >= 3 ? ' is-deep' : ''}`}>
          <Link to={chapterPath(work, chapter)}>{title}</Link>
        </Title>

        {/* Обёртка держит перенос на узком экране: на широком у неё
            display: contents, и четыре её элемента остаются колонками самой
            строки; на телефоне она становится своей flex-строкой под
            заглавием. Без неё заглавию при 375px достаётся 45px из 345. */}
        <span className="vol-toc-meta">
          {showAuthors && <span className="vol-toc-author">{author ?? ''}</span>}

          <span className="vol-toc-act">
            <Link
              to={readPath(work, chapter.start_page)}
              className="vol-toc-read-link"
              // Имя с заглавием: на уровне таких ссылок десятки, и одно
              // «потоком» в списке ссылок скринридера ничего не различает.
              aria-label={`Читать потоком: ${title}`}
            >
              потоком
            </Link>
            {pages.length > 0 && (
              <>
                <span className="vol-toc-act-sep" aria-hidden="true">
                  ·
                </span>
                <button
                  type="button"
                  className="vol-toc-cells-btn"
                  aria-expanded={cellsOpen}
                  aria-label={`Постранично: ${title}`}
                  onClick={() => setCellsOpen((open) => !open)}
                >
                  постранично
                </button>
              </>
            )}
          </span>

          <span className="vol-toc-pages">
            {chapter.start_page}—{chapter.end_page}
          </span>

          {/* Место под полоску занято всегда, даже когда сегментов нет: иначе
              правый край плясал бы от строки к строке. */}
          <span className="vol-toc-ready">
            {segments.length > 0 && (
              <span className="vol-toc-ready-bar" role="img" aria-label={readinessLabel(pages)}>
                {segments.map((segment) => (
                  <i
                    key={segment.status}
                    className={`vol-toc-ready-part is-${segment.modifier}`}
                    style={{ width: `${segment.share * 100}%` }}
                  />
                ))}
              </span>
            )}
          </span>
        </span>
      </div>

      {cellsOpen && (
        <div className="vol-toc-cells" style={{ paddingLeft: `${indent(node.level) + 12}px` }}>
          <PageCells work={work} pages={pages} editable={editable} highlight={highlight} />
        </div>
      )}

      {node.children.length > 0 && (
        <ul className="vol-toc-list">
          {node.children.map((child) => (
            <VolumeOutlineRow
              key={child.chapter.id}
              node={child}
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
    </li>
  );
};
