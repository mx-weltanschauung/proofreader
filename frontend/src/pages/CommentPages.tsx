import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { diffWords } from 'diff';
import { useAuth, canEdit, isAdmin } from '../hooks/useAuth';
import { readerCommentsApi } from '../services/api';
import type { ReaderComment } from '../types';
import { ReaderCommentCard } from '../components/ReaderComments';
import { noteHref } from '../utils/marginNoteHref';
import { apiErrorMessage } from '../utils/apiError';

function ReviewActions({ comment: c, onDone }: { comment: ReaderComment; onDone(): void }) {
  const [reason, setReason] = useState('');
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState('');
  const run = async (action: () => Promise<unknown>) => {
    if (busy) return;
    setBusy(true);
    setFailure('');
    try {
      await action();
      onDone();
    } catch (err) {
      setFailure(apiErrorMessage(err, 'Не удалось рассмотреть комментарий'));
    } finally {
      setBusy(false);
    }
  };
  return (
    <div>
      {c.published_body && c.published_body !== c.body && (
        <div aria-label="Изменения">
          {diffWords(c.published_body, c.body).map((part, i) =>
            part.added ? (
              <ins key={i}>{part.value}</ins>
            ) : part.removed ? (
              <del key={i}>{part.value}</del>
            ) : (
              <span key={i}>{part.value}</span>
            ),
          )}
        </div>
      )}
      <label>
        Причина отказа (необязательно)
        <input value={reason} maxLength={1000} onChange={(e) => setReason(e.target.value)} />
      </label>
      <div className="margin-note-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={busy}
          onClick={() => void run(() => readerCommentsApi.review(c.id, c.revision, true))}
        >
          Одобрить
        </button>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          disabled={busy}
          onClick={() => void run(() => readerCommentsApi.review(c.id, c.revision, false, reason))}
        >
          Отклонить
        </button>
        {c.author_id && (
          <button
            type="button"
            className="btn btn-ghost btn-sm"
            disabled={busy}
            onClick={() => {
              if (
                window.confirm(
                  `Отклонить всё ожидающее от ${c.author_nickname} и запретить комментировать?`,
                )
              )
                void run(() => readerCommentsApi.ban(c.id));
            }}
          >
            Отклонить всё и запретить
          </button>
        )}
      </div>
      <small>Отказов у автора: {c.rejections ?? 0}</small>
      {failure && <p role="alert">{failure}</p>}
    </div>
  );
}

function BannedCommenters() {
  const [items, setItems] = useState<{ id: number; nickname: string }[]>([]);
  const [failure, setFailure] = useState('');
  useEffect(() => {
    let alive = true;
    void readerCommentsApi
      .banned()
      .then((r) => {
        if (alive) setItems(r.data);
      })
      .catch((e) => {
        if (alive) setFailure(apiErrorMessage(e, 'Не удалось загрузить запреты'));
      });
    return () => {
      alive = false;
    };
  }, []);
  return (
    <details>
      <summary>Запреты комментирования ({items.length})</summary>
      {items.map((u) => (
        <p key={u.id}>
          {u.nickname}{' '}
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={() => {
              void readerCommentsApi
                .unban(u.id)
                .then(() => setItems((s) => s.filter((x) => x.id !== u.id)))
                .catch((e) => setFailure(apiErrorMessage(e, 'Не удалось снять запрет')));
            }}
          >
            Снять запрет
          </button>
        </p>
      ))}
      {failure && <p role="alert">{failure}</p>}
    </details>
  );
}

function CommentListInner({ queue }: { queue: boolean }) {
  const user = useAuth((s) => s.user);
  const [items, setItems] = useState<ReaderComment[]>([]);
  const [failure, setFailure] = useState('');
  const [busy, setBusy] = useState(true);
  const [offset, setOffset] = useState(0);
  const [more, setMore] = useState(false);
  const [refresh, setRefresh] = useState(0);
  const reload = useCallback(() => {
    setBusy(true);
    setFailure('');
    setOffset(0);
    setRefresh((v) => v + 1);
    window.dispatchEvent(new Event('reader-comments-changed'));
  }, []);
  useEffect(() => {
    if (!user || (queue && !canEdit(user))) return;
    let alive = true;
    void (queue ? readerCommentsApi.queue(offset) : readerCommentsApi.mine(offset))
      .then((r) => {
        if (!alive) return;
        const page = r.data ?? [];
        setItems((s) => (offset === 0 ? page : [...s, ...page]));
        setMore(page.length === 50);
        setBusy(false);
      })
      .catch((e) => {
        if (alive) {
          setFailure(apiErrorMessage(e, 'Не удалось загрузить комментарии'));
          setBusy(false);
        }
      });
    return () => {
      alive = false;
    };
  }, [user, queue, offset, refresh]);
  if (!user)
    return (
      <div className="comment-list">
        <h1>Мои комментарии</h1>
        <Link to="/join">Войдите, чтобы видеть свои комментарии</Link>
      </div>
    );
  if (queue && !canEdit(user)) return <p>Нужны права сотрудника</p>;
  return (
    <main className="comment-list">
      <h1>{queue ? 'Комментарии на проверку' : 'Мои комментарии'}</h1>
      {!queue && (
        <p>
          <Link to="/mine/notes">Мои заметки</Link>
        </p>
      )}
      {queue && isAdmin(user) && <BannedCommenters key={refresh} />}
      {failure && <p role="alert">{failure}</p>}
      {busy && <p role="status">Загрузка…</p>}
      {!busy && items.length === 0 && !failure && (
        <p>{queue ? 'Нет комментариев на проверку' : 'Вы ещё не отправляли комментарии'}</p>
      )}
      {items.map((c) => (
        <section key={`${c.id}:${c.revision}`} className="comment-review">
          <Link to={noteHref(c).to}>
            {c.work_title || 'К тексту'}
            {c.chapter_title && ` — ${c.chapter_title}`}
          </Link>
          {c.quote_text && <blockquote>{c.quote_text}</blockquote>}
          <ReaderCommentCard
            comment={c}
            onChange={(saved) => {
              setItems((s) => s.map((x) => (x.id === saved.id ? saved : x)));
              window.dispatchEvent(new Event('reader-comments-changed'));
            }}
            onDelete={reload}
          />
          {queue && <ReviewActions comment={c} onDone={reload} />}
        </section>
      ))}
      {more && (
        <button
          type="button"
          className="btn btn-secondary"
          disabled={busy}
          onClick={() => {
            setBusy(true);
            setFailure('');
            setOffset((v) => v + 50);
          }}
        >
          Ещё
        </button>
      )}
    </main>
  );
}
export function MyComments() {
  const id = useAuth((s) => s.user?.id);
  return <CommentListInner key={id ?? 'anonymous'} queue={false} />;
}
export function CommentReviewQueue() {
  const id = useAuth((s) => s.user?.id);
  return <CommentListInner key={id ?? 'anonymous'} queue />;
}
