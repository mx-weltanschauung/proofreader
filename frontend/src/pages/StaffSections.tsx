import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { canEdit, isAdmin, useAuth } from '../hooks/useAuth';
import { feedbackApi, readerCommentsApi, suggestionsApi } from '../services/api';
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
        <section aria-labelledby="staff-frequent">
          <h2 id="staff-frequent">Часто используемые</h2>
          <Link to="/suggestions/queue">
            Предложения <Count value={suggestions} />
          </Link>
          {admin && (
            <>
              <Link to="/admin/readers">Читатели</Link>
              <Link to="/admin/stats">Статистика</Link>
            </>
          )}
        </section>
        <section aria-labelledby="staff-work">
          <h2 id="staff-work">Работа</h2>
          <Link to="/documents/review">Разборы на проверку</Link>
          <Link to="/reader-comments/review">
            Комментарии <Count value={comments} />
          </Link>
          <Link to="/admin/audio">Озвучка</Link>
        </section>
        {admin && (
          <section aria-labelledby="staff-management">
            <h2 id="staff-management">Управление</h2>
            <Link to="/admin/users">Пользователи</Link>
            <Link to="/admin/feedback">
              Обращения <Count value={feedback} />
            </Link>
            <Link to="/admin/closures">Закрытия</Link>
            <Link to="/admin/cache">Кэш</Link>
            <Link to="/admin/settings">Настройки</Link>
          </section>
        )}
        <section aria-labelledby="staff-mine">
          <h2 id="staff-mine">Моё</h2>
          <Link to="/mine/notes">Заметки</Link>
          <Link to="/mine/comments">Мои комментарии</Link>
        </section>
      </div>
    </div>
  );
}
