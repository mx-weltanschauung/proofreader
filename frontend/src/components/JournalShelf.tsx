import { Link } from 'react-router-dom';
import type { JournalSummary } from '../types';
import { journalPath } from '../utils/paths';
import { plural } from '../utils/volumeLabel';
import './JournalShelf.css';

/** Годы загруженных номеров: «1922—1935», один год — без тире. */
function years(j: JournalSummary): string {
  if (!j.year_from) return '';
  return j.year_to && j.year_to !== j.year_from ? `${j.year_from}—${j.year_to}` : `${j.year_from}`;
}

export function JournalShelf({ journals }: { journals: JournalSummary[] }) {
  if (journals.length === 0) return null;
  return (
    <section className="shelf-section journal-shelf" aria-labelledby="journal-shelf-heading">
      <h2 id="journal-shelf-heading" className="journal-shelf-heading">
        Журналы
      </h2>
      <ul className="journal-shelf-list">
        {journals.map((j) => (
          <li key={j.id}>
            <Link to={journalPath(j)} className="journal-spine">
              <span className="journal-spine-title">{j.title}</span>
              <span className="journal-spine-meta">
                {[years(j), plural(j.issues_total, ['номер', 'номера', 'номеров'])]
                  .filter(Boolean)
                  .join(' · ')}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
