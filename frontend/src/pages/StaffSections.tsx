import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { canEdit, isAdmin, useAuth } from '../hooks/useAuth';
import { feedbackApi, readerCommentsApi, suggestionsApi } from '../services/api';
import { staffGroups } from '../utils/staffNavigation';
import './StaffSections.css';

function Count({ value }: { value: number | null }) {
  return value !== null && value > 0 ? <span className="staff-sections-count">{value}</span> : null;
}

export function StaffSections() {
  const { user } = useAuth();
  const staff = canEdit(user);
  const admin = isAdmin(user);
  const [suggestions, setSuggestions] = useState<number | null>(null);
  const [comments, setComments] = useState<number | null>(null);
  const [feedback, setFeedback] = useState<number | null>(null);

  useEffect(() => {
    if (!staff) return;
    let alive = true;
    void suggestionsApi
      .queue('новое', 0, 0)
      .then(({ data }) => {
        if (alive) setSuggestions(data.total);
      })
      .catch(() => {});
    void readerCommentsApi
      .pendingCount()
      .then(({ data }) => {
        if (alive) setComments(data.count);
      })
      .catch(() => {});
    if (admin) {
      void feedbackApi
        .unreadCount()
        .then(({ data }) => {
          if (alive) setFeedback(data.count);
        })
        .catch(() => {});
    }
    return () => {
      alive = false;
    };
  }, [staff, admin]);

  if (!staff) return null;

  return (
    <div className="staff-sections">
      <h1>Все разделы</h1>
      <div className="staff-sections-grid">
        {staffGroups(admin).map((group) => (
          <section key={group.id} aria-labelledby={`staff-${group.id}`}>
            <h2 id={`staff-${group.id}`}>{group.title}</h2>
            {group.links.map((link) => (
              <Link key={link.to} to={link.to}>
                {link.label}{' '}
                {link.count && <Count value={{ suggestions, comments, feedback }[link.count]} />}
              </Link>
            ))}
          </section>
        ))}
      </div>
    </div>
  );
}
