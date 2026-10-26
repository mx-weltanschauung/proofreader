import { Link } from 'react-router-dom';
import type { EventOrganizationSummary } from '../types';
import { organizationPath } from '../utils/paths';
import { plural } from '../utils/volumeLabel';
import { organizationAnchor } from '../utils/homeLayout';
import './OrganizationShelf.css';

/**
 * Организации событий. На главной блоков бывает несколько — свой в каждом
 * разделе и общий после разделов, — поэтому якорь блока (`id`) и уровень
 * заголовка задаёт страница; заголовок связан с блоком по якорю.
 */
export function OrganizationShelf({
  organizations,
  id = 'sezdy',
  headingLevel = 'h2',
}: {
  organizations: EventOrganizationSummary[];
  id?: string;
  headingLevel?: 'h2' | 'h3';
}) {
  if (organizations.length === 0) return null;
  const Heading = headingLevel;
  return (
    <section className="shelf-section org-shelf" id={id} aria-labelledby={`${id}-heading`}>
      <Heading id={`${id}-heading`} className="org-shelf-heading">
        Съезды и конгрессы
      </Heading>
      <ul className="org-shelf-list">
        {organizations.map((o) => (
          <li key={o.id} id={organizationAnchor(o)}>
            <Link to={organizationPath(o)} className="org-spine">
              <span className="org-spine-title">{o.title}</span>
              <span className="org-spine-meta">
                {plural(o.events_total, ['событие', 'события', 'событий'])}
              </span>
            </Link>
          </li>
        ))}
      </ul>
    </section>
  );
}
