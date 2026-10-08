import { useEffect, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { pagesApi, worksApi } from '../services/api';
import type { PageStatus, Work } from '../types';
import { getPreviewUrl } from '../utils/url';
import { MarkdownEditor } from '../components/MarkdownEditor';
import { usePageByNumber } from '../hooks/usePageByNumber';
import { useChaptersForPage } from '../hooks/useChaptersForPage';
import { ChapterBreadcrumb } from '../components/ChapterBreadcrumb';
import { apiErrorMessage } from '../utils/apiError';
import { numericId, workPath } from '../utils/paths';
import './PageEditor.css';

const PAGE_STATUSES: PageStatus[] = [
  'не_вычитана',
  'вычитывается',
  'вычитана',
  'есть_проблемы',
  'пустая_страница',
  'вычитано_машиной',
  'требует_внимания',
];

export const PageEditor: React.FC = () => {
  // workId остаётся сырым сегментом адреса — usePageByNumber/useChaptersForPage
  // сами делают Number(workId) внутри себя, поэтому им передаётся уже
  // очищенная строка с разобранным номером, а не сырой сегмент со слагом.
  // Ссылки этого экрана строятся билдерами из utils/paths ниже.
  const { workId, pageNumber } = useParams<{ workId: string; pageNumber: string }>();
  const parsedWorkId = numericId(workId);
  const cleanWorkId = parsedWorkId !== null ? String(parsedWorkId) : undefined;
  const { page, isLoading, notFound, error: loadError } = usePageByNumber(cleanWorkId, pageNumber);
  const { levels: chapterLevels } = useChaptersForPage(cleanWorkId, page?.page_number);
  const [work, setWork] = useState<Work | null>(null);
  const [content, setContent] = useState('');
  const [status, setStatus] = useState<PageStatus>('не_вычитана');
  const [comment, setComment] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');

  // Форма — черновик поверх загруженной страницы: пересевается при каждом
  // новом резолве, в том числе при переходе на соседнюю страницу.
  const [prevPage, setPrevPage] = useState(page);
  if (prevPage !== page) {
    setPrevPage(page);
    if (page) {
      setContent(page.content_markdown);
      setStatus(page.status);
    }
  }

  useEffect(() => {
    if (parsedWorkId === null) return;

    const loadWork = async () => {
      try {
        const response = await worksApi.get(parsedWorkId);
        setWork(response.data);
      } catch (err: unknown) {
        console.error('Failed to load work:', err);
      }
    };

    // Загрузчик перехватывает свою ошибку логированием и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadWork();
  }, [parsedWorkId]);

  const handleSave = async () => {
    if (!page) return;

    setIsSaving(true);
    setError('');

    try {
      await pagesApi.update(page.work_id, page.id, {
        content_markdown: content,
        status,
      });
      setComment('');
      toast.success('Страница сохранена!');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить страницу'));
    } finally {
      setIsSaving(false);
    }
  };

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

  return (
    <div className="page-editor-container">
      <div className="page-editor-header">
        <div className="page-title-section">
          {work && (
            <div className="work-breadcrumb">
              <Link to={workPath(work)} className="work-link">
                {work.title}
              </Link>
              <span className="breadcrumb-separator">/</span>
              <ChapterBreadcrumb work={work} levels={chapterLevels} />
            </div>
          )}
          <h1>Страница {page.page_number}</h1>
        </div>
        <div className="page-controls">
          <label>
            Статус:
            <select value={status} onChange={(e) => setStatus(e.target.value as PageStatus)}>
              {PAGE_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {s}
                </option>
              ))}
            </select>
          </label>

          <button onClick={handleSave} disabled={isSaving} className="save-button">
            {isSaving ? 'Сохранение…' : 'Сохранить'}
          </button>
        </div>

        {error && <div className="error-message">{error}</div>}
      </div>

      <div className="version-comment-section">
        <label htmlFor="comment">Комментарий к версии (необязательно):</label>
        <input
          type="text"
          id="comment"
          value={comment}
          onChange={(e) => setComment(e.target.value)}
          placeholder="Опишите правку…"
        />
      </div>

      <div className="content-section">
        <h3>Содержимое</h3>
        <MarkdownEditor
          initialContent={content}
          onChange={setContent}
          onSubmit={handleSave}
          imageUrl={page.preview_url ?? getPreviewUrl(page.preview_path) ?? undefined}
        />
      </div>
    </div>
  );
};
