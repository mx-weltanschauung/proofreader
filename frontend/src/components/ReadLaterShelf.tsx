import { Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { useChangeReadLater, useReadLater } from '../hooks/useReadLater';
import type { ReadLaterEntry } from '../types';
import { chapterPath, workPath } from '../utils/paths';
import { apiErrorMessage } from '../utils/apiError';
import './ReadLaterShelf.css';

const PREVIEW_SIZE = 3;

export function ReadLaterShelf({ userId, preview = false }: { userId: number; preview?: boolean }) {
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
  if (!shelf.data.length) {
    return (
      <div className="read-later-empty">
        <p>Отложенных публикаций пока нет.</p>
        <p>Чтобы сохранить публикацию, нажмите «Отложить» на странице тома или главы.</p>
      </div>
    );
  }

  const entries = preview ? shelf.data.slice(0, PREVIEW_SIZE) : shelf.data;

  return (
    <>
      <ul className="read-later-list">
        {entries.map((entry) => (
          <ShelfRow
            key={entry.chapter_id ? `chapter/${entry.chapter_id}` : `work/${entry.work_id}`}
            entry={entry}
            userId={userId}
          />
        ))}
      </ul>
      {entries.length < shelf.data.length && (
        <Link to="/mine/read-later" className="btn btn-secondary">
          Открыть всю полку
        </Link>
      )}
    </>
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
