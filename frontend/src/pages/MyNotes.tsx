import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { marginNotesApi } from '../services/api';
import type { MarginNote } from '../types';
import { useAuth } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import { chapterPath, pagePath, workPath } from '../utils/paths';
import { apiErrorMessage } from '../utils/apiError';
import './MyNotes.css';

const PAGE_SIZE = 50;

type Load = { status: 'loading' } | { status: 'ok' } | { status: 'error'; message: string };

/** Адрес места заметки; пустой `search` у главы и тома. */
function noteHref(n: MarginNote): { to: string; missing: boolean; deletedChapter: boolean } {
  const work = { id: n.work_id, slug: n.work_slug };
  if (n.target === 'quote') {
    const page = n.start_page ?? n.snapshot_start_page;
    if (page === null) return { to: workPath(work), missing: true, deletedChapter: false };
    const params = [
      n.quote_key ? `quote=${encodeURIComponent(n.quote_key)}` : '',
      n.quote_tail ? `to=${encodeURIComponent(n.quote_tail)}` : '',
    ].filter(Boolean);
    const search = params.length > 0 ? `?${params.join('&')}` : '';
    return {
      to: `${pagePath(work, page)}${search}`,
      missing: n.start_page === null,
      deletedChapter: false,
    };
  }
  if (n.target === 'chapter') {
    if (n.chapter_id === null) return { to: workPath(work), missing: false, deletedChapter: true };
    return {
      to: chapterPath(work, { id: n.chapter_id }),
      missing: false,
      deletedChapter: false,
    };
  }
  return { to: workPath(work), missing: false, deletedChapter: false };
}

export const MyNotes: React.FC = () => {
  const { isAuthenticated } = useAuth();
  const location = useLocation();
  const [items, setItems] = useState<MarginNote[]>([]);
  const [total, setTotal] = useState(0);
  const [workId, setWorkId] = useState<number | undefined>(undefined);
  const [works, setWorks] = useState<Map<number, string>>(new Map());
  const [load, setLoad] = useState<Load>({ status: 'loading' });
  const [loadingMore, setLoadingMore] = useState(false);
  // Устаревший ответ (быстрая смена фильтра) не должен перезаписать свежий.
  const seq = useRef(0);

  const fetchPage = useCallback((wid: number | undefined, offset: number) => {
    const mine = ++seq.current;
    marginNotesApi
      .mine({ work_id: wid, limit: PAGE_SIZE, offset })
      .then((response) => {
        if (mine !== seq.current) return;
        const got = response.data.items ?? [];
        setItems((prev) => (offset === 0 ? got : [...prev, ...got]));
        setTotal(response.data.total);
        setWorks((prev) => {
          const next = new Map(prev);
          for (const n of got) if (!next.has(n.work_id)) next.set(n.work_id, n.work_title ?? '');
          return next;
        });
        setLoad({ status: 'ok' });
        setLoadingMore(false);
      })
      .catch((err: unknown) => {
        if (mine !== seq.current) return;
        setLoad({ status: 'error', message: apiErrorMessage(err, 'Не удалось загрузить заметки') });
        setLoadingMore(false);
      });
  }, []);

  useEffect(() => {
    if (!isAuthenticated) return;
    fetchPage(undefined, 0);
  }, [isAuthenticated, fetchPage]);

  if (!isAuthenticated) {
    return (
      <div className="my-notes-container">
        <h1>Мои заметки</h1>
        <div className="my-notes-join-invite">
          <p>
            Заметки на полях видны только их автору — чтобы вести их, нужно записаться в читальню.
          </p>
          <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
            Записаться
          </Link>
        </div>
      </div>
    );
  }

  const onFilter = (value: string) => {
    const wid = value === '' ? undefined : Number(value);
    setWorkId(wid);
    setItems([]);
    setLoad({ status: 'loading' });
    fetchPage(wid, 0);
  };

  const onMore = () => {
    setLoadingMore(true);
    fetchPage(workId, items.length);
  };

  return (
    <div className="my-notes-container">
      <h1>Мои заметки</h1>
      <p className="my-notes-hint">
        Заметки на полях видите только вы. <Link to="/help#notes">Как они устроены</Link>
      </p>

      {works.size > 0 && (
        <label className="my-notes-filter">
          <span>Том</span>
          <select
            aria-label="Том"
            value={workId === undefined ? '' : String(workId)}
            onChange={(e) => onFilter(e.target.value)}
          >
            <option value="">Все тома</option>
            {[...works].map(([id, title]) => (
              <option key={id} value={id}>
                {title}
              </option>
            ))}
          </select>
        </label>
      )}

      {load.status === 'loading' && <div className="loading-state">Загрузка…</div>}
      {load.status === 'error' && <div className="error-state">{load.message}</div>}

      {load.status === 'ok' && items.length === 0 && (
        <div className="my-notes-empty">
          <p>Заметок пока нет.</p>
        </div>
      )}

      {items.length > 0 && (
        <ul className="my-notes-list">
          {items.map((n) => {
            const { to, missing, deletedChapter } = noteHref(n);
            const where = n.context_chapter || n.chapter_title;
            const page = n.start_page ?? n.snapshot_start_page;
            return (
              <li key={n.id} className="my-notes-row">
                <div className="my-notes-place">
                  <Link to={to} className="my-notes-link">
                    {[
                      n.work_title,
                      deletedChapter ? '' : where,
                      n.target === 'quote' && page !== null ? `с. ${page}` : '',
                    ]
                      .filter(Boolean)
                      .join(' · ')}
                  </Link>
                  {deletedChapter && (
                    <span className="my-notes-missing">глава не найдена: {n.chapter_title}</span>
                  )}
                  {n.target === 'quote' && missing && (
                    <span className="my-notes-missing">место не найдено</span>
                  )}
                </div>
                {n.target === 'quote' && n.quote_text && (
                  <blockquote className="my-notes-quote">{n.quote_text}</blockquote>
                )}
                <div className="my-notes-body">{n.body}</div>
              </li>
            );
          })}
        </ul>
      )}

      {items.length > 0 && items.length < total && (
        <button type="button" className="btn btn-secondary" disabled={loadingMore} onClick={onMore}>
          Показать ещё
        </button>
      )}
    </div>
  );
};
