import { useCallback, useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { chaptersApi, closuresApi, editionsApi, journalsApi, worksApi } from '../../services/api';
import type { ActiveClosure, ClosureEntry } from '../../types';
import { apiErrorMessage } from '../../utils/apiError';
import { chapterPath, editionPath, journalPath, workPath } from '../../utils/paths';
import './ClosuresAdmin.css';

const PAGE = 100;

const DATE = new Intl.DateTimeFormat('ru-RU', {
  timeZone: 'Europe/Moscow',
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
});

const KIND_LABEL: Record<ClosureEntry['kind'], string> = {
  work: 'том',
  edition: 'собрание',
  journal: 'журнал',
  chapter: 'глава',
};

function entityHref(row: ClosureEntry | ActiveClosure): string | null {
  if (row.kind === 'work') return workPath({ id: row.entity_id });
  if (row.kind === 'edition') return editionPath({ id: row.entity_id });
  if ('slug' in row && row.kind === 'journal' && row.slug) return journalPath({ slug: row.slug });
  if ('work_id' in row && row.kind === 'chapter' && row.work_id) {
    return chapterPath({ id: row.work_id }, { id: row.entity_id });
  }
  return null;
}

function rowKey(row: ActiveClosure): string {
  return `${row.kind}-${row.entity_id}-${row.scope}`;
}

function displayDate(at: string | null): string {
  if (!at) return '—';
  const date = new Date(at);
  return Number.isNaN(date.getTime()) ? '—' : DATE.format(date);
}

async function openClosure(row: ActiveClosure, reason: string): Promise<void> {
  const closed = false;
  if (row.kind === 'chapter') {
    if (row.work_id === null) throw new Error('У главы не указан том');
    if (row.scope === 'scans')
      await chaptersApi.setScansHidden(row.work_id, row.entity_id, closed, reason);
    else await chaptersApi.setClosed(row.work_id, row.entity_id, closed, reason);
    return;
  }
  const api = row.kind === 'work' ? worksApi : row.kind === 'edition' ? editionsApi : journalsApi;
  if (row.scope === 'scans') await api.setScansHidden(row.entity_id, closed, reason);
  else await api.setClosed(row.entity_id, closed, reason);
}

export const ClosuresAdmin: React.FC = () => {
  const [tab, setTab] = useState<'active' | 'journal'>('active');
  const [activeRows, setActiveRows] = useState<ActiveClosure[]>([]);
  const [journalRows, setJournalRows] = useState<ClosureEntry[]>([]);
  const [activeLoading, setActiveLoading] = useState(true);
  const [journalLoading, setJournalLoading] = useState(false);
  const [activeMore, setActiveMore] = useState(false);
  const [journalMore, setJournalMore] = useState(false);
  const [activeAppending, setActiveAppending] = useState(false);
  const [journalAppending, setJournalAppending] = useState(false);
  const [activeError, setActiveError] = useState('');
  const [journalError, setJournalError] = useState('');
  const [opening, setOpening] = useState<Set<string>>(() => new Set());
  const [openErrors, setOpenErrors] = useState<Record<string, string>>({});
  const activeRequest = useRef(0);
  const journalRequest = useRef(0);
  const tabRef = useRef<'active' | 'journal'>('active');
  const openingRef = useRef(new Set<string>());

  const loadActive = useCallback(async (offset: number) => {
    const request = ++activeRequest.current;
    try {
      const { data } = await closuresApi.active({ limit: PAGE, offset });
      if (request !== activeRequest.current) return;
      setActiveRows((prev) => (offset === 0 ? data : [...prev, ...data]));
      setActiveMore(data.length === PAGE);
      setActiveError('');
    } catch (err: unknown) {
      if (request !== activeRequest.current) return;
      setActiveError(apiErrorMessage(err, 'Не удалось прочитать действующие закрытия'));
    } finally {
      if (request === activeRequest.current) {
        setActiveLoading(false);
        setActiveAppending(false);
      }
    }
  }, []);

  const loadJournal = useCallback(async (offset: number) => {
    const request = ++journalRequest.current;
    try {
      const { data } = await closuresApi.list({ limit: PAGE, offset });
      if (request !== journalRequest.current) return;
      setJournalRows((prev) => (offset === 0 ? data : [...prev, ...data]));
      setJournalMore(data.length === PAGE);
      setJournalError('');
    } catch (err: unknown) {
      if (request !== journalRequest.current) return;
      setJournalError(apiErrorMessage(err, 'Не удалось прочитать журнал закрытий'));
    } finally {
      if (request === journalRequest.current) {
        setJournalLoading(false);
        setJournalAppending(false);
      }
    }
  }, []);

  useEffect(() => {
    void loadActive(0);
  }, [loadActive]);

  const selectTab = (next: 'active' | 'journal') => {
    if (tab === next) return;
    tabRef.current = next;
    setTab(next);
    if (next === 'journal') {
      setJournalLoading(true);
      void loadJournal(0);
    } else {
      setActiveLoading(true);
      void loadActive(0);
    }
  };

  const open = async (row: ActiveClosure) => {
    const reason = window.prompt('Причина открытия (обязательно): письмо, от кого, о чём')?.trim();
    if (!reason) return;
    const key = rowKey(row);
    if (openingRef.current.has(key)) return;
    openingRef.current.add(key);
    setOpening(new Set(openingRef.current));
    setOpenErrors((prev) => ({ ...prev, [key]: '' }));
    try {
      await openClosure(row, reason);
      setActiveLoading(tabRef.current === 'active');
      await loadActive(0);
      if (tabRef.current === 'journal') {
        setJournalLoading(true);
        await loadJournal(0);
      }
    } catch (err: unknown) {
      setOpenErrors((prev) => ({ ...prev, [key]: apiErrorMessage(err, 'Не удалось открыть') }));
    } finally {
      openingRef.current.delete(key);
      setOpening(new Set(openingRef.current));
    }
  };

  const isLoading = tab === 'active' ? activeLoading : journalLoading;
  const error = tab === 'active' ? activeError : journalError;

  return (
    <div className="closures-admin">
      <h1>Закрытия</h1>
      <p className="closures-admin-note">
        Ограничения томов, собраний, журналов и глав. Причины видит только администратор.
      </p>
      <div className="closures-admin-tabs" role="tablist" aria-label="Закрытия">
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'active'}
          onClick={() => selectTab('active')}
        >
          Актуальные
        </button>
        <button
          type="button"
          role="tab"
          aria-selected={tab === 'journal'}
          onClick={() => selectTab('journal')}
        >
          Журнал
        </button>
      </div>
      <div role="tabpanel" aria-label={tab === 'active' ? 'Актуальные' : 'Журнал'}>
        {isLoading ? (
          <div className="loading-state">Загрузка…</div>
        ) : error ? (
          <div className="error-state">{error}</div>
        ) : tab === 'active' ? (
          <>
            <p className="closures-admin-note">
              Открытие снимает только указанное ограничение. Другие ограничения могут сохраняться.
            </p>
            {activeRows.length === 0 ? (
              <p>Актуальных закрытий нет.</p>
            ) : (
              <div className="closures-admin-table-scroll">
                <table className="closures-admin-table">
                  <thead>
                    <tr>
                      <th>Что</th>
                      <th>Область</th>
                      <th>Закрыто</th>
                      <th>Причина</th>
                      <th>Кто</th>
                      <th>Действие</th>
                    </tr>
                  </thead>
                  <tbody>
                    {activeRows.map((row) => {
                      const href = entityHref(row);
                      const key = rowKey(row);
                      return (
                        <tr key={key}>
                          <td>
                            <span className="closures-admin-kind">{KIND_LABEL[row.kind]}</span>{' '}
                            {href ? <Link to={href}>{row.title}</Link> : row.title}
                            {row.inherited && (
                              <div className="closures-admin-inherited">
                                После открытия останется ограничен сверху
                              </div>
                            )}
                          </td>
                          <td>{row.scope === 'scans' ? 'Сканы' : 'Всё'}</td>
                          <td>{displayDate(row.at)}</td>
                          <td>{row.reason || '—'}</td>
                          <td>{row.actor_name || '—'}</td>
                          <td>
                            <button
                              type="button"
                              className="btn btn-secondary btn-sm"
                              disabled={opening.has(key)}
                              onClick={() => void open(row)}
                            >
                              Открыть
                            </button>
                            {openErrors[key] && (
                              <div className="closures-admin-row-error" role="alert">
                                {openErrors[key]}
                              </div>
                            )}
                          </td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
            {activeMore && (
              <button
                type="button"
                className="btn btn-secondary"
                disabled={activeAppending}
                onClick={() => {
                  setActiveAppending(true);
                  void loadActive(activeRows.length);
                }}
              >
                Ещё
              </button>
            )}
          </>
        ) : (
          <>
            {journalRows.length === 0 ? (
              <p>Закрытий пока не было.</p>
            ) : (
              <div className="closures-admin-table-scroll">
                <table className="closures-admin-table">
                  <thead>
                    <tr>
                      <th>Когда</th>
                      <th>Что</th>
                      <th>Область</th>
                      <th>Действие</th>
                      <th>Причина</th>
                      <th>Кто</th>
                    </tr>
                  </thead>
                  <tbody>
                    {journalRows.map((row) => {
                      const href = entityHref(row);
                      return (
                        <tr key={`${row.kind}-${row.entity_id}-${row.scope}-${row.at}`}>
                          <td>{DATE.format(new Date(row.at))}</td>
                          <td>
                            <span className="closures-admin-kind">{KIND_LABEL[row.kind]}</span>{' '}
                            {href ? <Link to={href}>{row.title}</Link> : row.title}
                          </td>
                          <td>{row.scope === 'scans' ? 'Сканы' : 'Всё'}</td>
                          <td>
                            {row.scope === 'scans'
                              ? row.closed
                                ? 'скрыты'
                                : 'открыты'
                              : row.closed
                                ? 'закрыто'
                                : 'открыто'}
                          </td>
                          <td>{row.reason}</td>
                          <td>{row.actor_name}</td>
                        </tr>
                      );
                    })}
                  </tbody>
                </table>
              </div>
            )}
            {journalMore && (
              <button
                type="button"
                className="btn btn-secondary"
                disabled={journalAppending}
                onClick={() => {
                  setJournalAppending(true);
                  void loadJournal(journalRows.length);
                }}
              >
                Ещё
              </button>
            )}
          </>
        )}
      </div>
    </div>
  );
};
