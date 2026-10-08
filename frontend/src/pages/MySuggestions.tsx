import { useEffect, useState } from 'react';
import { Link, useLocation, useNavigate } from 'react-router-dom';
import toast from 'react-hot-toast';
import { suggestionsApi, collectionsApi, documentsApi, authApi } from '../services/api';
import type { Collection, Document, RejectReason, SuggestionRow } from '../types';
import { useAuth } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import { collectionPath } from '../utils/paths';
import { documentPath } from '../utils/documentPaths';
import { printedFolio } from '../utils/folio';
import { apiErrorMessage } from '../utils/apiError';
import { documentStateLabel, DOCUMENT_REJECT_LABEL } from '../utils/documentLabels';
import './MySuggestions.css';

const REJECT_LABEL: Record<RejectReason, string> = {
  так_в_оригинале: 'Так в оригинале',
  уже_исправлено: 'Уже исправлено',
  не_по_теме: 'Не по теме',
};

const STATUS_LABEL: Record<SuggestionRow['status'], string> = {
  новое: 'Рассматривается',
  принято: 'Принято',
  отклонено: 'Отклонено',
};

// Одно поле состояния вместо isLoading/rows/error порознь: у резолва нет
// промежуточных комбинаций (загрузка одновременно с уже показанным списком
// не бывает), а раздельные setState в эффекте — та же ловушка, что чинил
// usePageByNumber: react-hooks запрещает синхронный setState в теле эффекта,
// сброс на «loading» при смене статуса входа делается во время рендера, а не
// в эффекте.
type FetchState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok'; rows: SuggestionRow[] }
  | { status: 'error'; message: string };

// Тот же резолв, что у списка правок, только с подборками — собственный
// «идентификатор» вместо rows, чтобы два раздела не путались типами.
type CollectionsFetchState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok'; collections: Collection[] }
  | { status: 'error'; message: string };

// Третий раздел, тот же приём: свой резолв вместо смешивания с подборками —
// у разборов своя форма состояния (review_status/published_at), которая
// подборкам не нужна.
type DocumentsFetchState =
  | { status: 'idle' }
  | { status: 'loading' }
  | { status: 'ok'; documents: Document[] }
  | { status: 'error'; message: string };

