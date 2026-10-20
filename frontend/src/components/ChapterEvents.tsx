import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { eventsApi } from '../services/api';
import type { ChapterEventLink } from '../types';
import { eventPath } from '../utils/paths';
import './ChapterEvents.css';

/** «Входит в: …» — события, в повестке которых стоит глава. Сбой и пусто — ничего. */
export function ChapterEvents({ workId, chapterId }: { workId: number; chapterId: number }) {
  const [loaded, setLoaded] = useState<{ key: string; links: ChapterEventLink[] } | null>(null);
  const key = `${workId}/${chapterId}`;
  useEffect(() => {
    let alive = true;
    eventsApi
      .chapterEvents(workId, chapterId)
      .then(({ data }) => {
        if (alive) setLoaded({ key, links: data ?? [] });
      })
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [workId, chapterId, key]);
  const links = loaded?.key === key ? loaded.links : [];
  if (links.length === 0) return null;
  return (
    <p className="event-chapter-links">
      Входит в:{' '}
      {links.map((l, i) => (
        <span key={`${l.event_slug}-${l.section_position}`}>
          {i > 0 && ' · '}
          <Link
            to={`${eventPath(l.organization_slug, l.event_slug)}#section-${l.section_position + 1}`}
          >
            {`${l.event_title} — ${l.section_title}`}
          </Link>
        </span>
      ))}
    </p>
  );
}
