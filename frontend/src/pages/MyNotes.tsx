import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { marginNotesApi, readerCommentsApi } from '../services/api';
import type { MarginNote } from '../types';
import { useAuth } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import { noteHref } from '../utils/marginNoteHref';
import { apiErrorMessage } from '../utils/apiError';
import { printedFolio } from '../utils/folio';
import { notesMarkdown } from '../utils/notesMarkdown';
import './MyNotes.css';

const PAGE_SIZE = 50;

type Load = { status: 'loading' } | { status: 'ok' } | { status: 'error'; message: string };

export const MyNotes: React.FC = () => {
  const { isAuthenticated, user } = useAuth();
  const location = useLocation();

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

  return <NotesList key={user?.id} />;
};

const NotesList: React.FC = () => {
  const [items, setItems] = useState<MarginNote[]>([]);
  const [total, setTotal] = useState(0);
  const [workId, setWorkId] = useState<number | undefined>();
  const [works, setWorks] = useState<{ id: number; title: string }[]>([]);
  const [search, setSearch] = useState('');
  const query = search.trim();
  const [load, setLoad] = useState<Load>({ status: 'loading' });
  const [loadingMore, setLoadingMore] = useState(false);
  const [selected, setSelected] = useState<Set<number>>(new Set());
  const [downloading, setDownloading] = useState(false);
  const [exportError, setExportError] = useState('');
  const seq = useRef(0);
  const invalidate = useCallback(() => {
    seq.current++;
  }, []);

  const fetchPage = useCallback((wid: number | undefined, q: string, offset: number) => {
    const mine = ++seq.current;
    marginNotesApi
      .mine({ work_id: wid, limit: PAGE_SIZE, offset, ...(q ? { q } : {}) })
      .then(({ data }) => {
        if (mine !== seq.current) return;
        const got = data.items ?? [];
        setItems((prev) => (offset === 0 ? got : [...prev, ...got]));
        setTotal(data.total);
        if (data.works) setWorks(data.works);
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
    const timer = window.setTimeout(() => fetchPage(workId, query, 0), query ? 250 : 0);
    return () => {
      window.clearTimeout(timer);
      invalidate();
    };
  }, [workId, query, fetchPage, invalidate]);

  const resetList = () => {
    invalidate();
    setItems([]);
    setLoad({ status: 'loading' });
    setLoadingMore(false);
  };

  const onMore = () => {
    setLoadingMore(true);
    fetchPage(workId, query, items.length);
  };

  const toggle = (id: number) => {
    setSelected((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
    setExportError('');
  };

  const download = async () => {
    const userId = useAuth.getState().user?.id;
    setExportError('');
    setDownloading(true);
    try {
      const { data } = await marginNotesApi.forExport([...selected]);
      const markdown = await notesMarkdown(data, window.location.origin);
      if (useAuth.getState().user?.id !== userId) return;
      const url = URL.createObjectURL(
        new Blob([markdown], { type: 'text/markdown;charset=utf-8' }),
      );
      const link = document.createElement('a');
      link.href = url;
      link.download = 'my-notes.md';
      document.body.appendChild(link);
      link.click();
      link.remove();
      window.setTimeout(() => URL.revokeObjectURL(url), 1000);
    } catch (err) {
      setExportError(apiErrorMessage(err, 'Не удалось скачать заметки'));
    } finally {
      setDownloading(false);
    }
  };

  return (
    <div className="my-notes-container">
      <h1>Мои заметки</h1>
      <p>
        <Link to="/mine/comments">Мои комментарии</Link>
      </p>
      <p className="my-notes-hint">
        Заметки на полях видите только вы. <Link to="/help#notes">Как они устроены</Link>
      </p>

      <div className="my-notes-controls">
        <label className="my-notes-filter">
          <span>Поиск по заметкам и цитатам</span>
          <input
            type="search"
            value={search}
            onChange={(e) => {
              if (e.target.value.trim() !== query) resetList();
              setSearch(e.target.value);
            }}
          />
        </label>
        {(works.length > 0 || workId !== undefined) && (
          <label className="my-notes-filter">
            <span>Том</span>
            <select
              value={workId ?? ''}
              onChange={(e) => {
                resetList();
                setWorkId(e.target.value ? Number(e.target.value) : undefined);
              }}
            >
              <option value="">Все тома</option>
              {workId !== undefined && !works.some((work) => work.id === workId) && (
                <option value={workId}>Недоступный том</option>
              )}
              {works.map(({ id, title }) => (
                <option key={id} value={id}>
                  {title}
                </option>
              ))}
            </select>
          </label>
        )}
      </div>

      <div className="my-notes-selection">
        <span role="status">Выбрано: {selected.size}</span>
        <button
          type="button"
          className="btn btn-primary"
          disabled={!selected.size || downloading}
          onClick={() => void download()}
        >
          {downloading ? 'Подготовка…' : 'Скачать Markdown'}
        </button>
        {selected.size > 0 && (
          <button
            type="button"
            className="btn btn-secondary"
            onClick={() => {
              setSelected(new Set());
              setExportError('');
            }}
          >
            Снять выбор
          </button>
        )}
      </div>
      {exportError && (
        <div role="alert" className="error-state">
          {exportError}
        </div>
      )}

      {load.status === 'loading' && <div className="loading-state">Загрузка…</div>}
      {load.status === 'error' && <div className="error-state">{load.message}</div>}

      {load.status === 'ok' && items.length === 0 && (
        <div className="my-notes-empty">
          <p>{query || workId ? 'Заметки не найдены.' : 'Заметок пока нет.'}</p>
        </div>
      )}

      {items.length > 0 && (
        <ul className="my-notes-list">
          {items.map((n) => {
            const { to, missing, deletedChapter } = noteHref(n);
            const where = n.context_chapter || n.chapter_title;
            const page = n.start_page ?? n.snapshot_start_page;
            // Печатный номер, как на карточке полосы; у полосы вне счёта
            // (обложка, форзац) колонцифры нет — тогда номер из адреса.
            const folio =
              page === null
                ? null
                : (printedFolio(page, {
                    page_offset: n.page_offset ?? 0,
                    numbering_style: n.numbering_style,
                  }) ?? String(page));
            return (
              <li key={n.id} className="my-notes-row">
                <div className="my-notes-place">
                  <input
                    type="checkbox"
                    aria-label={`Выбрать заметку: ${n.body.slice(0, 80)}`}
                    checked={selected.has(n.id)}
                    onChange={() => toggle(n.id)}
                  />
                  <Link to={to} className="my-notes-link">
                    {[
                      n.work_title,
                      deletedChapter ? '' : where,
                      n.target === 'quote' && folio !== null ? `с. ${folio}` : '',
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
                {Array.from(n.body).length <= 2000 && (
                  <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    onClick={() => {
                      const request = seq.current;
                      void readerCommentsApi
                        .create(n.work_id, { note_id: n.id, body: n.body })
                        .then(() => {
                          if (request === seq.current) {
                            setItems((s) => s.filter((x) => x.id !== n.id));
                            setTotal((v) => v - 1);
                          }
                          setSelected((prev) => {
                            const next = new Set(prev);
                            next.delete(n.id);
                            return next;
                          });
                          window.dispatchEvent(new Event('reader-comments-changed'));
                        })
                        .catch((e) =>
                          setLoad({
                            status: 'error',
                            message: apiErrorMessage(e, 'Не удалось отправить комментарий'),
                          }),
                        );
                    }}
                  >
                    Сделать комментарием
                  </button>
                )}
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
