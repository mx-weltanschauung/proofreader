import { useEffect, useState } from 'react';
import { useParams, useLocation, Link } from 'react-router-dom';
import { pagesApi, worksApi, suggestionsApi } from '../services/api';
import type { Work } from '../types';
import { getPreviewUrl } from '../utils/url';
import { ScanViewer } from '../components/ScanViewer';
import { MarkdownEditor } from '../components/MarkdownEditor';
import { usePageByNumber } from '../hooks/usePageByNumber';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { printedFolio } from '../utils/folio';
import { pageSha256 } from '../utils/pageSha256';
import { numericId, pagePath, workPath } from '../utils/paths';
import { useAuth } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import './PageSuggest.css';

const NOTE_MAX = 2000;

export const PageSuggest: React.FC = () => {
  // workId остаётся сырым сегментом адреса — используется для ссылок ниже.
  // usePageByNumber сам делает Number(workId) внутри себя, поэтому ему
  // передаётся уже очищенная строка с разобранным номером, а не сырой сегмент
  // со слагом.
  const { workId, pageNumber } = useParams<{ workId: string; pageNumber: string }>();
  const location = useLocation();
  const { isAuthenticated } = useAuth();
  const parsedWorkId = numericId(workId);
  const cleanWorkId = parsedWorkId !== null ? String(parsedWorkId) : undefined;
  const { page, isLoading, notFound, error: loadError } = usePageByNumber(cleanWorkId, pageNumber);
  const [work, setWork] = useState<Work | null>(null);

  const [content, setContent] = useState('');
  const [note, setNote] = useState('');
  const [bindingRef, setBindingRef] = useState('');

  // Основа: sha256 того же текста, который в этот момент лёг в поле — а не
  // пересчитанная из отредактированного текста при отправке, иначе проверка
  // расхождения на сервере ничего бы не ловила.
  const [base, setBase] = useState('');

  const [isSubmitting, setIsSubmitting] = useState(false);
  const [submitError, setSubmitError] = useState('');
  const [conflict, setConflict] = useState(false);
  const [freshText, setFreshText] = useState<string | null>(null);
  const [isRefreshing, setIsRefreshing] = useState(false);
  const [submitted, setSubmitted] = useState(false);

  // Форма — черновик поверх загруженной страницы: пересевается при каждом
  // новом резолве (переходе на другую полосу), но не от последующих
  // изменений состояния внутри самого экрана — иначе «обновить основу» после
  // 409 стирала бы то, что читатель набрал.
  const [prevPage, setPrevPage] = useState(page);
  if (prevPage !== page) {
    setPrevPage(page);
    if (page) {
      setContent(page.content_markdown);
    }
  }

  // Хэш — асинхронный, поэтому считается эффектом, а не в теле рендера;
  // источник тот же `page`, что и предзаполнение поля выше, так что оба
  // берут ровно один и тот же текст.
  useEffect(() => {
    if (!page) return;
    let cancelled = false;
    void pageSha256(page.content_markdown).then((hash) => {
      if (!cancelled) setBase(hash);
    });
    return () => {
      cancelled = true;
    };
  }, [page]);

  useEffect(() => {
    if (parsedWorkId === null) return;
    let cancelled = false;
    worksApi
      .get(parsedWorkId)
      .then((response) => {
        if (!cancelled) setWork(response.data);
      })
      .catch((err: unknown) => {
        console.error('Failed to load work:', err);
      });
    return () => {
      cancelled = true;
    };
  }, [parsedWorkId]);

  const handleSubmit = async () => {
    if (!page) return;

    setIsSubmitting(true);
    setSubmitError('');
    setConflict(false);

    try {
      await suggestionsApi.create(page.work_id, page.id, {
        proposed_markdown: content,
        note,
        base_sha256: base,
        // Живое значение ловушки — пустое у человека, заполненное у бота,
        // который заполняет все поля формы подряд.
        binding_ref: bindingRef,
      });
      setSubmitted(true);
    } catch (err: unknown) {
      if (apiErrorStatus(err) === 409) {
        setConflict(true);
        setSubmitError(apiErrorMessage(err, 'Полосу успели поправить, пока вы её редактировали'));
      } else {
        setSubmitError(apiErrorMessage(err, 'Не удалось отправить предложение'));
      }
    } finally {
      setIsSubmitting(false);
    }
  };

  const handleRefreshBase = async () => {
    if (!page) return;

    setIsRefreshing(true);
    try {
      const response = await pagesApi.getByNumber(page.work_id, page.page_number);
      setFreshText(response.data.content_markdown);
      setBase(await pageSha256(response.data.content_markdown));
    } catch (err: unknown) {
      setSubmitError(apiErrorMessage(err, 'Не удалось обновить основу'));
    } finally {
      setIsRefreshing(false);
    }
  };

  if (!isAuthenticated) {
    return (
      <div className="page-suggest-join-invite">
        <p>Чтобы предложить правку, нужно записаться в читальню.</p>
        <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
          Записаться
        </Link>
      </div>
    );
  }

  if (isLoading) {
    return <div className="loading-state">Загрузка страницы…</div>;
  }

  if (notFound) {
    return (
      <div className="error-state">
        <div>Страница {pageNumber} не найдена в этой работе</div>
        <Link
          to={parsedWorkId !== null ? workPath({ id: parsedWorkId, slug: work?.slug }) : '/'}
          className="back-link"
        >
          ← К работе
        </Link>
      </div>
    );
  }

  if (!page) {
    return <div className="error-state">{loadError || 'Страница не найдена'}</div>;
  }

  const previewUrl = page.preview_url ?? getPreviewUrl(page.preview_path) ?? undefined;

  // У тома offset нулевой — печатный номер совпадает с адресным, дублировать
  // незачем; показываем колонцифру только там, где она расходится с номером
  // страницы, либо там, где полоса вовсе вне печатного счёта («б/н»).
  const folio = work ? printedFolio(page.page_number, work) : null;
  const folioLabel: string | null =
    folio === null ? null : folio !== String(page.page_number) ? `стр. ${folio}` : null;

  return (
    <div className="page-suggest-container">
      <div className="page-suggest-header">
        {work && (
          <Link to={workPath(work)} className="work-link">
            {work.title}
          </Link>
        )}
        {/* Заголовок — вторая крошка пути назад к полосе: читатель, открывший
            форму, теряет место чтения без неё (заводить он его мог только
            кнопкой «назад» браузера). Ссылка ведёт на саму полосу, а не на
            том — том уже доступен строкой выше. */}
        <h1>
          <Link
            to={pagePath({ id: parsedWorkId ?? 0, slug: work?.slug }, page.page_number)}
            className="page-suggest-page-link"
          >
            Страница {page.page_number}
            {folioLabel !== null && <span className="page-suggest-folio"> · {folioLabel}</span>}
          </Link>
        </h1>
        <p className="page-suggest-intro">
          Нашли опечатку или ошибку распознавания? Поправьте текст полосы целиком и отправьте —
          редактор рассмотрит предложение.
        </p>
      </div>

      {submitted ? (
        <div className="page-suggest-success">
          <p>Предложение отправлено. Спасибо!</p>
          <Link to="/mine" className="btn btn-primary btn-sm">
            Мои предложения
          </Link>
        </div>
      ) : (
        <div className="page-suggest-two-columns">
          <div className="page-suggest-left">
            <div className="page-suggest-note-section">
              <label htmlFor="suggest-note">Записка редактору (необязательно):</label>
              <textarea
                id="suggest-note"
                value={note}
                onChange={(e) => setNote(e.target.value)}
                maxLength={NOTE_MAX}
                placeholder="Например: перепутаны похожие буквы в третьем абзаце"
                rows={2}
              />
            </div>

            {/* Ловушка: человек её не видит и табом не достаёт. Имя вне словарей
                автозаполнения — иначе менеджер паролей заполнил бы её сам, и правка
                живого читателя молча пропала бы, получив в ответ «принято». */}
            <input
              type="text"
              name="binding_ref"
              value={bindingRef}
              onChange={(e) => setBindingRef(e.target.value)}
              tabIndex={-1}
              autoComplete="off"
              aria-hidden="true"
              className="suggest-trap"
            />

            <div className="page-suggest-content-section">
              <h3>Текст полосы</h3>
              {/* Корпус держит стихи цитатой с обратной косой в конце строки
                  и сноски как [^N] — сырой markdown чужой читателю разметки
                  легко сломать не глядя. Живой предпросмотр — не для набора
                  символов, а чтобы поломку было видно до отправки. Скан не
                  передаётся сюда отдельным imageUrl: он уже показан справа
                  через ScanViewer, дублировать панель незачем — редактор
                  остаётся в режиме «редактор + предпросмотр». */}
              <MarkdownEditor
                initialContent={content}
                onChange={setContent}
                sanitizeUntrustedContent
              />
            </div>

            {conflict && (
              <div className="page-suggest-conflict">
                <p>{submitError}</p>
                <button
                  type="button"
                  onClick={() => void handleRefreshBase()}
                  disabled={isRefreshing}
                  className="btn btn-secondary btn-sm"
                >
                  {isRefreshing ? 'Обновляем…' : 'Обновить основу'}
                </button>
                {freshText !== null && (
                  <div className="page-suggest-fresh-text">
                    <h4>Актуальный текст на сервере</h4>
                    <p>
                      Основа обновлена. Ваш текст в поле выше не тронут — сравните его с текущим
                      текстом полосы и решите сами, что оставить.
                    </p>
                    <pre>{freshText}</pre>
                  </div>
                )}
              </div>
            )}

            {submitError && !conflict && <div className="page-suggest-error">{submitError}</div>}

            <div className="page-suggest-actions">
              <button
                type="button"
                onClick={() => void handleSubmit()}
                disabled={isSubmitting}
                className="btn btn-primary"
              >
                {isSubmitting ? 'Отправляем…' : 'Предложить правку'}
              </button>
            </div>

            <p className="page-suggest-footnote">
              Сохраняются: текст правки, записка и необратимая отметка вашего адреса — для защиты от
              спама.
            </p>
          </div>

          <div className="page-suggest-right">
            {previewUrl && (
              <div className="page-suggest-preview-section">
                <h3>Скан страницы</h3>
                <ScanViewer src={previewUrl} alt={`Страница ${page.page_number}`} />
              </div>
            )}
          </div>
        </div>
      )}
    </div>
  );
};