export const MySuggestions: React.FC = () => {
  const { isAuthenticated, user, logout } = useAuth();
  const location = useLocation();
  const navigate = useNavigate();
  const [confirmNick, setConfirmNick] = useState('');
  const [isLeaving, setIsLeaving] = useState(false);
  const [state, setState] = useState<FetchState>(() =>
    isAuthenticated ? { status: 'loading' } : { status: 'idle' },
  );
  const [collectionsState, setCollectionsState] = useState<CollectionsFetchState>(() =>
    isAuthenticated ? { status: 'loading' } : { status: 'idle' },
  );
  const [documentsState, setDocumentsState] = useState<DocumentsFetchState>(() =>
    isAuthenticated ? { status: 'loading' } : { status: 'idle' },
  );

  // Смена статуса входа (вошёл/вышел, не покидая страницу) должна сразу
  // вернуть состояние к «свежему» резолву — тот же приём, что в
  // usePageByNumber.
  const [prevAuthenticated, setPrevAuthenticated] = useState(isAuthenticated);
  if (prevAuthenticated !== isAuthenticated) {
    setPrevAuthenticated(isAuthenticated);
    setState(isAuthenticated ? { status: 'loading' } : { status: 'idle' });
    setCollectionsState(isAuthenticated ? { status: 'loading' } : { status: 'idle' });
    setDocumentsState(isAuthenticated ? { status: 'loading' } : { status: 'idle' });
  }

  useEffect(() => {
    if (!isAuthenticated) return;
    let cancelled = false;
    suggestionsApi
      .mine()
      .then((response) => {
        if (!cancelled) setState({ status: 'ok', rows: response.data });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить список правок'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [isAuthenticated]);

  useEffect(() => {
    if (!isAuthenticated) return;
    let cancelled = false;
    collectionsApi
      .mine()
      .then((response) => {
        if (!cancelled) setCollectionsState({ status: 'ok', collections: response.data ?? [] });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setCollectionsState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить список подборок'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [isAuthenticated]);

  useEffect(() => {
    if (!isAuthenticated) return;
    let cancelled = false;
    documentsApi
      .mine()
      .then((response) => {
        if (!cancelled) setDocumentsState({ status: 'ok', documents: response.data ?? [] });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setDocumentsState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить список разборов'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [isAuthenticated]);

  // Пустое поле не подтверждает НИЧЕГО и НИКОГДА — даже если бы у вошедшего
  // не оказалось ника (схема сегодня этого не допускает, но тип User.nickname
  // необязателен, и запасное значение `?? ''` в сравнении со строкой ввода
  // иначе совпало бы на двух пустых строках, включив необратимую кнопку при
  // незаполненном поле). Явная проверка на непустой user.nickname держит это
  // намерение видимым, а не полагается на то, что поле ввода и так пусто.
  const nicknameConfirmed = !!user?.nickname && confirmNick.trim() === user.nickname;

  // Уход из читальни: необратимо (почты у читателя нет — не с чего слать
  // восстановление), поэтому подтверждение — набор своего ника, а не
  // модальное «вы уверены?». Токен сервер не отзывает (см. DeleteMe),
  // поэтому клиент обязан сам выбросить его — logout() — сразу после успеха.
  const onLeave = async () => {
    setIsLeaving(true);
    try {
      await authApi.deleteMe();
      logout();
      toast.success('Учётная запись удалена');
      navigate('/');
    } catch (err: unknown) {
      setIsLeaving(false);
      toast.error(apiErrorMessage(err, 'Не удалось удалить учётную запись'));
    }
  };

  const isLoading = state.status === 'loading';
  const loadError = state.status === 'error' ? state.message : '';
  const rows = state.status === 'ok' ? state.rows : [];

  const isCollectionsLoading = collectionsState.status === 'loading';
  const collectionsError = collectionsState.status === 'error' ? collectionsState.message : '';
  const collections = collectionsState.status === 'ok' ? collectionsState.collections : [];

  const isDocumentsLoading = documentsState.status === 'loading';
  const documentsError = documentsState.status === 'error' ? documentsState.message : '';
  const documents = documentsState.status === 'ok' ? documentsState.documents : [];

  if (!isAuthenticated) {
    return (
      <div className="my-suggestions-container">
        <h1>Моё</h1>
        <div className="my-suggestions-join-invite">
          <p>Чтобы увидеть судьбу своих правок, нужно записаться в читальню.</p>
          <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
            Записаться
          </Link>
        </div>
      </div>
    );
  }

  return (
    <div className="my-suggestions-container">
      <h1>Моё</h1>

      {isLoading && <div className="loading-state">Загрузка…</div>}
      {loadError && <div className="error-state">{loadError}</div>}

      {!isLoading && !loadError && rows.length > 0 && (
        <ul className="my-suggestions-list">
          {rows.map((row) => {
            const folio = printedFolio(row.page_number, { page_offset: row.page_offset });
            const folioLabel =
              folio !== null && folio !== String(row.page_number) ? ` (стр. ${folio})` : '';
            return (
              <li key={row.id} className="my-suggestions-row">
                <div className="my-suggestions-row-main">
                  <span className="my-suggestions-work">{row.work_title}</span>
                  <span className="my-suggestions-page">
                    стр. {row.page_number}
                    {folioLabel}
                  </span>
                  <span className="my-suggestions-date">
                    {new Date(row.created_at).toLocaleDateString()}
                  </span>
                  <span className={`my-suggestions-status my-suggestions-status-${row.status}`}>
                    {STATUS_LABEL[row.status]}
                  </span>
                </div>
                {row.status === 'отклонено' && row.reject_reason && (
                  <div className="my-suggestions-reject-reason">
                    Причина: {REJECT_LABEL[row.reject_reason]}
                  </div>
                )}
              </li>
            );
          })}
        </ul>
      )}

      {!isLoading && !loadError && rows.length === 0 && (
        <div className="my-suggestions-empty">
          <p>У вас пока нет предложенных правок.</p>
        </div>
      )}

      <h2 className="my-suggestions-section-title">Мои подборки</h2>

      {isCollectionsLoading && <div className="loading-state">Загрузка…</div>}
      {collectionsError && <div className="error-state">{collectionsError}</div>}

      {!isCollectionsLoading && !collectionsError && collections.length > 0 && (
        <ul className="my-suggestions-list">
          {collections.map((collection) => (
            <li key={collection.id} className="my-suggestions-row">
              <div className="my-suggestions-row-main">
                <Link to={collectionPath(collection)} className="my-suggestions-work">
                  {collection.title}
                </Link>
                <span
                  className={
                    collection.published_at
                      ? 'my-suggestions-status my-suggestions-status-принято'
                      : 'my-suggestions-status my-suggestions-status-новое'
                  }
                >
                  {collection.published_at ? 'опубликовано' : 'черновик'}
                </span>
              </div>
            </li>
          ))}
        </ul>
      )}

      {!isCollectionsLoading && !collectionsError && collections.length === 0 && (
        <div className="my-suggestions-empty">
          <p>Подборок пока нет.</p>
          <Link to="/collections/new" className="btn btn-primary">
            Создать подборку
          </Link>
        </div>
      )}

      <h2 className="my-suggestions-section-title">Мои разборы</h2>

      {isDocumentsLoading && <div className="loading-state">Загрузка…</div>}
      {documentsError && <div className="error-state">{documentsError}</div>}

      {!isDocumentsLoading && !documentsError && documents.length > 0 && (
        <ul className="my-suggestions-list">
          {documents.map((document) => (
            <li key={document.id} className="my-suggestions-row">
              <div className="my-suggestions-row-main">
                <Link to={documentPath(document)} className="my-suggestions-work">
                  {document.title}
                </Link>
                <span className="my-suggestions-status">{documentStateLabel(document)}</span>
              </div>
              {document.review_status === 'отклонено' && document.reject_reason && (
                <div className="my-suggestions-reject-reason">
                  Причина: {DOCUMENT_REJECT_LABEL[document.reject_reason]}
                </div>
              )}
            </li>
          ))}
        </ul>
      )}

      {!isDocumentsLoading && !documentsError && documents.length === 0 && (
        <div className="my-suggestions-empty">
          <p>Разборов пока нет.</p>
          <Link to="/documents/new" className="btn btn-primary">
            Создать разбор
          </Link>
        </div>
      )}

      {user?.role === 'reader' && (
        <section className="mine-section mine-leave">
          <h2 className="my-suggestions-section-title">Уйти из читальни</h2>
          <p>
            Учётная запись будет удалена без возможности восстановления: почты мы не собираем, и
            вернуть её нечем. Опубликованное — подборки, разборы и предложенные правки — останется в
            читальне под вашей подписью, но править это не сможет уже никто: ни вы, ни редакция.
          </p>
          {/* Обещание правится по факту (задача 02 в .scratch/razbor-vkleyka/issues):
              неопубликованный черновик разбора после ухода не виден никому и не
              удаляется никем — административного пути к разборам в читальне нет.
              Прежний текст обещал, что «останется под подписью» всё, и про
              черновик это было неверно. */}
          <p>
            Неопубликованный черновик разбора — другое дело: его не увидит больше никто, включая
            вас, и снять его будет уже нельзя. Если такой черновик вам не нужен, удалите его до
            ухода.
          </p>
          {/* Ник НЕ освобождается (I4): под ним в читальне остались тексты, и
              занять его заново значило бы подписать чужое чужим именем. */}
          <p>
            Ваш ник останется занятым навсегда: ни вы, ни кто-либо другой не сможет записаться под
            ним снова — под этим именем в читальне остались тексты.
          </p>
          <label className="mine-leave-confirm-label" htmlFor="mine-leave-confirm-nickname">
            Чтобы подтвердить, наберите свой ник:
          </label>
          <input
            id="mine-leave-confirm-nickname"
            type="text"
            className="mine-leave-confirm-input"
            value={confirmNick}
            onChange={(e) => setConfirmNick(e.target.value)}
            disabled={isLeaving}
          />
          <button
            type="button"
            className="btn btn-danger mine-leave-button"
            disabled={isLeaving || !nicknameConfirmed}
            onClick={() => void onLeave()}
          >
            Удалить учётную запись
          </button>
        </section>
      )}
    </div>
  );
};
