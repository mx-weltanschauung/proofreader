import { useMemo, useState } from 'react';
import type { CommentRevision, ReaderComment } from '../types';
import { readerCommentsApi } from '../services/api';
import { apiErrorMessage } from '../utils/apiError';
import { MarginNoteEditor } from './MarginNoteEditor';
import { MarginNotes, type MarginNotesProps } from './MarginNotes';
import { useReaderComments, type ReaderCommentsState } from '../hooks/useReaderComments';
import { useAuth } from '../hooks/useAuth';
import './ReaderComments.css';

export function ReaderCommentCard({
  comment: c,
  onChange,
  onDelete,
}: {
  comment: ReaderComment;
  onChange(c: ReaderComment): void;
  onDelete(id: number): void;
}) {
  const userId = useAuth((s) => s.user?.id ?? null);
  const [editing, setEditing] = useState(false);
  const [history, setHistory] = useState<CommentRevision[] | null>(null);
  const [failure, setFailure] = useState('');
  const [busy, setBusy] = useState(false);
  const action = async (run: () => Promise<unknown>, done: () => void) => {
    if (busy) return;
    setBusy(true);
    setFailure('');
    try {
      await run();
      done();
    } catch (e) {
      setFailure(apiErrorMessage(e, 'Не удалось выполнить действие'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div className="reader-comment">
      {c.target === 'chapter' && c.chapter_id === null && (
        <div>Глава не найдена: {c.chapter_title}</div>
      )}
      <div className="reader-comment-author">{c.author_nickname || 'Редакция'}</div>
      {c.status && (
        <div className="reader-comment-status">
          {c.status === 'на_рассмотрении'
            ? 'Ждёт проверки'
            : c.status === 'отклонено'
              ? `Отклонён${c.reject_reason ? `: ${c.reject_reason}` : ''}`
              : 'Опубликован'}
        </div>
      )}
      {editing ? (
        <MarginNoteEditor
          kind="comment"
          initial={c.body}
          save={(body) =>
            readerCommentsApi.update(c.id, body, c.revision).then((r) => {
              onChange(r.data);
              return r.data;
            })
          }
          onSaved={() => setEditing(false)}
          onCancel={() => setEditing(false)}
        />
      ) : (
        <p className="reader-comment-body">{c.body}</p>
      )}
      {c.edited_at && <small>Изменено {new Date(c.edited_at).toLocaleString('ru')}</small>}
      {c.published_body && c.published_body !== c.body && (
        <details>
          <summary>Сейчас опубликовано</summary>
          <p className="reader-comment-body">{c.published_body}</p>
        </details>
      )}
      <div className="margin-note-actions">
        {c.can_edit && !editing && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => setEditing(true)}>
            Изменить
          </button>
        )}
        {c.can_manage && (
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={busy}
            onClick={() => {
              if (window.confirm('Удалить комментарий?'))
                void action(
                  () => readerCommentsApi.remove(c.id),
                  () => onDelete(c.id),
                );
            }}
          >
            Удалить
          </button>
        )}
        {c.can_manage && (
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={busy}
            onClick={() => {
              if (history) {
                setHistory(null);
                return;
              }
              void action(
                () => readerCommentsApi.history(c.id).then((r) => setHistory(r.data)),
                () => {},
              );
            }}
          >
            История
          </button>
        )}
        {c.can_edit && !c.published_revision && (
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={busy}
            onClick={() =>
              void action(
                () =>
                  readerCommentsApi.toNote(c.id).then((r) => {
                    window.dispatchEvent(
                      new CustomEvent('margin-note-created', { detail: { note: r.data, userId } }),
                    );
                  }),
                () => onDelete(c.id),
              )
            }
          >
            Вернуть в заметки
          </button>
        )}
      </div>
      {history && (
        <ol>
          {history.map((r) => (
            <li key={r.revision}>
              <small>
                Редакция {r.revision}: {r.status}
                {r.reason && ` — ${r.reason}`}
              </small>
              <p className="reader-comment-body">{r.body}</p>
            </li>
          ))}
        </ol>
      )}
      {failure && <div role="alert">{failure}</div>}
    </div>
  );
}

/** Единая раскладка исключает наложение независимых слоёв заметок и комментариев. */
export function CommentedMarginNotes({
  workId,
  readerComments,
  ...props
}: MarginNotesProps & { workId: number; readerComments?: ReaderCommentsState }) {
  const own = useReaderComments(readerComments ? null : workId);
  const state = readerComments ?? own;
  const combined = useMemo(
    () => [
      ...props.notes,
      ...state.comments.filter((c) => c.target === 'quote').map((c) => ({ ...c, id: -c.id })),
    ],
    [props.notes, state.comments],
  );
  return (
    <MarginNotes
      {...props}
      notes={combined}
      renderCard={(n) => {
        if (n.id >= 0) return undefined;
        const c = state.comments.find((x) => x.id === -n.id);
        return c ? (
          <ReaderCommentCard
            key={`${c.id}:${c.revision}`}
            comment={c}
            onChange={state.replace}
            onDelete={state.drop}
          />
        ) : null;
      }}
    />
  );
}

interface TargetCommentsProps {
  workId: number;
  chapterId?: number;
  readerComments?: ReaderCommentsState;
}

function TargetCommentsInner({ workId, chapterId, readerComments }: TargetCommentsProps) {
  const own = useReaderComments(readerComments ? null : workId);
  const state = readerComments ?? own;
  const user = useAuth((s) => s.user);
  const [adding, setAdding] = useState(false);
  const items = state.comments.filter((c) =>
    chapterId !== undefined
      ? c.target === 'chapter' && c.chapter_id === chapterId
      : c.target === 'work' || (c.target === 'chapter' && c.chapter_id === null),
  );
  if (!user && items.length === 0) return null;
  return (
    <div className="target-comments">
      {items.length > 0 && (
        <details>
          <summary>
            Комментарии к {chapterId !== undefined ? 'главе' : 'тому'} ({items.length})
          </summary>
          {items.map((c) => (
            <ReaderCommentCard
              key={`${c.id}:${c.revision}`}
              comment={c}
              onChange={state.replace}
              onDelete={state.drop}
            />
          ))}
        </details>
      )}
      {user && !adding && (
        <button type="button" className="btn btn-secondary btn-sm" onClick={() => setAdding(true)}>
          Комментарий к {chapterId !== undefined ? 'главе' : 'тому'}
        </button>
      )}
      {adding && (
        <MarginNoteEditor
          kind="comment"
          save={(body) =>
            readerCommentsApi
              .create(
                workId,
                chapterId !== undefined
                  ? { target: 'chapter', chapter_id: chapterId, body }
                  : { target: 'work', body },
              )
              .then((r) => r.data)
          }
          onSaved={() => {
            setAdding(false);
            window.dispatchEvent(new Event('reader-comments-changed'));
          }}
          onCancel={() => setAdding(false)}
        />
      )}
    </div>
  );
}

export function TargetComments(props: TargetCommentsProps) {
  const userId = useAuth((s) => s.user?.id ?? null);
  return <TargetCommentsInner key={`${userId}:${props.workId}:${props.chapterId}`} {...props} />;
}
