import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { documentsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Document } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { documentPath } from '../utils/documentPaths';
import './DocumentList.css';

export const DocumentList: React.FC = () => {
  const [documents, setDocuments] = useState<Document[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  // Собрать и опубликовать свой разбор может любой вошедший, не только
  // персонал — mayEditDocument на сервере решает по нику-снимку (задача 11).
  const { isAuthenticated } = useAuth();
  const editable = isAuthenticated;

  useEffect(() => {
    const loadDocuments = async () => {
      try {
        const response = await documentsApi.list();
        setDocuments(response.data || []);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить разборы'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadDocuments();
  }, []);

  if (isLoading) {
    return <div className="loading-state">Загрузка разборов…</div>;
  }

  return (
    <div className="document-list-container">
      <div className="document-list-header">
        <h1>Разборы</h1>
      </div>

      {error && <div className="error-message">{error}</div>}

      {editable && (
        <div className="document-list-actions">
          <Link to="/documents/new" className="create-document-button">
            Собрать разбор
          </Link>
        </div>
      )}

      {documents.length === 0 ? (
        <div className="empty-state">
          <p>Разборов пока нет.</p>
          {editable && (
            <Link to="/documents/new" className="create-document-button">
              Собрать первый разбор
            </Link>
          )}
        </div>
      ) : (
        <div className="documents-grid">
          {documents.map((document) => (
            <Link key={document.id} to={documentPath(document)} className="document-card">
              <h3>{document.title}</h3>
              {/* Несъёмная пометка происхождения — тот же текст и то же
                  условие, что на странице просмотра (DocumentView.tsx). */}
              {document.author_nickname && (
                <p className="document-card-origin">Собрал читатель {document.author_nickname}</p>
              )}
              {/* Витрина — список ОПУБЛИКОВАННЫХ разборов (ListPublished
                  сортирует по published_at DESC), поэтому дата тут —
                  публикация, не создание черновика: она отвечает на вопрос
                  «когда это вышло к читателю», а не «когда автор начал».
                  published_at на этом экране не бывает null (сервер уже
                  отфильтровал непубликованные), но тип общий с черновиком —
                  запасное значение честное, а не утверждающее обратное `!`. */}
              <p className="document-meta">
                Опубликован:{' '}
                {new Date(document.published_at ?? document.created_at).toLocaleDateString()}
              </p>
            </Link>
          ))}
        </div>
      )}
    </div>
  );
};
