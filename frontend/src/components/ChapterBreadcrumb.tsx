import { Fragment, useState } from 'react';
import { Link } from 'react-router-dom';
import type { Chapter } from '../types';
import { chapterPath } from '../utils/paths';
import './ChapterBreadcrumb.css';

interface ChapterBreadcrumbProps {
  /** Минимум для построения адреса — не обязательно полный Work. */
  work: { id: number; slug?: string } | undefined;
  /** Уровни от внешнего к внутреннему; внутри уровня — сестринские главы. */
  levels: Chapter[][];
  className?: string;
}

// Глубже четырёх уровней цепочка перестаёт быть строкой и начинает быть
// абзацем: в томе 3 доходит до одиннадцати уровней и 482 символов. Порог
// именно 4, а не меньше: при пяти уровнях «…» прячет два, при четырёх спрятало
// бы один — и заняло бы столько же места.
const MAX_LEVELS_BEFORE_COLLAPSE = 4;
// Сколько последних уровней остаётся видно рядом с первым.
const VISIBLE_TAIL_LEVELS = 2;

// Только отрисовка: что показывать, решает useChaptersForPage.
export const ChapterBreadcrumb: React.FC<ChapterBreadcrumbProps> = ({
  work,
  levels,
  className,
}) => {
  const [isExpanded, setIsExpanded] = useState(false);
  const [prevLevels, setPrevLevels] = useState(levels);

  // Переход на соседнюю страницу приносит новый массив уровней — цепочка
  // должна вернуться к свёрнутому виду, а не унаследовать раскрытие с прошлой.
  if (prevLevels !== levels) {
    setPrevLevels(levels);
    setIsExpanded(false);
  }

  if (!work || levels.length === 0) return null;

  const isCollapsed = !isExpanded && levels.length > MAX_LEVELS_BEFORE_COLLAPSE;
  const hiddenCount = levels.length - VISIBLE_TAIL_LEVELS - 1;
  const shownLevels = isCollapsed
    ? [levels[0], ...levels.slice(levels.length - VISIBLE_TAIL_LEVELS)]
    : levels;
  // Кнопка встаёт на место середины, то есть сразу после первого уровня.
  const ellipsisAfterIndex = 0;

  return (
    <span className={className ? `chapter-breadcrumb ${className}` : 'chapter-breadcrumb'}>
      {shownLevels.map((level, levelIndex) => (
        // Уровни плотные и непустые, так что id первой главы — стабильный ключ.
        <Fragment key={level[0].id}>
          {levelIndex > 0 && (
            <span className="chapter-breadcrumb-separator" aria-hidden="true">
              {' / '}
            </span>
          )}
          {level.map((chapter, siblingIndex) => (
            <Fragment key={chapter.id}>
              {siblingIndex > 0 && (
                <span className="chapter-breadcrumb-separator" aria-hidden="true">
                  {' · '}
                </span>
              )}
              <Link to={chapterPath(work, chapter)} className="chapter-breadcrumb-link">
                {chapter.title}
              </Link>
            </Fragment>
          ))}
          {isCollapsed && levelIndex === ellipsisAfterIndex && (
            <>
              <span className="chapter-breadcrumb-separator" aria-hidden="true">
                {' / '}
              </span>
              <button
                type="button"
                className="chapter-breadcrumb-expand"
                aria-label={`Показать пропущенные уровни (${hiddenCount})`}
                onClick={() => setIsExpanded(true)}
              >
                …
              </button>
            </>
          )}
        </Fragment>
      ))}
    </span>
  );
};
