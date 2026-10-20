import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { eventsApi } from '../services/api';
import type { EventOrganizationDetail } from '../types';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { apiErrorMessage } from '../utils/apiError';
import { eventPath, organizationEditPath } from '../utils/paths';
import { plural } from '../utils/volumeLabel';
import { useAuth, canEdit } from '../hooks/useAuth';
import './OrganizationDetail.css';

/**
 * Пометка сотруднику. Видна она только у события без единой небитой цели и у
 * вида, где таких событий не осталось: `materials_total` сотруднику считает
 * все небитые цели, закрытые тоже, — событие, все материалы которого закрыты,
 * отсюда не распознать, и пометки у него нет.
 */
function HiddenMark() {
  return <span className="org-hidden-mark">читателям не видно</span>;
}

export function OrganizationDetail() {
  const { slug = '' } = useParams<{ slug: string }>();
  const [org, setOrg] = useState<EventOrganizationDetail | null>(null);
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const { user } = useAuth();
  useDocumentTitle(org?.title);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      setIsLoading(true);
      setError('');
      try {
        const { data } = await eventsApi.organization(slug);
        if (alive) setOrg(data);
      } catch (err: unknown) {
        if (alive) setError(apiErrorMessage(err, 'Организация не найдена'));
      } finally {
        if (alive) setIsLoading(false);
      }
    };
    void load();
    return () => {
      alive = false;
    };
  }, [slug]);

  const staff = canEdit(user);
  if (isLoading) return <div className="org-state">Загрузка…</div>;
  if (error || !org) {
    return (
      <div className="org-state org-state-error">
        <div>{error || 'Организация не найдена'}</div>
        <Link to="/" className="org-back-link">
          ← В читальню
        </Link>
      </div>
    );
  }
  return (
    <div className="org-page">
      <Link to="/" className="org-back-link">
        ← В читальню
      </Link>
      <header className="org-header">
        <h1 className="org-title">{org.title}</h1>
        {staff && (
          <Link to={organizationEditPath(org.slug)} className="org-edit-link">
            Править
          </Link>
        )}
        {org.description && <p className="org-description">{org.description}</p>}
      </header>
      {org.kinds.map((kind) => (
        <section key={kind.id} className="org-kind">
          <h2 className="org-kind-title">{kind.title}</h2>
          {staff && kind.events.every((ev) => ev.materials_total === 0) && <HiddenMark />}
          <ul className="org-events">
            {kind.events.map((ev) => (
              <li key={ev.id} className="org-event">
                <Link to={eventPath(org.slug, ev.slug)} className="org-event-link">
                  {ev.title}
                </Link>
                {ev.dates && <span className="org-event-meta">{ev.dates}</span>}
                <span className="org-event-meta">
                  {plural(ev.materials_total, ['материал', 'материала', 'материалов'])}
                </span>
                {staff && ev.materials_total === 0 && <HiddenMark />}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
