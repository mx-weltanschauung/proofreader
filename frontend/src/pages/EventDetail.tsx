import { useEffect, useState } from 'react';
import { Link, useLocation, useParams } from 'react-router-dom';
import { eventsApi } from '../services/api';
import type { EventDetail as Detail, EventTarget } from '../types';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { apiErrorMessage } from '../utils/apiError';
import { chapterPath, eventEditPath, organizationPath, workPath } from '../utils/paths';
import { jumpToAnchor } from '../utils/anchorNav';
import { useAuth, canEdit } from '../hooks/useAuth';
import { ClosedBadge } from '../components/ClosedBadge';
import './EventDetail.css';

function targetPath(t: EventTarget): string {
  const work = { id: t.work_id as number };
  return t.kind === 'chapter' && t.chapter_id != null
    ? chapterPath(work, { id: t.chapter_id })
    : workPath(work);
}

/** Битая цель — текст без ссылки (её видит только сотрудник). */
function BrokenTarget({ t }: { t: EventTarget }) {
  return (
    <span className="event-target-broken">
      {t.kind === 'chapter' ? 'глава не найдена' : 'том не найден'}: {t.title}
    </span>
  );
}

function SingleTarget({ label, t }: { label: string; t: EventTarget }) {
  if (t.broken) {
    return (
      <>
        {label} · <BrokenTarget t={t} />
      </>
    );
  }
  return (
    <>
      <Link to={targetPath(t)}>{label}</Link>
      {t.closed && <ClosedBadge />}
      {t.label && <small className="event-target-label"> {t.label}</small>}
    </>
  );
}

export function EventDetail() {
  const { slug = '', eventSlug = '' } = useParams<{ slug: string; eventSlug: string }>();
  const [event, setEvent] = useState<Detail | null>(null);
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const { user } = useAuth();
  const { hash } = useLocation();
  useDocumentTitle(event?.title);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      setIsLoading(true);
      setError('');
      try {
        const { data } = await eventsApi.event(slug, eventSlug);
        if (alive) setEvent(data);
      } catch (err: unknown) {
        if (alive) setError(apiErrorMessage(err, 'Событие не найдено'));
      } finally {
        if (alive) setIsLoading(false);
      }
    };
    void load();
    return () => {
      alive = false;
    };
  }, [slug, eventSlug]);

  // Пункты появляются после ответа API, и браузер к якорю из адреса уже не
  // вернётся: прыгаем сами, когда заголовки встали в документ.
  useEffect(() => {
    if (event && hash) jumpToAnchor(decodeURIComponent(hash.slice(1)));
  }, [event, hash]);

  if (isLoading) return <div className="event-state">Загрузка…</div>;
  if (error || !event) {
    return (
      <div className="event-state event-state-error">
        <div>{error || 'Событие не найдено'}</div>
        <Link to="/" className="event-back-link">
          ← В читальню
        </Link>
      </div>
    );
  }
  const where = [event.dates, event.place].filter(Boolean).join(', ');
  return (
    <div className="event-page">
      <Link to={organizationPath(event.organization)} className="event-back-link">
        ← {event.organization.title}
      </Link>
      <header className="event-header">
        <h1 className="event-title">{event.title}</h1>
        {canEdit(user) && (
          <Link to={eventEditPath(event.organization.slug, event.slug)} className="event-edit-link">
            Править
          </Link>
        )}
        {where && <p className="event-where">{where}</p>}
        {event.description && <p className="event-description">{event.description}</p>}
      </header>
      {event.sections.map((section) => (
        <section key={section.position} className="event-section">
          <h2 id={`section-${section.position + 1}`} tabIndex={-1} className="event-section-title">
            {section.title}
          </h2>
          <ul className="event-entries">
            {section.entries.map((entry, i) => (
              <li key={i} className="event-entry">
                {entry.targets.length === 1 ? (
                  <SingleTarget label={entry.label} t={entry.targets[0]} />
                ) : (
                  <>
                    {entry.label}
                    {entry.targets.length > 0 && ': '}
                    {entry.targets.map((t, j) => (
                      <span key={j}>
                        {j > 0 && ' · '}
                        {t.broken ? (
                          <BrokenTarget t={t} />
                        ) : (
                          <>
                            <Link to={targetPath(t)}>{t.label || t.title}</Link>
                            {t.closed && <ClosedBadge />}
                          </>
                        )}
                      </span>
                    ))}
                  </>
                )}
              </li>
            ))}
          </ul>
        </section>
      ))}
    </div>
  );
}
