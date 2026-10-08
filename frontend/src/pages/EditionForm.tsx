import { useEffect, useState } from 'react';
import { useParams, useNavigate, Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { editionsApi } from '../services/api';
import { apiErrorMessage } from '../utils/apiError';
import { slugify } from '../utils/slugify';
import { numericId, editionPath } from '../utils/paths';
import './EditionForm.css';

export const EditionForm: React.FC = () => {
  // idParam — сырой сегмент адреса; id — разобранный номер для вызовов API.
  // Ссылки/навигация ниже (backHref, navigate после сохранения) строятся
  // построителем editionPath из id и url_slug ответа сервера, а не из
  // сырого idParam.
  const { id: idParam } = useParams<{ id?: string }>();
  const id = numericId(idParam);
  const navigate = useNavigate();
  // isEditMode выводится из РАЗОБРАННОГО id, а не из сырого idParam: ветка
  // отправки (handleSubmit) тоже смотрит на id, и оба признака обязаны идти
  // из одного источника. На битом сегменте (/editions/xyz/edit) idParam
  // задан, а id — null; такой адрес получает отдельный экран отказа ниже, а
  // не тихо становится «Новое собрание» (и раньше вдобавок вечно висел на
  // «Загрузка…», потому что загрузчик выходил раньше setIsLoading(false)).
  const isEditMode = id !== null;

  const [title, setTitle] = useState('');
  const [slug, setSlug] = useState('');
  // Отдельный слаг для адресов (url_slug) — рядом с существующим `slug`,
  // который остаётся ключом журнала публикации и не переименовывается.
  const [urlSlug, setUrlSlug] = useState('');
  const [description, setDescription] = useState('');
  // Подсказка слага живёт только до первой ручной правки: иначе набор
  // названия затирал бы осознанно выбранный слаг.
  const [slugTouched, setSlugTouched] = useState(false);
  const [isLoading, setIsLoading] = useState(isEditMode);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    if (id === null) return;

    const load = async () => {
      try {
        const response = await editionsApi.get(id);
        setTitle(response.data.title);
        setSlug(response.data.slug);
        setUrlSlug(response.data.url_slug ?? '');
        setDescription(response.data.description);
        // Существующий слаг менять подсказкой нельзя ни при каких правках.
        setSlugTouched(true);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить собрание'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void load();
  }, [id]);

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

    const payload = {
      title: title.trim(),
      slug: slug.trim(),
      url_slug: urlSlug.trim(),
      description,
    };

    try {
      if (id !== null) {
        const updated = await editionsApi.update(id, payload);
        toast.success('Собрание сохранено');
        navigate(editionPath(updated.data));
      } else {
        const created = await editionsApi.create(payload);
        toast.success('Собрание создано');
        navigate(editionPath(created.data));
      }
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить собрание'));
    } finally {
      setIsSaving(false);
    }
  };

  if (isLoading) {
    return <div className="loading-state">Загрузка…</div>;
  }

  // Сегмент id в адресе есть, но numericId его не разобрал (битый номер).
  // Показываем отказ вместо формы: иначе isEditMode тихо стал бы false, и
  // экран превратился бы в «Новое собрание» вместо честного «не найдено».
  if (idParam && id === null) {
    return (
      <div className="error-state">
        <div>Собрание не найдено</div>
        <Link to="/" className="back-link">
          ← В читальню
        </Link>
      </div>
    );
  }

  // Отмена и «назад» ведут туда, откуда по смыслу пришли: у новой карточки
  // ещё нет собственного адреса, поэтому обе — на главную; у правки — на
  // страницу собрания, которое правят.
  const backHref = id !== null ? editionPath({ id, url_slug: urlSlug }) : '/';
  const backLabel = isEditMode ? '← К собранию' : '← В читальню';

  return (
    <div className="edition-form-container">
      <Link to={backHref} className="back-link">
        {backLabel}
      </Link>

      <div className="edition-form-card">
        <h1>{isEditMode ? 'Правка собрания' : 'Новое собрание'}</h1>

        {error && <div className="error-message">{error}</div>}

        <form onSubmit={(e) => void handleSubmit(e)} className="edition-form">
          <div className="form-group">
            <label htmlFor="edition-title">
              Название <span className="required">*</span>
            </label>
            <input
              id="edition-title"
              type="text"
              value={title}
              onChange={(e) => handleTitleChange(e.target.value)}
              placeholder="Сочинения, 2-е изд."
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="edition-slug">
              Слаг <span className="required">*</span>
            </label>
            <input
              id="edition-slug"
              type="text"
              value={slug}
              onChange={(e) => {
                setSlugTouched(true);
                setSlug(e.target.value);
              }}
              placeholder="mae-2"
              required
            />
            <p className="form-help-text">Сервер слаг не генерирует и пустой не принимает.</p>
          </div>

          <div className="form-group">
            <label htmlFor="url_slug">Слаг для адресов</label>
            <input
              id="url_slug"
              type="text"
              value={urlSlug}
              onChange={(e) => setUrlSlug(e.target.value)}
              placeholder="lenin"
            />
            <p className="form-help-text">
              Отдельно от слага выше: тот остаётся ключом журнала публикации и не меняется этим
              полем.
            </p>
          </div>

          <div className="form-group">
            <label htmlFor="edition-description">Описание</label>
            <textarea
              id="edition-description"
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
    </div>
  );
};
