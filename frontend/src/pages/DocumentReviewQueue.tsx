import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { documentsApi } from '../services/api';
import type { Document, DocumentRejectReason } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { DOCUMENT_REJECT_LABEL } from '../utils/documentLabels';
import { documentPath } from '../utils/documentPaths';
import './DocumentReviewQueue.css';

type ListState =
  | { status: 'loading' }
  | { status: 'ok'; items: Document[] }
  | { status: 'error'; message: string };

function QueueRow({ doc, onResolved }: { doc: Document; onResolved: (updated: Document) => void }) {
  const [reason, setReason] = useState<DocumentRejectReason | ''>('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [error, setError] = useState('');

  // Очередь несёт и слаг, и подпись у каждой строки — ходить за ними на
  // сервер отдельным запросом не нужно, key собирается прямо из doc.
  const key = { nickname: doc.author_nickname || undefined, slug: doc.slug };

  const handleApprove = async () => {
    setIsSubmitting(true);
    setError('');
    try {
      const response = await documentsApi.approve(key);
      toast.success('Разбор одобрен');
      onResolved(response.data);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось одобрить разбор'));
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleReject = async () => {
    if (!reason) return;
    setIsSubmitting(true);
    setError('');
    try {
      const response = await documentsApi.reject(key, reason);
      toast.success('Разбор отклонён');
      onResolved(response.data);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось отклонить разбор'));
    } finally {
      setIsSubmitting(false);
    }
  };

  return (
    <li className="document-review-row">
      <div className="document-review-row-main">
        <span className="document-review-title">{doc.title}</span>
        <span className="document-review-author">Читатель {doc.author_nickname}</span>
        {doc.submitted_at && (
          <span className="document-review-date">
            Подано: {new Date(doc.submitted_at).toLocaleDateString()}
          </span>
        )}
        <Link to={documentPath(doc)} className="document-review-preview-link">
          Предпросмотр
        </Link>
      </div>

      {error && <div className="document-review-error">{error}</div>}

      <div className="document-review-actions">
        <button
          type="button"
          className="btn btn-primary"
          disabled={isSubmitting}
          onClick={() => void handleApprove()}
        >
          Принять
        </button>

        <select
          aria-label="Причина"
          value={reason}
          onChange={(e) => setReason(e.target.value as DocumentRejectReason | '')}
          disabled={isSubmitting}
        >
          <option value="">Причина отказа…</option>
          {(Object.keys(DOCUMENT_REJECT_LABEL) as DocumentRejectReason[]).map((r) => (
            <option key={r} value={r}>
              {DOCUMENT_REJECT_LABEL[r]}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="btn btn-secondary"
          disabled={isSubmitting || !reason}
          onClick={() => void handleReject()}
        >
          Отклонить
        </button>
      </div>
    </li>
  );
}

/**
 * Очередь модерации читательских разборов (задача 12). Модели —
 * SuggestionQueue.tsx, но без панели разбора одного элемента: у разбора,
 * в отличие от предложения к полосе, есть собственная страница просмотра
 * (`/documents/{слаг}` или `/documents/{ник}/{слаг}`, см. documentPaths.ts),
 * и вести туда же второй, встроенный предпросмотр не нужно — только ссылка.
 */
export const DocumentReviewQueue: React.FC = () => {
  const [state, setState] = useState<ListState>({ status: 'loading' });

  useEffect(() => {
    let cancelled = false;
    documentsApi
      .review()
      .then((response) => {
        if (!cancelled) setState({ status: 'ok', items: response.data ?? [] });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить очередь'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const handleResolved = (id: number) => {
    // Разобранная запись выходит из очереди сама по себе — approve/reject не
    // меняют review_status обратно на «на_рассмотрении», перечитывать список
    // ради одной строки незачем.
    setState((prev) =>
      prev.status === 'ok' ? { status: 'ok', items: prev.items.filter((d) => d.id !== id) } : prev,
    );
  };

  const isLoading = state.status === 'loading';
  const loadError = state.status === 'error' ? state.message : '';
  const items = state.status === 'ok' ? state.items : [];

  return (
    <div className="document-review-container">
      <h1>Очередь разборов</h1>

      {isLoading && <div className="loading-state">Загрузка…</div>}
      {loadError && <div className="error-state">{loadError}</div>}

      {!isLoading && !loadError && items.length === 0 && (
        <div className="document-review-empty">Очередь пуста.</div>
      )}

      {!isLoading && !loadError && items.length > 0 && (
        <ul className="document-review-list">
          {items.map((doc) => (
            <QueueRow key={doc.id} doc={doc} onResolved={(updated) => handleResolved(updated.id)} />
          ))}
        </ul>
      )}
    </div>
  );
};
