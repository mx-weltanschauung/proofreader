import { useEffect, useState } from 'react';
import { useParams, useNavigate } from 'react-router-dom';
import toast from 'react-hot-toast';
import { editionsApi, worksApi } from '../services/api';
import type { Edition, Work, WorkStatus } from '../types';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { WorkVolumeFields, type WorkVolumePatch } from '../components/WorkVolumeFields';
import { workStatusLabel, WORK_STATUSES } from '../utils/workStatusLabel';
import { numericId, workPath } from '../utils/paths';
import './WorkForm.css';

export const WorkEdit: React.FC = () => {
  const { id: idParam } = useParams<{ id: string }>();
  const id = numericId(idParam);
  const navigate = useNavigate();
  const [work, setWork] = useState<Work | null>(null);
  const [title, setTitle] = useState('');
  const [author, setAuthor] = useState('');
  const [language, setLanguage] = useState('');
  const [country, setCountry] = useState('');
  const [publicationDate, setPublicationDate] = useState('');
  const [status, setStatus] = useState<WorkStatus>('draft');
  const [editions, setEditions] = useState<Edition[]>([]);
  const [editionId, setEditionId] = useState<number | null>(null);
  const [volumeNumber, setVolumeNumber] = useState<number | null>(null);
  const [volumePart, setVolumePart] = useState<string | null>(null);
  const [pageOffset, setPageOffset] = useState(0);
  const [shelfLabel, setShelfLabel] = useState('');
  const [description, setDescription] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');

  useEffect(() => {
    const loadWork = async () => {
      // Битый сегмент id (numericId вернул null): грузить нечего, но флаг
      // загрузки обязан сброситься — иначе форма вечно висит на «Загрузка
      // работы…» вместо уже существующего экрана «Работа не найдена» ниже.
      // Сброс — внутри асинхронной функции, а не прямо в теле эффекта:
      // react-hooks/set-state-in-effect запрещает синхронный setState там.
      if (id === null) {
        setIsLoading(false);
        return;
      }

      try {
        const response = await worksApi.get(id);
        const workData = response.data;
        setWork(workData);
        setTitle(workData.title);
        setAuthor(workData.author);
        setLanguage(workData.language);
        setCountry(workData.country);
        setPublicationDate(
          workData.publication_date ? workData.publication_date.split('T')[0] : '',
        );
        setStatus(workData.status);
        setEditionId(workData.edition_id ?? null);
        setVolumeNumber(workData.volume_number ?? null);
        setVolumePart(workData.volume_part ?? null);
        setPageOffset(workData.page_offset ?? 0);
        setShelfLabel(workData.shelf_label ?? '');
        setDescription(workData.description ?? '');
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить работу'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadWork();
  }, [id]);

  useEffect(() => {
    // Список собраний — вспомогательный: нужен только для селекта в
    // WorkVolumeFields. Раньше страница вообще не знала об /editions, и его
    // падение не должно превращать рабочую страницу в «Work not found» —
    // поэтому отдельный эффект со своим catch, а не общий Promise.all с
    // загрузкой самой работы.
    const loadEditions = async () => {
      try {
        const response = await editionsApi.list();
        setEditions(response.data ?? []);
      } catch {
        // Форма координат тома просто останется без выбора собрания —
        // редактирование остальных полей работы это не блокирует.
      }
    };

    void loadEditions();
  }, []);

  const handleVolumeChange = (patch: WorkVolumePatch) => {
    if ('editionId' in patch) setEditionId(patch.editionId ?? null);
    if ('volumeNumber' in patch) setVolumeNumber(patch.volumeNumber ?? null);
    if ('volumePart' in patch) setVolumePart(patch.volumePart ?? null);
    if ('pageOffset' in patch) setPageOffset(patch.pageOffset ?? 0);
    if ('shelfLabel' in patch) setShelfLabel(patch.shelfLabel ?? '');
  };

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (id === null) return;

    setError('');
    setIsSaving(true);

    try {
      const response = await worksApi.update(id, {
        title,
        author,
        language,
        country,
        publication_date: publicationDate || undefined,
        status,
        // Явные null, а не undefined: сервер различает отсутствующий ключ
        // (сохранить прежнее) и null (очистить), а undefined из JSON
        // просто исчезает.
        edition_id: editionId,
        volume_number: volumeNumber,
        volume_part: volumePart,
        page_offset: pageOffset,
        // Пустая строка — это «убрать подпись», и сервер её так и читает:
        // отдельного null здесь не нужно.
        shelf_label: shelfLabel,
        // Тот же приём: пустая строка — «описания нет», ключ шлём всегда,
        // иначе стереть описание с карточки будет нечем.
        description: description,
      });
      toast.success('Работа сохранена!');
      // Слаг берётся из ответа сервера, а не из уже загруженного work: правка
      // могла поменять заглавие, от которого сервер выводит слаг.
      navigate(workPath(response.data));
    } catch (err: unknown) {
      // Тело ответа приходит строкой от http.Error, а apiErrorMessage читает
      // JSON-поле message — серверный текст до формы не доходит. Единственный
      // надёжный признак «том занят» — код 409.
      setError(
        apiErrorStatus(err) === 409
          ? 'Этот том уже занят другой работой в собрании'
          : apiErrorMessage(err, 'Не удалось сохранить работу'),
      );
    } finally {
      setIsSaving(false);
    }
  };

  if (isLoading) {
    return <div className="loading-state">Загрузка работы…</div>;
  }

  if (!work) {
    return <div className="error-state">Работа не найдена</div>;
  }

  return (
    <div className="form-container">
      <div className="form-header">
        <h1>Изменить работу</h1>
        <p>Правка сведений о работе.</p>
      </div>

      {error && <div className="error-message">{error}</div>}

      <form onSubmit={handleSubmit} className="work-form">
        <div className="form-group">
          <label htmlFor="title">Заглавие *</label>
          <input
            type="text"
            id="title"
            value={title}
            onChange={(e) => setTitle(e.target.value)}
            required
          />
        </div>

        <div className="form-group">
          <label htmlFor="author">Автор *</label>
          <input
            type="text"
            id="author"
            value={author}
            onChange={(e) => setAuthor(e.target.value)}
            required
          />
        </div>

        <div className="form-group">
          <label htmlFor="description">Описание</label>
          <textarea
            id="description"
            value={description}
            onChange={(e) => setDescription(e.target.value)}
            maxLength={2000}
            rows={4}
          />
          <p className="form-help-text">Текст увидит читатель на карточке тома.</p>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label htmlFor="language">Язык *</label>
            <input
              type="text"
              id="language"
              value={language}
              onChange={(e) => setLanguage(e.target.value)}
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="country">Страна *</label>
            <input
              type="text"
              id="country"
              value={country}
              onChange={(e) => setCountry(e.target.value)}
              required
            />
          </div>
        </div>

        <div className="form-row">
          <div className="form-group">
            <label htmlFor="publicationDate">Дата публикации (необязательно)</label>
            <input
              type="date"
              id="publicationDate"
              value={publicationDate}
              onChange={(e) => setPublicationDate(e.target.value)}
            />
          </div>

          <div className="form-group">
            <label htmlFor="status">Статус *</label>
            <select
              id="status"
              value={status}
              onChange={(e) => setStatus(e.target.value as WorkStatus)}
              required
            >
              {WORK_STATUSES.map((s) => (
                <option key={s} value={s}>
                  {workStatusLabel(s)}
                </option>
              ))}
            </select>
          </div>
        </div>

        <WorkVolumeFields
          editions={editions}
          editionId={editionId}
          volumeNumber={volumeNumber}
          volumePart={volumePart}
          pageOffset={pageOffset}
          shelfLabel={shelfLabel}
          onChange={handleVolumeChange}
        />

        <div className="form-actions">
          <button type="button" onClick={() => navigate(workPath(work))} className="cancel-button">
            Отмена
          </button>
          <button type="submit" disabled={isSaving} className="submit-button">
            {isSaving ? 'Сохранение…' : 'Сохранить'}
          </button>
        </div>
      </form>
    </div>
  );
};
