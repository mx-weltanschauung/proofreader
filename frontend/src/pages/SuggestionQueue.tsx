import { useEffect, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import toast from 'react-hot-toast';
import { suggestionsApi, pagesApi } from '../services/api';
import type {
  SuggestionRow,
  SuggestionDetail,
  SuggestionStatus,
  RejectReason,
  Page,
} from '../types';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { printedFolio } from '../utils/folio';
import { getPreviewUrl } from '../utils/url';
import { pagePath } from '../utils/paths';
import { MarkdownDiff } from '../components/MarkdownDiff';
import { MarkdownEditor } from '../components/MarkdownEditor';
import { ScanViewer } from '../components/ScanViewer';
import './SuggestionQueue.css';

const LIMIT = 20;
const CONFLICT_MESSAGE = 'Предложение уже разобрано';

const REJECT_LABEL: Record<RejectReason, string> = {
  так_в_оригинале: 'Так в оригинале',
  уже_исправлено: 'Уже исправлено',
  не_по_теме: 'Не по теме',
};

const STATUS_LABEL: Record<SuggestionStatus, string> = {
  новое: 'Новые',
  принято: 'Принятые',
  отклонено: 'Отклонённые',
};

function formatDelta(delta: number): string {
  return delta > 0 ? `+${delta}` : `${delta}`;
}

/** Ошибка resolve-действия (accept/reject): 409 значит, что предложение уже
 *  разобрал кто-то другой — это не поломка, а гонка редакторов. */
function resolveErrorMessage(err: unknown, fallback: string): string {
  return apiErrorStatus(err) === 409
    ? apiErrorMessage(err, CONFLICT_MESSAGE)
    : apiErrorMessage(err, fallback);
}

type ListState =
  | { status: 'loading' }
  | { status: 'ok'; items: SuggestionRow[]; total: number }
  | { status: 'error'; message: string };

function QueueList() {
  const [filter, setFilter] = useState<SuggestionStatus | ''>('новое');
  const [offset, setOffset] = useState(0);
  const [state, setState] = useState<ListState>({ status: 'loading' });

  // Смена фильтра или страницы — новый запрос: возврат в 'loading' решается
  // сравнением в теле рендера, а не setState в начале эффекта — тот же приём,
  // что usePageByNumber и ConceptList (react-hooks/set-state-in-effect не
  // разрешает синхронный setState в эффекте).
  const [prevParams, setPrevParams] = useState({ filter, offset });
  if (prevParams.filter !== filter || prevParams.offset !== offset) {
    setPrevParams({ filter, offset });
    setState({ status: 'loading' });
  }

  // Счётчик перечитывания: удаление меняет выборку на сервере, а фильтр и
  // смещение при этом те же. Без него список остался бы со строками, которых
  // в базе уже нет, и клик по ним уходил бы в 409.
  const [reloadKey, setReloadKey] = useState(0);
  const [isDeleting, setIsDeleting] = useState(false);

  useEffect(() => {
    let cancelled = false;
    suggestionsApi
      .queue(filter || null, LIMIT, offset)
      .then((response) => {
        if (!cancelled) {
          setState({ status: 'ok', items: response.data.items, total: response.data.total });
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить очередь'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [filter, offset, reloadKey]);

  const isLoading = state.status === 'loading';
  const loadError = state.status === 'error' ? state.message : '';
  const items = state.status === 'ok' ? state.items : [];
  const total = state.status === 'ok' ? state.total : 0;

  const handleRowDelete = async (row: SuggestionRow) => {
    if (!window.confirm(`Удалить отклонённое предложение к стр. ${row.page_number}? Это навсегда.`))
      return;
    setIsDeleting(true);
    try {
      await suggestionsApi.remove(row.id);
      toast.success('Предложение удалено');
      setReloadKey((k) => k + 1);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось удалить предложение'));
    } finally {
      setIsDeleting(false);
    }
  };

  const handlePurge = async () => {
    if (!window.confirm(`Удалить все отклонённые предложения (${total})? Это навсегда.`)) return;
    setIsDeleting(true);
    try {
      const response = await suggestionsApi.purgeRejected();
      toast.success(`Удалено предложений: ${response.data.deleted}`);
      // Хвост списка после чистки заведомо пуст — возвращаемся к началу.
      setOffset(0);
      setReloadKey((k) => k + 1);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось удалить отклонённые предложения'));
    } finally {
      setIsDeleting(false);
    }
  };

  return (
    <div>
      <div className="suggestion-queue-filter">
        <label htmlFor="queue-status-filter">Статус</label>
        <select
          id="queue-status-filter"
          value={filter}
          onChange={(e) => {
            // Смена фильтра — новая выборка, где может быть меньше страниц:
            // без сброса редактор рискует попасть на пустой хвост непустого
            // списка, а не на первую страницу.
            setFilter(e.target.value as SuggestionStatus | '');
            setOffset(0);
          }}
        >
          <option value="">Все</option>
          {(Object.keys(STATUS_LABEL) as SuggestionStatus[]).map((s) => (
            <option key={s} value={s}>
              {STATUS_LABEL[s]}
            </option>
          ))}
        </select>

        {filter === 'отклонено' && items.length > 0 && (
          <button
            type="button"
            className="btn btn-danger btn-sm"
            disabled={isDeleting}
            onClick={() => void handlePurge()}
          >
            Удалить все отклонённые
          </button>
        )}
      </div>

      {isLoading && <div className="loading-state">Загрузка…</div>}
      {loadError && <div className="error-state">{loadError}</div>}

      {!isLoading && !loadError && items.length === 0 && (
        <div className="suggestion-queue-empty">В очереди пока нет предложений.</div>
      )}

      {!isLoading && !loadError && items.length > 0 && (
        <>
          <ul className="suggestion-queue-list">
            {items.map((row) => {
              const folio = printedFolio(row.page_number, { page_offset: row.page_offset });
              const folioLabel =
                folio !== null && folio !== String(row.page_number) ? ` (стр. ${folio})` : '';
              return (
                <li key={row.id} className="suggestion-queue-item">
                  <Link to={`?id=${row.id}`} className="suggestion-queue-row">
                    <span className="suggestion-queue-work">{row.work_title}</span>
                    <span className="suggestion-queue-page">
                      стр. {row.page_number}
                      {folioLabel}
                    </span>
                    <span className="suggestion-queue-date">
                      {new Date(row.created_at).toLocaleDateString()}
                    </span>
                    <span
                      className={
                        row.length_delta >= 0
                          ? 'suggestion-queue-delta suggestion-queue-delta-positive'
                          : 'suggestion-queue-delta suggestion-queue-delta-negative'
                      }
                    >
                      {formatDelta(row.length_delta)}
                    </span>
                    {row.stale && <span className="suggestion-queue-stale">устарело</span>}
                  </Link>
                  {/* Кнопка стоит РЯДОМ со ссылкой, а не внутри неё: кнопка
                      внутри <a> — недопустимая разметка, и клик по ней
                      уводил бы на карточку вместо удаления. */}
                  {row.status === 'отклонено' && (
                    <button
                      type="button"
                      className="btn btn-danger btn-sm suggestion-queue-delete"
                      aria-label="Удалить предложение"
                      title="Удалить предложение"
                      disabled={isDeleting}
                      onClick={() => void handleRowDelete(row)}
                    >
                      ×
                    </button>
                  )}
                </li>
              );
            })}
          </ul>

          <div className="suggestion-queue-pagination">
            <span>
              Показано {offset + 1}–{Math.min(offset + LIMIT, total)} из {total}
            </span>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={offset === 0}
              onClick={() => setOffset(Math.max(0, offset - LIMIT))}
            >
              Назад
            </button>
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              disabled={offset + LIMIT >= total}
              onClick={() => setOffset(offset + LIMIT)}
            >
              Дальше
            </button>
          </div>
        </>
      )}
    </div>
  );
}

type DetailState =
  | { status: 'loading' }
  | { status: 'ok'; detail: SuggestionDetail }
  | { status: 'error'; message: string };

function SuggestionDetailPanel({ id, onBack }: { id: number; onBack: () => void }) {
  const [state, setState] = useState<DetailState>({ status: 'loading' });

  const [prevId, setPrevId] = useState(id);
  if (prevId !== id) {
    setPrevId(id);
    setState({ status: 'loading' });
  }

  useEffect(() => {
    let cancelled = false;
    suggestionsApi
      .get(id)
      .then((response) => {
        if (!cancelled) setState({ status: 'ok', detail: response.data });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить предложение'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [id]);

  const detail = state.status === 'ok' ? state.detail : null;

  // Поле «Принять» стартует с текста читателя — но НЕ на устаревшем
  // предложении: там за время ожидания могла пройти машинная вычитка, и
  // текущий текст полосы старший. Подставить предложенное можно кнопкой —
  // осознанным действием, а не значением по умолчанию.
  const [content, setContent] = useState('');
  const [prevDetail, setPrevDetail] = useState<SuggestionDetail | null>(null);
  if (detail !== prevDetail) {
    setPrevDetail(detail);
    if (detail) setContent(detail.stale ? detail.current_markdown : detail.proposed_markdown);
  }

  const [page, setPage] = useState<Page | null>(null);
  useEffect(() => {
    if (!detail) return;
    let cancelled = false;
    pagesApi
      .get(detail.work_id, detail.page_id)
      .then((response) => {
        if (!cancelled) setPage(response.data);
      })
      .catch((err: unknown) => {
        // Скан — вспомогательная панель; его отсутствие не должно мешать
        // разбору предложения по тексту.
        console.error('Failed to load page scan:', err);
      });
    return () => {
      cancelled = true;
    };
  }, [detail]);

  const [comment, setComment] = useState('');
  const [rejectReason, setRejectReason] = useState<RejectReason | ''>('');
  const [isSubmitting, setIsSubmitting] = useState(false);
  const [actionError, setActionError] = useState('');

  const handleAccept = async () => {
    setIsSubmitting(true);
    setActionError('');
    try {
      await suggestionsApi.accept(id, content, comment);
      toast.success('Предложение принято');
      onBack();
    } catch (err: unknown) {
      const message = resolveErrorMessage(err, 'Не удалось принять предложение');
      if (apiErrorStatus(err) === 409) {
        toast.error(message);
        onBack();
      } else {
        setActionError(message);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleReject = async () => {
    if (!rejectReason) return;
    setIsSubmitting(true);
    setActionError('');
    try {
      await suggestionsApi.reject(id, rejectReason);
      toast.success('Предложение отклонено');
      onBack();
    } catch (err: unknown) {
      const message = resolveErrorMessage(err, 'Не удалось отклонить предложение');
      if (apiErrorStatus(err) === 409) {
        toast.error(message);
        onBack();
      } else {
        setActionError(message);
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  // Удалить можно только отклонённое, и удаление физическое: строка исчезнет
  // и из «Моих предложений» читателя. Для мусора и спама это и есть цель.
  const handleDelete = async () => {
    if (!window.confirm('Удалить отклонённое предложение? Это навсегда.')) return;
    setIsSubmitting(true);
    setActionError('');
    try {
      await suggestionsApi.remove(id);
      toast.success('Предложение удалено');
      onBack();
    } catch (err: unknown) {
      setActionError(apiErrorMessage(err, 'Не удалось удалить предложение'));
    } finally {
      setIsSubmitting(false);
    }
  };

  if (state.status === 'loading') {
    return <div className="loading-state">Загрузка…</div>;
  }
  if (state.status === 'error') {
    return <div className="error-state">{state.message}</div>;
  }
  if (!detail) return null;

  const folio = printedFolio(detail.page_number, { page_offset: detail.page_offset });
  const folioLabel =
    folio !== null && folio !== String(detail.page_number) ? ` (стр. ${folio})` : '';
  const previewUrl = page
    ? (page.preview_url ?? getPreviewUrl(page.preview_path) ?? undefined)
    : undefined;

  // Разобранное предложение — не рабочее место: принять/отклонить его
  // второй раз сервер откажет 409-м, а поле «Итоговый текст» тут никогда не
  // покажет применённого — оно вообще не хранится в предложении (accept
  // пишет в полосу текст из формы редактора, не proposed_markdown дословно).
  const isDecided = detail.status !== 'новое';
  // SuggestionDetail (page_suggestion_repository.go) не несёт work_slug —
  // такого поля нет нигде в цепочке предложений. Адрес выходит голым номером.
  const pageHref = pagePath({ id: detail.work_id, slug: undefined }, detail.page_number);

  return (
    <div className="suggestion-queue-detail">
      <button type="button" className="btn btn-secondary btn-sm" onClick={onBack}>
        ← К очереди
      </button>

      <h2>
        {detail.work_title} · стр. {detail.page_number}
        {folioLabel}
      </h2>

      {detail.note && (
        <div className="suggestion-queue-note">
          <h3>Записка читателя</h3>
          <p>{detail.note}</p>
        </div>
      )}

      <div className="suggestion-queue-two-columns">
        <div className="suggestion-queue-left">
          {isDecided ? (
            <>
              <div className="suggestion-queue-note">
                <h3>Решение</h3>
                <p>
                  {detail.status === 'принято' ? 'Принято' : 'Отклонено'}
                  {detail.resolved_at && ` · ${new Date(detail.resolved_at).toLocaleString()}`}
                </p>
                {detail.status === 'отклонено' && detail.reject_reason && (
                  <p>Причина: {REJECT_LABEL[detail.reject_reason]}</p>
                )}
                {detail.status === 'принято' && (
                  <p>
                    Ниже — то, что прислал читатель, не обязательно то, что редактор применил
                    дословно: итоговый текст в самом предложении не хранится.{' '}
                    <Link to={pageHref}>Открыть применённый текст на полосе</Link>.
                  </p>
                )}
                {detail.status === 'отклонено' && (
                  <button
                    type="button"
                    className="btn btn-danger btn-sm"
                    disabled={isSubmitting}
                    onClick={() => void handleDelete()}
                  >
                    Удалить
                  </button>
                )}
                {actionError && <div className="suggestion-queue-error">{actionError}</div>}
              </div>
              <h3>
                {detail.status === 'принято' ? 'Что предложил читатель' : 'Предложенная правка'}
              </h3>
              <MarkdownDiff before={detail.base_markdown} after={detail.proposed_markdown} />
            </>
          ) : (
            <>
              {detail.stale ? (
                <>
                  <div className="suggestion-queue-stale-warning">
                    Полоса изменилась, пока предложение ждало в очереди — вероятно, прошёл прогон
                    машинной вычитки. Поле ниже предзаполнено текущим текстом полосы, а не тем, что
                    прислал читатель.
                  </div>
                  <h3>Предложил читатель</h3>
                  <MarkdownDiff before={detail.base_markdown} after={detail.proposed_markdown} />
                  <h3>Изменилось тем временем</h3>
                  <MarkdownDiff before={detail.base_markdown} after={detail.current_markdown} />
                  <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    onClick={() => setContent(detail.proposed_markdown)}
                  >
                    Подставить предложенное
                  </button>
                </>
              ) : (
                <>
                  <h3>Предложенная правка</h3>
                  <MarkdownDiff before={detail.base_markdown} after={detail.proposed_markdown} />
                </>
              )}

              <div className="suggestion-queue-editor">
                <h3>Итоговый текст</h3>
                <MarkdownEditor
                  initialContent={content}
                  onChange={setContent}
                  sanitizeUntrustedContent
                />
              </div>

              <div className="suggestion-queue-comment">
                <label htmlFor="queue-comment">Комментарий (необязательно)</label>
                <textarea
                  id="queue-comment"
                  value={comment}
                  onChange={(e) => setComment(e.target.value)}
                  rows={2}
                />
              </div>

              {actionError && <div className="suggestion-queue-error">{actionError}</div>}

              <div className="suggestion-queue-actions">
                <button
                  type="button"
                  className="btn btn-primary"
                  disabled={isSubmitting}
                  onClick={() => void handleAccept()}
                >
                  Принять
                </button>

                <select
                  id="queue-reject-reason"
                  aria-label="Причина отказа"
                  value={rejectReason}
                  onChange={(e) => setRejectReason(e.target.value as RejectReason | '')}
                  disabled={isSubmitting}
                >
                  <option value="">Причина отказа…</option>
                  {(Object.keys(REJECT_LABEL) as RejectReason[]).map((r) => (
                    <option key={r} value={r}>
                      {REJECT_LABEL[r]}
                    </option>
                  ))}
                </select>
                <button
                  type="button"
                  className="btn btn-secondary"
                  disabled={isSubmitting || !rejectReason}
                  onClick={() => void handleReject()}
                >
                  Отклонить
                </button>
              </div>
            </>
          )}
        </div>

        <div className="suggestion-queue-right">
          {previewUrl && (
            <div className="suggestion-queue-preview-section">
              <h3>Скан полосы</h3>
              <ScanViewer src={previewUrl} alt={`Страница ${detail.page_number}`} />
            </div>
          )}
        </div>
      </div>
    </div>
  );
}

/**
 * Очередь модерации читательских предложений. Список и разбор одного
 * предложения живут на одном маршруте (`/suggestions/queue`): выбранная
 * запись держится в query-параметре `id`, а не во вложенном маршруте — это
 * позволяет дать редактору прямую ссылку на конкретное предложение.
 */
export const SuggestionQueue: React.FC = () => {
  const [searchParams, setSearchParams] = useSearchParams();
  const idParam = searchParams.get('id');
  const selectedId = idParam ? Number(idParam) : null;

  const goBack = () => {
    const next = new URLSearchParams(searchParams);
    next.delete('id');
    setSearchParams(next);
  };

  return (
    <div className="suggestion-queue-container">
      <h1>Очередь предложений</h1>
      {selectedId !== null ? (
        <SuggestionDetailPanel id={selectedId} onBack={goBack} />
      ) : (
        <QueueList />
      )}
    </div>
  );
};
