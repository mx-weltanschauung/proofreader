import { useCallback, useEffect, useMemo, useState } from 'react';
import { useParams, useNavigate, useLocation, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { collectionsApi, chaptersApi } from '../services/api';
import type { Chapter, Collection, CollectionEntry, Shelf } from '../types';
import { FilterCombobox, type ComboOption } from '../components/FilterCombobox';
import { loadShelfOnce } from '../utils/shelfCache';
import { apiErrorMessage } from '../utils/apiError';
import { slugify } from '../utils/slugify';
import { sourceLabel } from '../utils/sourceLabel';
import { useAuth, isReader } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import { collectionPath } from '../utils/paths';
import './CollectionForm.css';

/** Плоский список глав дерева с отступом по уровню вложенности — приём из
 * ChapterForm (выбор родителя), здесь используется для выбора главы тома.
 * Порядок — порядок чтения: по алфавиту дерево тома рассыпалось бы. */
function flattenChapters(chapters: Chapter[]): ComboOption[] {
  const result: ComboOption[] = [];

  function traverse(items: Chapter[], depth: number) {
    for (const item of items) {
      result.push({ id: item.id, label: item.title, depth });
      if (item.children && item.children.length > 0) {
        traverse(item.children, depth + 1);
      }
    }
  }

  traverse(chapters, 0);
  return result;
}

/**
 * Тома для выбора — с полки, а не из `GET /works`: тот без limit отдаёт 50
 * самых свежих, и всё, что залито раньше, в выбор не попадало (том 14
 * Плеханова). Полка отдаёт все собрания в порядке издания, тома внутри —
 * по номеру, служебные передние листы в неё не входят. Ищется том и по
 * названию собрания, и по автору.
 */
function shelfOptions(shelf: Shelf): ComboOption[] {
  const result: ComboOption[] = [];
  for (const { edition, volumes } of shelf.editions) {
    for (const volume of volumes) {
      result.push({
        id: volume.id,
        label: volume.title,
        group: edition.title,
        search: `${edition.title} ${volume.title} ${volume.author ?? ''}`,
      });
    }
  }
  for (const work of shelf.loose_works) {
    result.push({ id: work.id, label: work.title, group: 'Отдельные работы' });
  }
  return result;
}

export const CollectionForm: React.FC = () => {
  const { slug: routeSlug } = useParams<{ slug?: string }>();
  const navigate = useNavigate();
  const location = useLocation();
  const { user, isAuthenticated } = useAuth();
  const isEditMode = Boolean(routeSlug);

  // Читатель адресует свои подборки только длинным видом (см. paths.ts и
  // collectionUrl в services/api.ts): состав всегда правится под собственным
  // ником вошедшего, гадать по URL не нужно — читатель может редактировать
  // только своё. У сотрудника (не читателя) ника нет, поэтому его правки
  // всегда идут коротким, витринным адресом.
  const nickname = isReader(user) ? user?.nickname : undefined;

  const [title, setTitle] = useState('');
  const [slug, setSlug] = useState('');
  const [description, setDescription] = useState('');
  // Подсказка слага живёт только до первой ручной правки — тот же приём,
  // что и в EditionForm.
  const [slugTouched, setSlugTouched] = useState(false);
  const [isLoading, setIsLoading] = useState(isEditMode);
  const [isSaving, setIsSaving] = useState(false);
  const [isPublishing, setIsPublishing] = useState(false);
  const [error, setError] = useState('');

  // Состав существует только у уже созданной подборки — в режиме создания
  // остаётся null.
  const [collection, setCollection] = useState<Collection | null>(null);

  const [workOptions, setWorkOptions] = useState<ComboOption[]>([]);
  const [selectedWorkId, setSelectedWorkId] = useState<number | ''>('');
  const [chapters, setChapters] = useState<Chapter[]>([]);
  const [selectedChapterId, setSelectedChapterId] = useState<number | ''>('');
  const [isAdding, setIsAdding] = useState(false);

  // Порядок и номера строк — забота сервера: после любой правки состава
  // подборка перечитывается заново, а не пересчитывается в браузере.
  const reloadCollection = useCallback(
    async (s: string) => {
      const response = await collectionsApi.get(s, nickname);
      setCollection(response.data);
      return response.data;
    },
    [nickname],
  );

  useEffect(() => {
    if (!routeSlug) return;

    const load = async () => {
      try {
        const data = await reloadCollection(routeSlug);
        setTitle(data.title);
        setSlug(data.slug);
        setDescription(data.description);
        // Существующий слаг менять подсказкой нельзя ни при каких правках.
        setSlugTouched(true);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить подборку'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void load();
  }, [routeSlug, reloadCollection]);

  useEffect(() => {
    if (!isEditMode) return;

    const loadWorks = async () => {
      try {
        setWorkOptions(shelfOptions(await loadShelfOnce()));
      } catch (err: unknown) {
        toast.error(apiErrorMessage(err, 'Не удалось загрузить список томов'));
      }
    };

    void loadWorks();
  }, [isEditMode]);

  useEffect(() => {
    // Сброс при отсутствии выбранного тома — не забота эффекта: она уже
    // сделана синхронно в обработчике выбора (handleWorkSelect), эффект
    // здесь только подтягивает главы для уже выбранного тома.
    if (!selectedWorkId) return;

    const loadChapters = async () => {
      try {
        const response = await chaptersApi.list(selectedWorkId);
        setChapters(response.data ?? []);
      } catch (err: unknown) {
        setChapters([]);
        toast.error(apiErrorMessage(err, 'Не удалось загрузить главы тома'));
      }
    };

    void loadChapters();
  }, [selectedWorkId]);

  const handleWorkSelect = (value: number | '') => {
    if (value === selectedWorkId) return;
    setSelectedWorkId(value);
    // Смена тома обнуляет выбор главы и старый список — иначе можно
    // добавить главу от предыдущего тома, пока список ещё не перезагрузился.
    setChapters([]);
    setSelectedChapterId('');
  };

  const flatChapters = useMemo(() => flattenChapters(chapters), [chapters]);

  const handleTitleChange = (value: string) => {
    setTitle(value);
    if (!slugTouched) setSlug(slugify(value));
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();

    if (!title.trim() || !slug.trim()) {
      setError('Название и слаг обязательны');
      return;
    }

    setError('');
    setIsSaving(true);

    const payload = { title: title.trim(), slug: slug.trim(), description };

    try {
      if (isEditMode && routeSlug) {
        await collectionsApi.update(routeSlug, payload, nickname);
        toast.success('Подборка сохранена');
        if (payload.slug !== routeSlug) {
          // Слаг сменился — состав правится под новым адресом. Адрес правки
          // не несёт ника: CollectionForm сам вычисляет его из вошедшего
          // (см. nickname выше), а не из URL.
          navigate(`/collections/${payload.slug}/edit`);
        } else {
          await reloadCollection(routeSlug);
        }
      } else {
        const created = await collectionsApi.create(payload);
        toast.success('Подборка создана');
        // Состав правится уже у существующей подборки — своего адреса у
        // ещё не созданного нет.
        navigate(`/collections/${created.data.slug}/edit`);
      }
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить подборку'));
    } finally {
      setIsSaving(false);
    }
  };

  const handleMove = async (entry: CollectionEntry, neighborOrderNumber: number) => {
    if (!routeSlug) return;
    try {
      await collectionsApi.moveItem(routeSlug, entry.id, neighborOrderNumber, nickname);
      await reloadCollection(routeSlug);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось переставить строку'));
    }
  };

  const handleAuthorBlur = async (entry: CollectionEntry, value: string) => {
    if (!routeSlug || value === entry.author_override) return;
    try {
      await collectionsApi.updateItem(routeSlug, entry.id, { author_override: value }, nickname);
      await reloadCollection(routeSlug);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось сохранить автора'));
    }
  };

  const handleRemove = async (entry: CollectionEntry) => {
    if (!routeSlug) return;
    const confirmed = window.confirm(`Убрать «${entry.title}» из состава?`);
    if (!confirmed) return;

    try {
      await collectionsApi.removeItem(routeSlug, entry.id, nickname);
      toast.success('Строка убрана из состава');
      await reloadCollection(routeSlug);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось убрать строку'));
    }
  };

  const handleAddChapter = async () => {
    if (!routeSlug || !selectedChapterId) return;
    setIsAdding(true);
    try {
      await collectionsApi.addItem(
        routeSlug,
        { kind: 'chapter', chapter_id: selectedChapterId },
        nickname,
      );
      toast.success('Глава добавлена в состав');
      setSelectedChapterId('');
      await reloadCollection(routeSlug);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось добавить главу'));
    } finally {
      setIsAdding(false);
    }
  };

  const handleAddWork = async () => {
    if (!routeSlug || !selectedWorkId) return;
    setIsAdding(true);
    try {
      await collectionsApi.addItem(routeSlug, { kind: 'work', work_id: selectedWorkId }, nickname);
      toast.success('Том добавлен в состав целиком');
      await reloadCollection(routeSlug);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось добавить том'));
    } finally {
      setIsAdding(false);
    }
  };

  const handlePublish = async () => {
    if (!routeSlug) return;
    setIsPublishing(true);
    try {
      const response = await collectionsApi.publish(routeSlug, nickname);
      setCollection(response.data);
      toast.success('Подборка опубликована');
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось опубликовать подборку'));
    } finally {
      setIsPublishing(false);
    }
  };

  const handleUnpublish = async () => {
    if (!routeSlug) return;
    setIsPublishing(true);
    try {
      const response = await collectionsApi.unpublish(routeSlug, nickname);
      setCollection(response.data);
      toast.success('Подборка снята с публикации');
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось снять подборку с публикации'));
    } finally {
      setIsPublishing(false);
    }
  };

  if (!isAuthenticated) {
    return (
      <div className="collection-form-container">
        <Link to="/collections" className="back-link">
          ← К подборкам
        </Link>
        <div className="collection-form-card">
          <h1>{isEditMode ? 'Правка подборки' : 'Новая подборка'}</h1>
          <div className="collection-form-join-invite">
            <p>Собирать и публиковать свою подборку может только читатель, вошедший в читальню.</p>
            <Link to={joinPathFor(location.pathname, location.search)} className="btn btn-primary">
              Записаться
            </Link>
          </div>
        </div>
      </div>
    );
  }

  if (isLoading) {
    return <div className="loading-state">Загрузка…</div>;
  }

  const backHref = collection ? collectionPath(collection) : '/collections';
  const backLabel = isEditMode ? '← К подборке' : '← К подборкам';
  const items = collection?.items ?? [];

  return (
    <div className="collection-form-container">
      <Link to={backHref} className="back-link">
        {backLabel}
      </Link>

      <div className="collection-form-card">
        <h1>{isEditMode ? 'Правка подборки' : 'Новая подборка'}</h1>

        {error && <div className="error-message">{error}</div>}

        <form onSubmit={(e) => void handleSubmit(e)} className="collection-form">
          <div className="form-group">
            <label htmlFor="collection-title">
              Название <span className="required">*</span>
            </label>
            <input
              id="collection-title"
              type="text"
              value={title}
              onChange={(e) => handleTitleChange(e.target.value)}
              placeholder="Материализм и эмпириокритицизм"
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="collection-slug">
              Слаг <span className="required">*</span>
            </label>
            <input
              id="collection-slug"
              type="text"
              value={slug}
              onChange={(e) => {
                setSlugTouched(true);
                setSlug(e.target.value);
              }}
              placeholder="materializm"
              required
            />
            <p className="form-help-text">Сервер слаг не генерирует и пустой не принимает.</p>
          </div>

          <div className="form-group">
            <label htmlFor="collection-description">Описание</label>
            <textarea
              id="collection-description"
              value={description}
              onChange={(e) => setDescription(e.target.value)}
              rows={3}
            />
          </div>

          <div className="form-actions">
            <button
              type="button"
              onClick={() => navigate(backHref)}
              className="cancel-button"
              disabled={isSaving}
            >
              Отмена
            </button>
            <button type="submit" className="submit-button" disabled={isSaving}>
              {isSaving ? 'Сохранение…' : isEditMode ? 'Сохранить' : 'Создать'}
            </button>
          </div>
        </form>
      </div>

      {isEditMode && collection && (
        <div className="collection-form-card collection-publish-card">
          <h2>Публикация</h2>
          <p className="collection-publish-status">
            {collection.published_at
              ? 'Подборка опубликована.'
              : 'Подборка сейчас черновик — её видите только вы.'}
          </p>
          <p className="collection-publish-note">
            Опубликованная подборка живёт по своей ссылке — её можно переслать. В общий список
            подборок читальни она не попадает.
          </p>
          {collection.published_at ? (
            <button
              type="button"
              className="cancel-button"
              disabled={isPublishing}
              onClick={() => void handleUnpublish()}
            >
              {isPublishing ? 'Снимаем…' : 'Снять с публикации'}
            </button>
          ) : (
            <button
              type="button"
              className="submit-button"
              disabled={isPublishing}
              onClick={() => void handlePublish()}
            >
              {isPublishing ? 'Публикуем…' : 'Опубликовать'}
            </button>
          )}
        </div>
      )}

      {isEditMode && routeSlug && (
        <div className="collection-form-card collection-composition">
          <h2>Состав</h2>

          {items.length === 0 ? (
            <p className="form-help-text">В подборке пока ничего нет.</p>
          ) : (
            <ol className="composition-list">
              {items.map((entry, index) => (
                <li key={entry.id} className="composition-row">
                  <div className="composition-arrows">
                    <button
                      type="button"
                      className="arrow-button"
                      aria-label={`Переместить «${entry.title}» вверх`}
                      disabled={index === 0}
                      onClick={() => void handleMove(entry, items[index - 1].order_number)}
                    >
                      ↑
                    </button>
                    <button
                      type="button"
                      className="arrow-button"
                      aria-label={`Переместить «${entry.title}» вниз`}
                      disabled={index === items.length - 1}
                      onClick={() => void handleMove(entry, items[index + 1].order_number)}
                    >
                      ↓
                    </button>
                  </div>

                  <div className="composition-body">
                    <div className="composition-head">
                      <span className="composition-title">{entry.title}</span>
                      <span className="composition-source">{sourceLabel(entry)}</span>
                    </div>

                    <div className="composition-author-field">
                      <label htmlFor={`composition-author-${entry.id}`}>Автор</label>
                      <input
                        id={`composition-author-${entry.id}`}
                        type="text"
                        defaultValue={entry.author_override}
                        placeholder={entry.work_author}
                        onBlur={(e) => void handleAuthorBlur(entry, e.target.value)}
                      />
                    </div>
                  </div>

                  <button
                    type="button"
                    className="btn btn-danger btn-sm"
                    onClick={() => void handleRemove(entry)}
                  >
                    Убрать
                  </button>
                </li>
              ))}
            </ol>
          )}

          <div className="composition-add">
            <h3>Добавить в состав</h3>
            <div className="composition-add-row">
              <div className="form-group">
                <label htmlFor="composition-add-work">Том</label>
                <FilterCombobox
                  id="composition-add-work"
                  options={workOptions}
                  value={selectedWorkId}
                  onChange={handleWorkSelect}
                  placeholder="Собрание, том или автор: «плеханов 14»"
                />
              </div>

              <div className="form-group">
                <label htmlFor="composition-add-chapter">Глава</label>
                <FilterCombobox
                  id="composition-add-chapter"
                  options={flatChapters}
                  value={selectedChapterId}
                  onChange={setSelectedChapterId}
                  placeholder={selectedWorkId ? 'Название главы' : 'Сначала выберите том'}
                  disabled={!selectedWorkId || flatChapters.length === 0}
                />
              </div>
            </div>

            <div className="composition-add-actions">
              <button
                type="button"
                className="btn btn-secondary"
                disabled={!selectedChapterId || isAdding}
                onClick={() => void handleAddChapter()}
              >
                Добавить главу
              </button>
              <button
                type="button"
                className="btn btn-secondary"
                disabled={!selectedWorkId || isAdding}
                onClick={() => void handleAddWork()}
              >
                Добавить том целиком
              </button>
            </div>
          </div>
        </div>
      )}
    </div>
  );
};
