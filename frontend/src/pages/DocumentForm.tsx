import { useState, useEffect, useRef } from 'react';
import { useNavigate, useParams, useLocation, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { documentsApi } from '../services/api';
import { MarkdownEditor } from '../components/MarkdownEditor';
import { CutPicker } from '../components/CutPicker';
import { apiErrorMessage } from '../utils/apiError';
import { useAuth, canEditDocument } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import { documentStateLabel, DOCUMENT_REJECT_LABEL } from '../utils/documentLabels';
import { documentPath } from '../utils/documentPaths';
import type { Document } from '../types';
import './DocumentForm.css';

export const DocumentForm: React.FC = () => {
  const navigate = useNavigate();
  const location = useLocation();
  const { nickname, slug } = useParams<{ nickname?: string; slug: string }>();
  const { isAuthenticated, user } = useAuth();
  const isEdit = !!slug;
  const [title, setTitle] = useState('');
  const [markdownContent, setMarkdownContent] = useState('');
  const [isLoading, setIsLoading] = useState(isEdit);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');
  const [notice, setNotice] = useState('');
  const [showCutPicker, setShowCutPicker] = useState(false);
  // Полный документ с сервера — источник полей состояния (review_status,
  // published_at, reject_reason и т.д.), которых нет в title/markdownContent.
  const [doc, setDoc] = useState<Document | null>(null);
  const [isReviewActionPending, setIsReviewActionPending] = useState(false);
  // MarkdownEditor держит value в своём состоянии и не отдаёт наружу ref на
  // textarea, но сама обёртка вокруг него — наша: querySelector достаёт
  // внутренний <textarea> через границу компонента, без правки MarkdownEditor.tsx.
  const markdownWrapperRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    if (!isEdit || !slug) return;

    const loadDocument = async () => {
      try {
        const response = await documentsApi.get({ nickname, slug });
        setDoc(response.data);
        setTitle(response.data.title);
        setMarkdownContent(response.data.markdown_content);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить документ'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadDocument();
  }, [isEdit, nickname, slug]);

  const handleSubmit = async () => {
    if (!title.trim()) {
      setError('Нужно заглавие');
      return;
    }

    setError('');
    setNotice('');
    setIsSaving(true);

    try {
      if (isEdit && slug) {
        const wasQueued = doc?.review_status === 'на_рассмотрении';
        const response = await documentsApi.update(
          { nickname, slug },
          {
            title,
            markdown_content: markdownContent,
          },
        );
        setDoc(response.data);
        toast.success('Документ изменён!');
        // Update() на сервере молча откатывает «на_рассмотрении» в
        // «черновик» при любой правке тела — интерфейс обязан сказать об
        // этом автору, а не промолчать: иначе заявка исчезает из очереди
        // модератора, и автор гадает, почему. Остаёмся на форме, а не
        // уводим автора на страницу просмотра, — иначе предупреждение
        // мелькнёт и пропадёт незамеченным.
        if (wasQueued && response.data.review_status !== 'на_рассмотрении') {
          setNotice(
            'Правка вернула разбор в черновики: чтобы он снова встал в очередь, ' +
              'отправьте его на проверку снова.',
          );
          return;
        }
        navigate(documentPath(response.data));
      } else {
        const response = await documentsApi.create({
          title,
          markdown_content: markdownContent,
        });
        toast.success('Документ создан!');
        navigate(documentPath(response.data));
      }
    } catch (err: unknown) {
      setError(
        apiErrorMessage(
          err,
          isEdit ? 'Не удалось сохранить документ' : 'Не удалось создать документ',
        ),
      );
    } finally {
      setIsSaving(false);
    }
  };

  const handleSubmitForReview = async () => {
    if (!slug) return;
    setError('');
    setNotice('');
    setIsReviewActionPending(true);
    try {
      const response = await documentsApi.submit({ nickname, slug });
      setDoc(response.data);
      toast.success('Разбор отправлен на проверку');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось отправить разбор на проверку'));
    } finally {
      setIsReviewActionPending(false);
    }
  };

  const handleUnpublish = async () => {
    if (!slug) return;
    setError('');
    setNotice('');
    setIsReviewActionPending(true);
    try {
      const response = await documentsApi.unpublish({ nickname, slug });
      // Полоса состояния (documentStateLabel) сама скажет «Разбор снят с
      // публикации» для нового doc — отдельное уведомление тут было бы
      // дублем тех же слов.
      setDoc(response.data);
      toast.success('Разбор снят с публикации');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось снять разбор с публикации'));
    } finally {
      setIsReviewActionPending(false);
    }
  };

  // Тег обязан встать ОТДЕЛЬНЫМ абзацем: внутри строки парсер разобрал бы
  // его как встроенный HTML, и сервер такую вклейку отклонит. Вставка — на
  // место курсора, а не в конец: textarea прячется внутри MarkdownEditor,
  // поэтому курсор читается через querySelector на обёртке, которую держит
  // сама DocumentForm.
  const insertCutTag = (cutId: number) => {
    const textarea = markdownWrapperRef.current?.querySelector('textarea');
    const tag = `\n\n<cut id="${cutId}">\n\n`;
    if (textarea) {
      const start = textarea.selectionStart ?? markdownContent.length;
      const end = textarea.selectionEnd ?? start;
      setMarkdownContent(markdownContent.slice(0, start) + tag + markdownContent.slice(end));
    } else {
      setMarkdownContent(markdownContent + tag);
    }
    setShowCutPicker(false);
  };

  // Форма открыта любому вошедшему — mayEditDocument на сервере решает по
  // нику-снимку, не по роли (задача 11). Гостю показываем приглашение
  // записаться, тем же приёмом, что CollectionForm.tsx.
  if (!isAuthenticated) {
    return (
      <div className="document-form-container">
        <Link to="/documents" className="back-link">
          ← К документам
        </Link>
        <div className="document-form-header">
          <h1>{isEdit ? 'Изменить документ' : 'Новый документ'}</h1>
        </div>
        <div className="document-form-join-invite">
          <p>Собирать и публиковать свой разбор может только читатель, вошедший в читальню.</p>
          <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
            Записаться
          </Link>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return <div className="loading-state">Загрузка документа…</div>;
  }

  // Чужой разбор правит только его автор — то же правило, что стоит на
  // сервере (mayEditDocument различает по нику-снимку, а не по роли; редактор
  // читательский разбор тоже не правит). До этой проверки постороннему
  // вошедшему показывали и поле заглавия, и редактор, и подборщик вклеек, а
  // отказ прилетал уже по нажатии «Сохранить»: серверные пути были закрыты,
  // неверен был набор предложенных кнопок.
  if (isEdit && doc && !canEditDocument(user, doc)) {
    return (
      <div className="document-form-container">
        <Link to="/documents" className="back-link">
          ← К документам
        </Link>
        <div className="document-form-header">
          <h1>Изменить документ</h1>
        </div>
        <div className="document-form-join-invite">
          <p>
            Этот разбор собрал другой читатель — править его может только он. Читальня чужие разборы
            не переписывает.
          </p>
          <Link to={documentPath(doc)} className="btn btn-primary">
            Читать разбор
          </Link>
        </div>
      </div>
    );
  }

  // Кнопка отправки на проверку не показывается, пока разбор уже стоит в
  // очереди — второй раз отправлять там нечего.
  const showSubmitForReview = isEdit && doc && doc.review_status !== 'на_рассмотрении';
  const showUnpublish = isEdit && doc && !!doc.published_at;

  return (
    <div className="document-form-container">
      <div className="document-form-header">
        <h1>{isEdit ? 'Изменить документ' : 'Новый документ'}</h1>
      </div>

      {doc && (
        <div className="document-state">
          <p className="document-state-label">{documentStateLabel(doc)}</p>
          {doc.review_status === 'отклонено' && doc.reject_reason && (
            <p className="document-state-reject-reason">
              Отклонён редактором: {DOCUMENT_REJECT_LABEL[doc.reject_reason]}
            </p>
          )}
        </div>
      )}

      {notice && <div className="document-form-notice">{notice}</div>}
      {error && <div className="error-message">{error}</div>}

      {doc && (showSubmitForReview || showUnpublish) && (
        <div className="document-form-review-actions">
          {showSubmitForReview && (
            <button
              type="button"
              className="submit-button"
              disabled={isReviewActionPending}
              onClick={() => void handleSubmitForReview()}
            >
              {isReviewActionPending
                ? 'Отправляем…'
                : doc.published_at
                  ? 'Отправить правку на проверку'
                  : 'Отправить на проверку'}
            </button>
          )}
          {showUnpublish && (
            <button
              type="button"
              className="cancel-button"
              disabled={isReviewActionPending}
              onClick={() => void handleUnpublish()}
            >
              {isReviewActionPending ? 'Снимаем…' : 'Снять с публикации'}
            </button>
          )}
        </div>
      )}

      <div className="document-form">
        <div className="form-group">
          <label htmlFor="title">Заглавие *</label>
          <input
            type="text"
            id="title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
            placeholder="Заглавие документа"
            className="title-input"
          />
        </div>

        <div className="form-group">
          <label htmlFor="content">Содержимое (Markdown)</label>
          {isEdit && slug && (
            <div className="form-group">
              <button
                type="button"
                className="cancel-button"
                onClick={() => setShowCutPicker((v) => !v)}
              >
                {showCutPicker ? 'Скрыть подборщик' : 'Вклейка'}
              </button>
            </div>
          )}
          {isEdit && slug && showCutPicker && (
            <CutPicker
              documentKey={{ nickname, slug }}
              onInsert={insertCutTag}
              onClose={() => setShowCutPicker(false)}
            />
          )}
          <div className="markdown-editor-wrapper" ref={markdownWrapperRef}>
            <MarkdownEditor
              initialContent={markdownContent}
              onChange={setMarkdownContent}
              onSubmit={handleSubmit}
            />
          </div>
        </div>

        <div className="form-actions">
          <button
            type="button"
            onClick={() => navigate('/documents')}
            className="cancel-button"
            disabled={isSaving}
          >
            Отмена
          </button>
          <button
            type="button"
            onClick={() => void handleSubmit()}
            disabled={isSaving || !title.trim()}
            className="submit-button"
          >
            {isSaving
              ? isEdit
                ? 'Сохранение…'
                : 'Создание…'
              : isEdit
                ? 'Сохранить'
                : 'Создать документ'}
          </button>
        </div>
      </div>
    </div>
  );
};
