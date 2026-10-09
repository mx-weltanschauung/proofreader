import { useEffect, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { chaptersApi, worksApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { ARTICLE_KINDS } from '../types';
import type { Chapter, CreditInput, Work } from '../types';
import { PersonPicker } from '../components/PersonPicker';
import { apiErrorMessage } from '../utils/apiError';
import { chapterTypeLabel } from '../utils/chapterTypeLabel';
import { numericId, workPath } from '../utils/paths';
import './ChapterForm.css';

/** Строка подписи в форме: то, что уходит на сервер, плюс имя и слаг
 *  человека для показа (на сервер не едут). */
type CreditRow = CreditInput & { person_name: string; person_slug: string };

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
  const [credits, setCredits] = useState<CreditRow[]>([]);
  const [createdId, setCreatedId] = useState<number | null>(null);
  const [isJournal, setIsJournal] = useState(false);
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
        setIsJournal(workResponse.data.role === 'journal_issue');
        setAllChapters(chaptersResponse.data || []);

        if (chapterId !== null) {
          const chapterResponse = await chaptersApi.get(workId, chapterId);
          setChapter(chapterResponse.data);
          setCredits(
            [...(chapterResponse.data.credits ?? [])]
              .sort((a, b) => a.position - b.position)
              .map((c) => ({
                role: c.role,
                printed: c.printed,
                person_id: c.person_id ?? null,
                person_name: '',
                person_slug: c.person_slug ?? '',
              })),
          );
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
      // credits приезжают в ответе GET и в тело PUT главы не нужны (подпись
      // уходит отдельным вызовом). article_kind у номера журнала шлётся всегда:
      // сервер читает null/отсутствие как «оставить», а снять вид можно только
      // пустой строкой; у тома ключа нет вовсе — вид не трогается.
      const { credits: _credits, ...rest } = chapter;
      void _credits;
      const chapterData: Partial<Chapter> = {
        ...rest,
        parent_id: chapter.parent_id || null,
      };
      if (isJournal) {
        // '' — не член ArticleKind, но это и есть сигнал «снять» для сервера.
        chapterData.article_kind = (chapter.article_kind ?? '') as Chapter['article_kind'];
      } else {
        delete chapterData.article_kind;
      }

      // Глава, уже созданная этой формой (подпись потом не сохранилась), при
      // повторной отправке правится, а не создаётся второй раз.
      const existingId = chapterId ?? createdId;
      let savedId: number;
      if (existingId !== null) {
        await chaptersApi.update(workId, existingId, chapterData);
        savedId = existingId;
      } else {
        const created = await chaptersApi.create(workId, chapterData);
        savedId = created.data.id;
        setCreatedId(savedId);
      }
      if (isJournal) {
        try {
          await chaptersApi.putCredits(
            workId,
            savedId,
            credits
              .filter((c) => c.printed.trim() !== '')
              .map(({ role, printed, person_id }) => ({ role, printed, person_id })),
          );
        } catch (err: unknown) {
          setError(
            `Глава сохранена, подпись не сохранилась: ${apiErrorMessage(err, 'ошибка сервера')}`,
          );
          return;
        }
      }
      toast.success(chapterId !== null ? 'Глава изменена' : 'Глава создана');
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

          {isJournal && (
            <>
              <div className="form-group">
                <label htmlFor="article_kind">Вид статьи</label>
                <select
                  id="article_kind"
                  value={chapter.article_kind ?? ''}
                  onChange={(e) =>
                    setChapter((c) => ({
                      ...c,
                      article_kind: (e.target.value || null) as Chapter['article_kind'],
                    }))
                  }
                >
                  <option value="">не статья (рубрика)</option>
                  {ARTICLE_KINDS.map((k) => (
                    <option key={k} value={k}>
                      {k.replace('_', ' ')}
                    </option>
                  ))}
                </select>
              </div>
              <fieldset className="form-group chapter-credits-editor">
                <legend>Подпись</legend>
                {credits.map((c, i) => (
                  <div key={i} className="chapter-credit-row">
                    <select
                      aria-label={`Роль ${i + 1}`}
                      value={c.role}
                      onChange={(e) =>
                        setCredits((cs) =>
                          cs.map((x, j) =>
                            j === i ? { ...x, role: e.target.value as CreditInput['role'] } : x,
                          ),
                        )
                      }
                    >
                      <option value="author">автор</option>
                      <option value="translator">переводчик</option>
                    </select>
                    <input
                      aria-label={`Подпись ${i + 1}`}
                      value={c.printed}
                      onChange={(e) =>
                        setCredits((cs) =>
                          cs.map((x, j) => (j === i ? { ...x, printed: e.target.value } : x)),
                        )
                      }
                    />
                    <PersonPicker
                      index={i + 1}
                      personId={c.person_id}
                      personName={c.person_name}
                      personSlug={c.person_slug}
                      onChange={(p) =>
                        setCredits((cs) =>
                          cs.map((x, j) =>
                            j === i
                              ? {
                                  ...x,
                                  person_id: p?.id ?? null,
                                  person_name: p?.name ?? '',
                                  person_slug: p?.slug ?? '',
                                }
                              : x,
                          ),
                        )
                      }
                    />
                    <button
                      type="button"
                      className="btn btn-secondary btn-sm"
                      onClick={() => setCredits((cs) => cs.filter((_, j) => j !== i))}
                    >
                      Убрать
                    </button>
                  </div>
                ))}
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() =>
                    setCredits((cs) => [
                      ...cs,
                      {
                        role: 'author',
                        printed: '',
                        person_id: null,
                        person_name: '',
                        person_slug: '',
                      },
                    ])
                  }
                >
                  Добавить подпись
                </button>
                <p className="form-help-text">
                  Человек — из поиска по фамилии; без человека подпись остаётся печатным текстом и
                  на страницу автора не ведёт.
                </p>
              </fieldset>
            </>
          )}

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
