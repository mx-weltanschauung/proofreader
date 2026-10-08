import React from 'react';
import { Link } from 'react-router-dom';
import type { Neighbours } from '../utils/pageNeighbours';
import { pagePath } from '../utils/paths';
import './PagePager.css';

interface Props {
  /** Минимум для построения адреса — не обязательно полный Work. */
  work: { id: number; slug?: string };
  neighbours: Neighbours;
  /** Сверху — компактно в строку заголовка, снизу — подписанными ссылками. */
  variant: 'top' | 'bottom';
}

export const PagePager: React.FC<Props> = ({ work, neighbours, variant }) => {
  const { prev, next, index, total } = neighbours;
  if (total === 0) return null;

  const href = (page: number) => pagePath(work, page);

  if (variant === 'top') {
    // Для top-варианта своего правила нет — базовой .page-pager хватает,
    // добавлять модификатор без стиля незачем.
    return (
      <nav className="page-pager" aria-label="Переход по страницам">
        {prev !== null && (
          <Link to={href(prev)} className="btn btn-secondary btn-sm">
            ← {prev}
          </Link>
        )}
        <span className="page-pager-place">
          стр. {index} из {total}
        </span>
        {next !== null && (
          <Link to={href(next)} className="btn btn-secondary btn-sm">
            {next} →
          </Link>
        )}
      </nav>
    );
  }

  return (
    <nav className="page-pager page-pager-bottom" aria-label="Переход по страницам">
      {prev !== null ? (
        <Link to={href(prev)} className="page-pager-link">
          ← {prev}. Предыдущая
        </Link>
      ) : (
        <span />
      )}
      <span className="page-pager-hint" aria-hidden="true">
        ← → листают
      </span>
      {next !== null ? (
        <Link to={href(next)} className="page-pager-link">
          {next}. Следующая →
        </Link>
      ) : (
        <span />
      )}
    </nav>
  );
};
