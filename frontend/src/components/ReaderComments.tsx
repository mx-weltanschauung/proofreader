import { useEffect, useId, useMemo, useRef, useState } from 'react';
import type { CommentRevision, ReaderComment } from '../types';
import { readerCommentsApi } from '../services/api';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { MarginNoteEditor } from './MarginNoteEditor';
import { MarginNotes, type MarginNotesProps } from './MarginNotes';
import { useReaderComments, type ReaderCommentsState } from '../hooks/useReaderComments';
import { useAuth } from '../hooks/useAuth';
import './ReaderComments.css';

export function ReaderCommentCard({
  comment: c,
  onChange,
  onDelete,
  onReply,
}: {
  comment: ReaderComment;
  onChange(c: ReaderComment): void;
  onDelete(id: number): void;
  onReply?(): void;
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
      {!c.thread_id && c.target === 'chapter' && c.chapter_id === null && (
        <div>Глава не найдена: {c.chapter_title}</div>
      )}
      {!c.removed && <div className="reader-comment-author">{c.author_nickname || 'Редакция'}</div>}
      {c.reply_to_nickname !== undefined && (
        <div className="reader-comment-status">
          {c.reply_to_nickname ? `Ответ читателю ${c.reply_to_nickname}` : 'Ответ редакции'}
        </div>
      )}
      {!c.removed && c.status && (
        <div className="reader-comment-status">
          {c.status === 'на_рассмотрении'
            ? 'Ждёт проверки'
            : c.status === 'отклонено'
              ? `Отклонён${c.reject_reason ? `: ${c.reject_reason}` : ''}`
              : 'Опубликован'}
        </div>
      )}
      {c.removed ? (
        <p className="reader-comment-status">
          {c.removed === 'author' ? 'Комментарий удалён' : 'Снят модератором'}
        </p>
      ) : editing ? (
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
      {!c.removed && c.edited_at && (
        <small>Изменено {new Date(c.edited_at).toLocaleString('ru')}</small>
      )}
      {!c.removed && c.published_body && c.published_body !== c.body && (
        <details>
          <summary>Сейчас опубликовано</summary>
          <p className="reader-comment-body">{c.published_body}</p>
        </details>
      )}
      <div className="margin-note-actions">
        {onReply && !c.removed && c.published && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={onReply}>
            Ответить
          </button>
        )}
        {c.can_edit && !c.removed && !editing && (
          <button type="button" className="btn btn-ghost btn-sm" onClick={() => setEditing(true)}>
            Изменить
          </button>
        )}
        {c.can_manage && !c.removed && (
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
        {c.can_edit && !c.removed && !c.thread_id && !c.published_revision && (
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

function useDisplayedReplies(
  rootId: number,
  replies: ReaderComment[] | undefined,
  viewerId: number | null,
) {
  const read = useRef(new Set<number>());
  useEffect(() => {
    if (viewerId === null || !replies?.some((reply) => reply.unread)) return;
    let alive = true;
    let sending = false;
    const markRead = async () => {
      if (document.visibilityState === 'hidden' || sending) return;
      sending = true;
      try {
        const ids = replies.filter((reply) => reply.unread && !read.current.has(reply.id));
        for (let i = 0; alive && i < ids.length; i += 200) {
          const batch = ids.slice(i, i + 200).map((reply) => reply.id);
          await readerCommentsApi.markThreadRead(rootId, batch);
          batch.forEach((id) => read.current.add(id));
          window.dispatchEvent(new Event('reader-comment-replies-read'));
        }
      } finally {
        sending = false;
      }
    };
    const visible = () => void markRead().catch(() => {});
    visible();
    document.addEventListener('visibilitychange', visible);
    return () => {
      alive = false;
      document.removeEventListener('visibilitychange', visible);
    };
  }, [rootId, replies, viewerId]);
}

function ThreadedComment({
  comment,
  state,
  visible = true,
}: {
  comment: ReaderComment;
  state: ReaderCommentsState;
  visible?: boolean;
}) {
  const viewerId = useAuth((s) => s.user?.id ?? null);
  const direct = state.requestedThread?.root.id === comment.id ? state.requestedThread : undefined;
  const [opened, setOpened] = useState<boolean | null>(null);
  const [loadedReplies, setLoadedReplies] = useState<ReaderComment[]>();
  const [recipient, setRecipient] = useState<ReaderComment | null>(null);
  const [failure, setFailure] = useState('');
  const [busy, setBusy] = useState(false);
  const request = useRef(0);
  const repliesId = useId();
  const open = opened ?? Boolean(direct);
  const replies = loadedReplies ?? direct?.replies;
  useDisplayedReplies(comment.id, visible && open && !busy ? replies : undefined, viewerId);

  const load = async (removed = false) => {
    const current = ++request.current;
    setBusy(true);
    setFailure('');
    try {
      const { data } = await readerCommentsApi.thread(comment.id);
      if (current !== request.current) return;
      setLoadedReplies(data.replies);
      if (removed || data.root.removed !== comment.removed) state.replace(data.root);
    } catch (err) {
      if (current !== request.current) return;
      if (removed || apiErrorStatus(err) === 404) {
        state.drop(comment.id);
      } else {
        setLoadedReplies([]);
        setFailure(apiErrorMessage(err, 'Не удалось загрузить ответы'));
      }
    } finally {
      if (current === request.current) setBusy(false);
    }
  };
  const startReply = (target: ReaderComment) => {
    setRecipient(target);
    if (!open) {
      setOpened(true);
      void load();
    }
  };
  const refreshAfterDelete = (id: number) => {
    if (recipient?.id === id) setRecipient(null);
    void load(true);
  };
  const count = comment.reply_count ?? 0;
  const hasUnpublished = replies?.some((reply) => reply.published === false);

  return (
    <div className="reader-comment-thread">
      <ReaderCommentCard
        key={comment.revision}
        comment={comment}
        onChange={state.replace}
        onDelete={refreshAfterDelete}
        onReply={viewerId !== null ? () => startReply(comment) : undefined}
      />
      {(open || count > 0 || hasUnpublished) && (
        <button
          type="button"
          className="btn btn-ghost btn-sm"
          aria-expanded={open}
          aria-controls={open ? repliesId : undefined}
          onClick={() => {
            setOpened(!open);
            setRecipient(null);
            if (!open) void load();
          }}
        >
          {open ? 'Скрыть ответы' : count > 0 ? `Ответы (${count})` : 'Ответы'}
        </button>
      )}
      {open && (
        <div className="reader-comment-replies" id={repliesId}>
          {busy && <p role="status">Загрузка ответов…</p>}
          {failure && <p role="alert">{failure}</p>}
          {!busy &&
            replies?.map((reply) => (
              <ReaderCommentCard
                key={`${reply.id}:${reply.revision}`}
                comment={reply}
                onChange={(saved) => {
                  setLoadedReplies((items) =>
                    (items ?? replies).map((item) => (item.id === saved.id ? saved : item)),
                  );
                  window.dispatchEvent(new Event('reader-comments-changed'));
                }}
                onDelete={refreshAfterDelete}
                onReply={viewerId !== null ? () => startReply(reply) : undefined}
              />
            ))}
          {recipient && replies && !busy && !failure && (
            <div className="reader-comment-composer">
              <div className="reader-comment-status">
                Ответ для {recipient.author_nickname || 'редакции'}
              </div>
              <MarginNoteEditor
                key={recipient.id}
                kind="comment"
                save={async (body) => {
                  const { data } = await readerCommentsApi.reply(recipient.id, body);
                  setLoadedReplies((items) => [...(items ?? replies), data]);
                  window.dispatchEvent(new Event('reader-comments-changed'));
                  return data;
                }}
                onSaved={() => setRecipient(null)}
                onCancel={() => setRecipient(null)}
              />
            </div>
          )}
        </div>
      )}
    </div>
  );
}

/** Единая раскладка исключает наложение независимых слоёв заметок и комментариев. */
export function CommentedMarginNotes({
  workId,
  readerComments,
  threadId,
  ...props
}: MarginNotesProps & {
  workId: number;
  readerComments?: ReaderCommentsState;
  threadId?: number | null;
}) {
  const own = useReaderComments(readerComments ? null : workId, threadId);
  const state = readerComments ?? own;
  const userId = useAuth((s) => s.user?.id ?? null);
  const requestedRoot = state.requestedThread?.root;
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
      scrollToId={requestedRoot?.target === 'quote' ? -requestedRoot.id : undefined}
      renderCard={(n) => {
        if (n.id >= 0) return undefined;
        const c = state.comments.find((x) => x.id === -n.id);
        return c ? (
          <ThreadedComment key={`${userId}:${workId}:${c.id}`} comment={c} state={state} />
        ) : null;
      }}
    />
  );
}

interface TargetCommentsProps {
  workId: number;
  chapterId?: number;
  readerComments?: ReaderCommentsState;
  threadId?: number | null;
}

function TargetCommentsInner({ workId, chapterId, readerComments, threadId }: TargetCommentsProps) {
  const own = useReaderComments(readerComments ? null : workId, threadId);
  const state = readerComments ?? own;
  const user = useAuth((s) => s.user);
  const [adding, setAdding] = useState(false);
  const [expanded, setExpanded] = useState<boolean | null>(null);
  const targetRef = useRef<HTMLDivElement>(null);
  const items = state.comments.filter((c) =>
    chapterId !== undefined
      ? c.target === 'chapter' && c.chapter_id === chapterId
      : c.target === 'work' || (c.target === 'chapter' && c.chapter_id === null),
  );
  const linked = items.find((c) => c.id === state.requestedThread?.root.id)?.id;
  const open = expanded ?? linked !== undefined;
  useEffect(() => {
    if (linked === undefined) return;
    targetRef.current?.scrollIntoView({ block: 'start' });
  }, [linked]);
  if (!user && items.length === 0) return null;
  return (
    <div className="target-comments" ref={targetRef}>
      {items.length > 0 && (
        <details open={open}>
          <summary
            onClick={(event) => {
              event.preventDefault();
              setExpanded(!open);
            }}
          >
            Комментарии к {chapterId !== undefined ? 'главе' : 'тому'} ({items.length})
          </summary>
          {items.map((c) => (
            <ThreadedComment key={c.id} comment={c} state={state} visible={open} />
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
  const threadId = props.threadId ?? props.readerComments?.requestedThread?.root.id;
  return (
    <TargetCommentsInner
      key={`${userId}:${props.workId}:${props.chapterId}:${threadId}`}
      {...props}
    />
  );
}
