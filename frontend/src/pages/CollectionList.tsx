import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { collectionsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Collection } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { collectionPath } from '../utils/paths';
import './CollectionList.css';

export const CollectionList: React.FC = () => {
  // Тот же заголовок, что печатает internal/seo/render_index.go
  // (CollectionList): «Подборки — Читальня».
  useDocumentTitle('Подборки');

  const [collections, setCollections] = useState<Collection[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  // Список отдаёт только витрину читальни (author_nickname == ''), но
  // создать подборку с этого экрана может любой вошедший — читатель тоже:
  // POST /collections принимает читателя наравне с редактором/администратором
  // (см. reader-подроутер в router.go), а собственные подборки читателя
  // видны на экране «моё», не здесь.
  const { isAuthenticated } = useAuth();

  useEffect(() => {
    const load = async () => {
      try {
        const response = await collectionsApi.list();
        // Список отдаёт null на пустой выборке, как и остальные списки API.
        setCollections(response.data ?? []);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить подборки'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void load();
  }, []);

  if (isLoading) {
    return <div className="loading-state">Загрузка подборок…</div>;
  }

  return (
    <div className="collection-list-container">
      <div className="collection-list-header">
        <h1>Подборки</h1>
      </div>

      {error && <div className="error-message">{error}</div>}

      {isAuthenticated && (
        <div className="collection-list-actions">
          <Link to="/collections/new" className="btn btn-primary">
            Создать подборку
          </Link>
        </div>
      )}

      {collections.length === 0 ? (
        <div className="empty-state">
          <p>Подборок пока нет.</p>
          {isAuthenticated && (
            <Link to="/collections/new" className="btn btn-primary">
              Создать первую подборку
            </Link>
          )}
        </div>
      ) : (
        <div className="collection-grid">
          {collections.map((collection) => (
            <Link key={collection.id} to={collectionPath(collection)} className="collection-card">
              <h3>{collection.title}</h3>
              {collection.description && (
                <p className="collection-card-description">{collection.description}</p>
              )}
            </Link>
          ))}
        </div>
      )}
    </div>
  );
};
