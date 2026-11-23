import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { collectionsApi } from '../services/api';
import type { CollectionProgramReviewEntry } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { collectionPath } from '../utils/paths';
import './DocumentReviewQueue.css';

function ProgramReviewRow({
  entry,
  onResolved,
}: {
  entry: CollectionProgramReviewEntry;
  onResolved: () => void;
}) {
  const [reason, setReason] = useState('');
  const [saving, setSaving] = useState(false);
  const [error, setError] = useState('');

  const resolve = async (decision: 'approve' | 'reject') => {
    setSaving(true);
    setError('');
    try {
      await collectionsApi.resolveProgram(entry, decision, reason.trim());
      onResolved();
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить решение'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <li className="document-review-row">
      <div className="document-review-row-main">
        <span className="document-review-title">{entry.title}</span>
        <span className="document-review-author">Читатель {entry.author_nickname}</span>
        <span className="document-review-date">
          {new Date(entry.submitted_at).toLocaleDateString()}
        </span>
        <Link
          to={collectionPath(entry)}
          target="_blank"
          rel="noreferrer"
          className="document-review-preview-link"
        >
          Предпросмотр в новой вкладке
        </Link>
      </div>
      {error && (
        <div role="alert" className="document-review-error">
          {error}
        </div>
      )}
      <div className="document-review-actions">
        <button
          type="button"
          className="btn btn-primary"
          disabled={saving}
          onClick={() => void resolve('approve')}
        >
          Одобрить и опубликовать
        </button>
        <textarea
          aria-label="Причина возврата"
          placeholder="Причина возврата"
          value={reason}
          onChange={(event) => setReason(event.target.value)}
          rows={2}
          disabled={saving}
        />
        <button
          type="button"
          className="btn btn-secondary"
          disabled={saving || !reason.trim()}
          onClick={() => void resolve('reject')}
        >
          Вернуть на доработку
        </button>
      </div>
    </li>
  );
}

export function CollectionProgramReviewQueue() {
  const [entries, setEntries] = useState<CollectionProgramReviewEntry[] | null>(null);
  const [error, setError] = useState('');

  useEffect(() => {
    let cancelled = false;
    collectionsApi.programReviewQueue().then(
      ({ data }) => {
        if (!cancelled) setEntries(data);
      },
      (err: unknown) => {
        if (!cancelled) setError(apiErrorMessage(err, 'Не удалось загрузить очередь программ'));
      },
    );
    return () => {
      cancelled = true;
    };
  }, []);

  return (
    <div className="document-review-container">
      <h1>Программы на проверку</h1>
      {error && (
        <div role="alert" className="error-state">
          {error}
        </div>
      )}
      {!entries && !error && <div className="loading-state">Загрузка…</div>}
      {entries?.length === 0 && <div className="document-review-empty">Очередь пуста.</div>}
      {entries && entries.length > 0 && (
        <ul className="document-review-list">
          {entries.map((entry) => (
            <ProgramReviewRow
              key={entry.id}
              entry={entry}
              onResolved={() =>
                setEntries((current) => current?.filter(({ id }) => id !== entry.id) ?? null)
              }
            />
          ))}
        </ul>
      )}
    </div>
  );
}
