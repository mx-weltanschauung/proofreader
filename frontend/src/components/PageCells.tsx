import { Link } from 'react-router-dom';
import type { PageMapEntry, PageStatus, Work } from '../types';
import { PAGE_STATUS_MODIFIER } from '../utils/pageStatus';
import { pageCellLabel } from '../utils/pageCellLabel';
import { pagePath } from '../utils/paths';
import './PageCells.css';

interface Props {
  work: Work;
  /** Страницы одного диапазона, уже отобранные вызывающим. */
  pages: PageMapEntry[];
  editable: boolean;
  /** Подсвеченный статус: клетки прочих статусов гаснут. */
  highlight?: PageStatus | null;
}

export const PageCells: React.FC<Props> = ({ work, pages, editable, highlight = null }) => {
  if (pages.length === 0) return null;

  return (
    <div className="vol-cells">
      {pages.map((entry) => {
        const label = pageCellLabel(work, entry);
        const dimmed = highlight !== null && entry.status !== highlight;
        return (
          <Link
            key={entry.page_number}
            to={`${pagePath(work, entry.page_number)}${editable ? '/edit' : ''}`}
            className={`vol-cell is-${PAGE_STATUS_MODIFIER[entry.status]}${dimmed ? ' is-dimmed' : ''}`}
            aria-label={label}
            title={label}
          />
        );
      })}
    </div>
  );
};
