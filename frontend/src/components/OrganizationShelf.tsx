import { Link } from 'react-router-dom';
import type { EventOrganizationSummary } from '../types';
import { organizationPath } from '../utils/paths';
import { plural } from '../utils/volumeLabel';
import './OrganizationShelf.css';

export function OrganizationShelf({
  organizations,
}: {
  organizations: EventOrganizationSummary[];
}) {
  if (organizations.length === 0) return null;
  return (
    <section className="shelf-section org-shelf" aria-labelledby="org-shelf-heading">
      <h2 id="org-shelf-heading" className="org-shelf-heading">
        Съезды и конгрессы
      </h2>
      <ul className="org-shelf-list">
        {organizations.map((o) => (
          <li key={o.id}>
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
