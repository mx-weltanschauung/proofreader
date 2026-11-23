import { Link, useLocation } from 'react-router-dom';
import toast from 'react-hot-toast';
import { useAuth } from '../hooks/useAuth';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { useChangeReadLater, useReadLater } from '../hooks/useReadLater';
import type { ReadLaterEntry } from '../types';
import { chapterPath, workPath } from '../utils/paths';
import { joinPathFor } from '../utils/returnUrl';
import { apiErrorMessage } from '../utils/apiError';
import './MyReadLater.css';

export function MyReadLater() {
  const { isAuthenticated, user } = useAuth();
  const location = useLocation();
  useDocumentTitle('Отложено');

  return (
    <div className="read-later-page">
      <h1>Отложено</h1>
      {isAuthenticated && user ? (
        <Shelf key={user.id} userId={user.id} />
      ) : (
        <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
          Войти в читальню
        </Link>
      )}
    </div>
  );
}

function Shelf({ userId }: { userId: number }) {
  const shelf = useReadLater(userId, true);
  if (shelf.isError) {
    return (
      <div role="alert">
        <p>{apiErrorMessage(shelf.error, 'Не удалось загрузить полку')}</p>
        <button type="button" className="btn btn-secondary" onClick={() => void shelf.refetch()}>
          Повторить
        </button>
      </div>
    );
  }
  if (shelf.isPending || (shelf.isFetching && !shelf.isFetchedAfterMount)) {
    return <p role="status">Загрузка…</p>;
  }
  if (!shelf.data.length) return <p>Отложенных публикаций пока нет.</p>;

  return (
    <ul className="read-later-list">
      {shelf.data.map((entry) => (
        <ShelfRow
          key={entry.chapter_id ? `chapter/${entry.chapter_id}` : `work/${entry.work_id}`}
          entry={entry}
          userId={userId}
        />
      ))}
    </ul>
  );
}

function ShelfRow({ entry, userId }: { entry: ReadLaterEntry; userId: number }) {
  const work = { id: entry.work_id };
  const chapter = entry.chapter_id;
  const change = useChangeReadLater(
    userId,
    chapter ? { kind: 'chapter', id: chapter } : { kind: 'work', id: work.id },
  );

  return (
    <li className="read-later-row">
      <div>
        <Link to={chapter ? chapterPath(work, { id: chapter }) : workPath(work)}>
          {entry.title}
        </Link>
        <div className="read-later-source">{entry.source}</div>
      </div>
      <button
        type="button"
        className="btn btn-secondary btn-sm"
        aria-label={`Убрать из отложенного: ${entry.title}`}
        disabled={change.isPending}
        onClick={() =>
          change.mutate(false, {
            onError: (error) => toast.error(apiErrorMessage(error, 'Не удалось убрать публикацию')),
          })
        }
      >
        Убрать
      </button>
    </li>
  );
}
