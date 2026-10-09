import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import { journalsApi } from '../services/api';
import type { JournalDetail as Detail } from '../types';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { apiErrorMessage } from '../utils/apiError';
import { workPath } from '../utils/paths';
import './JournalDetail.css';

export function JournalDetail() {
  const { slug = '' } = useParams<{ slug: string }>();
  const [detail, setDetail] = useState<Detail | null>(null);
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  useDocumentTitle(detail?.journal.title);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      setIsLoading(true);
      setError('');
      try {
        const { data } = await journalsApi.get(slug);
        if (alive) setDetail(data);
      } catch (err: unknown) {
        if (alive) setError(apiErrorMessage(err, 'Журнал не найден'));
      } finally {
        if (alive) setIsLoading(false);
      }
    };
    void load();
    return () => {
      alive = false;
    };
  }, [slug]);

  if (isLoading) return <div className="loading-state">Загрузка журнала…</div>;
  if (error || !detail) {
    return (
      <div className="error-state">
        <div>{error || 'Журнал не найден'}</div>
        <Link to="/" className="back-link">
          ← В читальню
        </Link>
      </div>
    );
  }
  const { journal, years } = detail;
  return (
    <div className="journal-page">
      <Link to="/" className="back-link">
        ← В читальню
      </Link>
      <header className="journal-header">
        <h1 className="journal-title">{journal.title}</h1>
        {journal.subtitle && <p className="journal-subtitle">{journal.subtitle}</p>}
        {journal.description && <p className="journal-description">{journal.description}</p>}
      </header>
      {years.length === 0 ? (
        <div className="empty-state">Номеров пока нет</div>
      ) : (
        <ol className="journal-years">
          {years.map((y) => (
            <li key={y.year} className="journal-year">
              <h2 className="journal-year-label">{y.year}</h2>
              <ul className="journal-issues">
                {y.issues.map((issue) => (
                  <li key={issue.id} className="journal-issue">
                    <Link
                      to={workPath({ id: issue.work_id, slug: issue.work_slug })}
                      className="journal-issue-link"
                    >
                      № {issue.label}
                    </Link>
                    {issue.months && <span className="journal-issue-months">{issue.months}</span>}
                  </li>
                ))}
              </ul>
            </li>
          ))}
        </ol>
      )}
    </div>
  );
}
