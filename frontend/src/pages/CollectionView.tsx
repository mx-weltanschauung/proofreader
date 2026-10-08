import { useEffect, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { collectionsApi } from '../services/api';
import { useAuth, canEditCollection } from '../hooks/useAuth';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import type { Collection, CollectionTocNode } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { sourceLabel } from '../utils/sourceLabel';
import { DownloadMenu } from '../components/DownloadMenu';
import { ShareButton } from '../components/ShareButton';
import { chapterPath, collectionPath, collectionReadPath } from '../utils/paths';
import './CollectionView.css';

function TocNodes({
  nodes,
  work,
}: {
  nodes: CollectionTocNode[];
  work?: { id: number; slug?: string };
}) {
  return (
    <ol className="collection-subtree">
      {nodes.map((node, i) => (
        <li key={`${node.title}-${node.page_start}-${i}`} className="collection-subtree-item">
          {/* Строка заголовок+страница вынесена в отдельный flex-контейнер:
              вложенный <ol> — блочный сосед, а не третий элемент той же
              строки. Раньше вложённое дерево (например, у «Замечаний к
              программе...» есть свои I—IV) вставало в строку с номером
              страницы родителя, а не под ним. */}
          <div className="collection-subtree-row">
            {work === undefined ? (
              // Тома нет — вести подглаву некуда.
              <span className="collection-subtree-title">{node.title}</span>
            ) : (
              <Link
                to={chapterPath(work, { id: node.chapter_id, slug: node.chapter_slug })}
                className="collection-subtree-title"
              >
                {node.title}
              </Link>
            )}
            <span className="collection-subtree-page">{node.page_start}</span>
          </div>
          {node.children && node.children.length > 0 && (
            <TocNodes nodes={node.children} work={work} />
          )}
        </li>
      ))}
    </ol>
  );
}

export const CollectionView: React.FC = () => {
  // nickname есть только у длинного (читательского) адреса — короткий вид
  // разбирает те же :slug, что и раньше (см. paths.ts: у витринной подборки
  // author_nickname пуст, и сервер под коротким адресом ищет ровно такую).
  const { nickname, slug } = useParams<{ nickname?: string; slug: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [collection, setCollection] = useState<Collection | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  // Своё/чужое — по нику владельца подборки, а не по общей роли: та же
  // проверка, что mayEdit на сервере (canEditCollection в useAuth.ts).
  const editable = Boolean(collection) && canEditCollection(user, collection!);

  // Средний сегмент — тот же, что печатает internal/seo/render_index.go
  // (Collection): «название — подборка — Читальня».
  useDocumentTitle(collection ? `${collection.title} — подборка` : null);

  useEffect(() => {
    if (!slug) return;

    const load = async () => {
      try {
        const response = await collectionsApi.get(slug, nickname);
        setCollection(response.data);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить подборку'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится.
    void load();
  }, [slug, nickname]);

  const handleDelete = async () => {
    if (!collection) return;
    const confirmed = window.confirm(`Удалить подборку «${collection.title}»?`);
    if (!confirmed) return;

    try {
      await collectionsApi.remove(collection.slug, nickname);
      toast.success('Подборка удалена');
      navigate('/collections');
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось удалить подборку'));
    }
  };

  if (isLoading) return <div className="loading-state">Загрузка подборки…</div>;

  if (error || !collection) {
    return (
      <div className="error-state">
        <div>{error || 'Подборка не найдена'}</div>
        <Link to="/collections" className="back-link">
          ← К подборкам
        </Link>
      </div>
    );
  }

  const items = collection.items ?? [];

  return (
    <div className="collection-view">
      <Link to="/collections" className="back-link">
        ← К подборкам
      </Link>

      <header className="collection-masthead">
        <h1>{collection.title}</h1>
        {collection.description && (
          <p className="collection-description">{collection.description}</p>
        )}
      </header>

      <div className="collection-view-actions">
        <DownloadMenu href={`/api${collectionPath(collection)}/download`} />
        {/* Черновик видит только владелец: получатель ссылки открыл бы 404. */}
        {collection.published_at && (
          <ShareButton title={`${collection.title} — подборка`} path={collectionPath(collection)} />
        )}
        {editable && (
          <>
            <Link to={`/collections/${collection.slug}/edit`} className="btn btn-secondary btn-sm">
              Изменить
            </Link>
            <button
              type="button"
              className="btn btn-danger btn-sm"
              onClick={() => void handleDelete()}
            >
              Удалить
            </button>
          </>
        )}
      </div>

      <h2 className="collection-toc-title">Содержание</h2>

      {items.length === 0 ? (
        <div className="empty-state">
          <p>В подборке пока ничего нет.</p>
        </div>
      ) : (
        <ol className="collection-toc">
          {items.map((entry) => (
            <li key={entry.id} className={`collection-entry ${entry.broken ? 'is-broken' : ''}`}>
              <div className="collection-entry-head">
                {entry.author && <span className="collection-entry-author">{entry.author}. </span>}
                {entry.broken ? (
                  // Битую строку некуда вести: источник удалён, читать нечего.
                  <span className="collection-entry-title">{entry.title}</span>
                ) : (
                  <Link
                    to={collectionReadPath(collection, entry.id)}
                    className="collection-entry-title"
                  >
                    {entry.title}
                  </Link>
                )}
                <span className="collection-entry-source">{sourceLabel(entry)}</span>
              </div>

              {entry.broken && (
                <div className="collection-entry-note">Источник удалён из читальни</div>
              )}

              {entry.children && entry.children.length > 0 && (
                <TocNodes
                  nodes={entry.children}
                  work={
                    entry.source
                      ? { id: entry.source.work_id, slug: entry.source.work_slug }
                      : undefined
                  }
                />
              )}
            </li>
          ))}
        </ol>
      )}
    </div>
  );
};
