import { useEffect, useState, useRef } from 'react';
import { useParams, Link, useNavigate } from 'react-router-dom';
import { documentsApi } from '../services/api';
import { useAuth, canEditDocument } from '../hooks/useAuth';
import { apiErrorMessage } from '../utils/apiError';
import { documentStateLabel } from '../utils/documentLabels';
import { documentEditPath, documentPath } from '../utils/documentPaths';
import { ShareButton } from '../components/ShareButton';
import { useKatex } from '../hooks/useKatex';
import 'katex/dist/katex.min.css';
import './DocumentView.css';
// Стиль вклеек подключён здесь, а не в отдельном ленивом чанке: DocumentView
// уже грузится лениво (см. App.tsx), и стиль обязан приехать в том же чанке,
// что и разметка, которую он красит — иначе он до неё не доедет (память
// проекта: ленивые маршруты режут CSS).
import '../components/documentCut.css';

interface DocumentViewData {
  id: number;
  slug: string;
  title: string;
  html_content: string;
  owner_id: number | null;
  author_nickname: string;
  published_at: string | null;
  created_at: string;
  updated_at: string;
  // Поля состояния. Постороннему сервер гасит ТОЛЬКО review_status (тем же
  // documentForViewer, что гасит его в карточке) — приезжает "". А
  // was_published не гасится: по нему читатель отличает снятое от небывшего,
  // это публичное сведение. Полосу состояния это не путает — её рисует
  // только тот, кто вправе править.
  review_status: string;
  was_published: boolean;
  /** Черновик разошёлся с тем, что на людях: разбор опубликован, автор
   *  сохранил правку, но на проверку не отправил. Считает сервер — обе
   *  редакции есть только у него и только у того, кто вправе их видеть. */
  has_unpublished_changes: boolean;
}

export const DocumentView: React.FC = () => {
  const { nickname, slug } = useParams<{ nickname?: string; slug: string }>();
  const navigate = useNavigate();
  const { user } = useAuth();
  const [document, setDocument] = useState<DocumentViewData | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  const contentRef = useRef<HTMLDivElement>(null);
  // «Что уже развёрнуто» — правило react-hooks 7 запрещает setState в теле
  // эффекта, а перерисовывать разбор целиком ради одного блока незачем:
  // ответ вставляется точечно, в готовый DOM, тем же приёмом, что и
  // useFootnotePreview.
  const expandingCutIds = useRef<Set<string>>(new Set());

  useEffect(() => {
    if (!slug) return;

    const loadDocument = async () => {
      try {
        const response = await documentsApi.view({ nickname, slug });
        setDocument(response.data);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить документ'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadDocument();
  }, [nickname, slug]);

  useKatex(contentRef, [document]);

  // Разбор приходит одним html_content и ставится dangerouslySetInnerHTML,
  // поэтому у отдельных кнопок «Развернуть здесь» нет узлов React, на
  // которые можно было бы повесить onClick, — слушатель вешается один раз на
  // контейнер и разбирает клик делегированием.
  useEffect(() => {
    if (!slug || !document) return;
    const container = contentRef.current;
    if (!container) return;

    const onClick = (e: Event) => {
      const target = e.target as HTMLElement;
      const button = target.closest<HTMLButtonElement>('.document-cut-expand');
      if (!button || !container.contains(button)) return;
      const cutId = button.getAttribute('data-cut-id');
      if (!cutId) return;
      // Кнопка снимается вместе со свёрнутым блоком, стоит ей развернуться
      // (см. ниже), — второй раз спросить этот cutId нечем. Guard всё равно
      // не лишний: сеть отвечает не мгновенно, а двойной клик — раньше, чем
      // React успеет что-то убрать.
      if (expandingCutIds.current.has(cutId)) return;
      expandingCutIds.current.add(cutId);

      const wrapper = button.closest<HTMLElement>('.document-cut');
      if (!wrapper) {
        expandingCutIds.current.delete(cutId);
        return;
      }

      button.disabled = true;
      button.textContent = 'Загрузка…';

      documentsApi
        .cut({ nickname, slug }, parseInt(cutId))
        .then((response) => {
          if (wrapper.isConnected) {
            wrapper.outerHTML = response.data.html;
          }
        })
        .catch((err: unknown) => {
          expandingCutIds.current.delete(cutId);
          button.disabled = false;
          button.textContent = 'Развернуть здесь';
          const message = apiErrorMessage(err, 'Не удалось загрузить остаток вклейки');
          let notice = wrapper.querySelector<HTMLParagraphElement>('.document-cut-expand-error');
          if (!notice) {
            notice = window.document.createElement('p');
            notice.className = 'document-cut-expand-error';
            button.closest('.document-cut-actions')?.after(notice);
          }
          notice.textContent = message;
        });
    };

    container.addEventListener('click', onClick);
    return () => container.removeEventListener('click', onClick);
  }, [nickname, slug, document]);

  if (isLoading) {
    return <div className="loading-state">Загрузка документа…</div>;
  }

  if (error || !document) {
    return (
      <div className="error-state">
        <div>{error || 'Документ не найден'}</div>
        <Link to="/documents" className="back-link">
          ← Назад к документам
        </Link>
      </div>
    );
  }

  const editable = canEditDocument(user, document);

  return (
    <div className="document-view-container">
      <div className="document-view-header">
        <Link to="/documents" className="back-link">
          ← Назад к документам
        </Link>
        <div className="document-view-actions">
          {/* Только опубликованный: неопубликованный разбор получатель
              ссылки открыл бы как 404 (documentVisibleTo на сервере). */}
          {document.published_at && (
            <ShareButton title={`${document.title} — разбор`} path={documentPath(document)} />
          )}
          {editable && (
            <button onClick={() => navigate(documentEditPath(document))} className="edit-button">
              Править
            </button>
          )}
        </div>
      </div>

      {/* Полоса состояния — тому, кто вправе править. Без неё автор видел
          здесь свой черновик и ничем не отличал его от публичного вида:
          ровно то различие, ради которого затеяна вся ветка, ему и не
          показывали. Слова — из общего словаря documentLabels, того же, что
          печатают форма правки и «моё»: разойтись им нельзя. */}
      {editable && (
        <div className="document-state document-view-state">
          <p className="document-state-label">{documentStateLabel(document)}</p>
        </div>
      )}

      <article className="document-content">
        <header className="document-header">
          <h1>{document.title}</h1>
          {/* Несъёмная пометка происхождения: разбор читателя не выглядит
              редакционным материалом читальни. Показывается всегда, когда
              у разбора есть автор-читатель, — не часть UI, которую можно
              спрятать правкой формы. */}
          {document.author_nickname && (
            <p className="document-origin">Собрал читатель {document.author_nickname}</p>
          )}
          <div className="document-meta">
            <span>Создан: {new Date(document.created_at).toLocaleDateString()}</span>
            {document.updated_at !== document.created_at && (
              <span>Изменён: {new Date(document.updated_at).toLocaleDateString()}</span>
            )}
          </div>
        </header>

        <div
          ref={contentRef}
          className="document-html-content"
          dangerouslySetInnerHTML={{ __html: document.html_content || '' }}
        />
      </article>
    </div>
  );
};
