import { useEffect, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { chaptersApi, worksApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Chapter, Work } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { chapterTypeLabel } from '../utils/chapterTypeLabel';
import { numericId, workPath } from '../utils/paths';
import './ChapterForm.css';

const TYPES = [
  'chapter',
  'preface',
  'introduction',
  'appendix',
  'epilogue',
  'part',
  'section',
  'story',
];

function flattenChapters(
  chapters: Chapter[],
  excludeId?: number,
): { id: number; title: string; depth: number }[] {
  const result: { id: number; title: string; depth: number }[] = [];

  function traverse(items: Chapter[], depth: number) {
    for (const item of items) {
      if (item.id !== excludeId) {
        result.push({ id: item.id, title: item.title, depth });
        if (item.children && item.children.length > 0) {
          traverse(item.children, depth + 1);
        }
      }
    }
  }

  traverse(chapters, 0);
  return result;
}

export const ChapterForm: React.FC = () => {
  // workParam/chapterParam — сырые сегменты адреса; workId/chapterId ниже —
  // разобранные номера для вызовов API. Ссылки экрана строятся построителем
  // (workPath) из разобранного workId и слага загруженной работы — сырой
  // workParam сам по себе больше нигде не подставляется в адрес.
  const { workId: workParam, chapterId: chapterParam } = useParams<{
    workId: string;
    chapterId?: string;
  }>();
  const workId = numericId(workParam);
  const chapterId = numericId(chapterParam);
  const navigate = useNavigate();
  const { user } = useAuth();
  const [work, setWork] = useState<Work | null>(null);
  const [allChapters, setAllChapters] = useState<Chapter[]>([]);
  const [chapter, setChapter] = useState<Partial<Chapter>>({
    title: '',
    type: 'chapter',
    order_number: 1,
    start_page: 1,
    end_page: 1,
    parent_id: null,
  });
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');

  // isEditMode выводится из РАЗОБРАННОГО chapterId, а не из сырого
  // chapterParam: submit-ветка (ниже) тоже смотрит на chapterId, и оба
  // признака обязаны идти из одного источника. На битом сегменте
  // (/works/5/chapters/xyz/edit) chapterParam задан, а chapterId — null;
  // такой адрес получает собственный экран отказа ниже, а не тихо
  // превращается в «Новая глава».
  const isEditMode = chapterId !== null;

  useEffect(() => {
    const loadData = async () => {
      // Битый сегмент работы (numericId вернул null): грузить нечего, но
      // флаг загрузки обязан сброситься — иначе компонент вечно висит на
      // «Загрузка…» вместо того чтобы дойти до уже существующего экрана
      // «Работа не найдена». Сброс — внутри асинхронной функции, а не прямо
      // в теле эффекта: react-hooks/set-state-in-effect запрещает
      // синхронный setState там.
      if (workId === null) {
        setIsLoading(false);
        return;
      }

      try {
        const [workResponse, chaptersResponse] = await Promise.all([
          worksApi.get(workId),
          chaptersApi.list(workId),
        ]);
        setWork(workResponse.data);
        setAllChapters(chaptersResponse.data || []);

        if (chapterId !== null) {
          const chapterResponse = await chaptersApi.get(workId, chapterId);
          setChapter(chapterResponse.data);
        }
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить данные'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadData();
  }, [workId, chapterId]);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (workId === null) return;

    if (!chapter.title || !chapter.type) {
      setError('Нужны заглавие и тип');
      return;
    }

    if (chapter.start_page! > chapter.end_page!) {
      setError('Начальная страница должна быть не больше конечной');
      return;
    }

    setIsSaving(true);
    setError('');

    try {
      const chapterData = {
        ...chapter,
        parent_id: chapter.parent_id || null,
      };

      if (chapterId !== null) {
        await chaptersApi.update(workId, chapterId, chapterData);
        toast.success('Глава изменена');
      } else {
        await chaptersApi.create(workId, chapterData);
        toast.success('Глава создана');
      }
      // work уже загружен (иначе форма не отрисовалась бы), но handleSubmit —
      // отдельное замыкание, и TS не пронесёт туда сужение до non-null из
      // JSX-веток ниже; поэтому слаг берётся через work?.slug, а не work.slug.
      navigate(workPath({ id: workId, slug: work?.slug }));
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить главу'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleInputChange = (field: keyof Chapter, value: string | number | null) => {
    setChapter((prev) => ({ ...prev, [field]: value }));
  };

  if (isLoading) {
    return <div className="loading-state">Загрузка…</div>;
  }

  if (!work) {
    return (
      <div className="error-state">
        <div>Работа не найдена</div>
        <Link to="/" className="back-link">
          ← Назад
        </Link>
      </div>
    );
  }

  // Сегмент главы в адресе есть, но numericId его не разобрал (битый номер).
  // Показываем отказ вместо формы: иначе isEditMode тихо стал бы false, и
  // отправка создала бы новую главу там, где читатель ждал правку конкретной.
  if (chapterParam && chapterId === null) {
    return (
      <div className="error-state">
        <div>Глава не найдена</div>
        <Link to={workPath(work)} className="back-link">
          ← К работе
        </Link>
      </div>
    );
  }

  const canEdit = user?.role === 'administrator' || user?.role === 'editor';

  if (!canEdit) {
    return (
      <div className="error-state">
        <div>Нет прав на правку глав</div>
        <Link to={workPath(work)} className="back-link">
          ← К работе
        </Link>
      </div>
    );
  }

  const availableParents = flattenChapters(allChapters, chapterId ?? undefined);

  return (
    <div className="chapter-form-container">
      <Link to={workPath(work)} className="back-link">
        ← К работе
      </Link>

      <div className="chapter-form-card">
        <h1>{isEditMode ? 'Изменить главу' : 'Новая глава'}</h1>
        <p className="work-title">для «{work.title}»</p>

        {error && <div className="error-message">{error}</div>}

        <form onSubmit={handleSubmit} className="chapter-form">
          <div className="form-group">
            <label htmlFor="title">
              Заглавие <span className="required">*</span>
            </label>
            <input
              id="title"
              type="text"
              value={chapter.title || ''}
              onChange={(e) => handleInputChange('title', e.target.value)}
              placeholder="Заглавие главы"
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="parent_id">Родительская глава</label>
            <select
              id="parent_id"
              value={chapter.parent_id ?? ''}
              onChange={(e) =>
                handleInputChange('parent_id', e.target.value ? parseInt(e.target.value) : null)
              }
            >
              <option value="">Без родителя (верхний уровень)</option>
              {availableParents.map((parent) => (
                <option key={parent.id} value={parent.id}>
                  {'—'.repeat(parent.depth)} {parent.title}
                </option>
              ))}
            </select>
            <p className="form-help-text">Выберите родителя, чтобы сделать эту главу вложенной</p>
          </div>

          <div className="form-group">
            <label htmlFor="type">
              Тип <span className="required">*</span>
            </label>
            <select
              id="type"
              value={chapter.type || 'chapter'}
              onChange={(e) => handleInputChange('type', e.target.value)}
              required
            >
              {TYPES.map((t) => (
                <option key={t} value={t}>
                  {chapterTypeLabel(t)}
                </option>
              ))}
            </select>
          </div>

          <div className="form-row">
            <div className="form-group">
              <label htmlFor="order_number">
                Порядковый номер <span className="required">*</span>
              </label>
              <input
                id="order_number"
                type="number"
                min="1"
                value={chapter.order_number || 1}
                onChange={(e) => handleInputChange('order_number', parseInt(e.target.value))}
                required
              />
            </div>

            <div className="form-group">
              <label htmlFor="start_page">
                Начальная страница <span className="required">*</span>
              </label>
              <input
                id="start_page"
                type="number"
                min="1"
                value={chapter.start_page || 1}
                onChange={(e) => handleInputChange('start_page', parseInt(e.target.value))}
                required
              />
            </div>

            <div className="form-group">
              <label htmlFor="end_page">
                Конечная страница <span className="required">*</span>
              </label>
              <input
                id="end_page"
                type="number"
                min="1"
                value={chapter.end_page || 1}
                onChange={(e) => handleInputChange('end_page', parseInt(e.target.value))}
                required
              />
            </div>
          </div>

          <div className="form-actions">
            <button
              type="button"
              onClick={() => navigate(workPath(work))}
              className="cancel-button"
              disabled={isSaving}
            >
              Отмена
            </button>
            <button type="submit" className="submit-button" disabled={isSaving}>
              {isSaving ? 'Сохранение…' : isEditMode ? 'Сохранить' : 'Создать главу'}
            </button>
          </div>
        </form>
      </div>
    </div>
  );
};
