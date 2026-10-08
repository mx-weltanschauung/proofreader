import { useState } from 'react';
import { useNavigate } from 'react-router-dom';
import toast from 'react-hot-toast';
import { worksApi } from '../services/api';
import type { WorkStatus } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { workStatusLabel, WORK_STATUSES } from '../utils/workStatusLabel';
import { workPath } from '../utils/paths';
import './WorkForm.css';

export const WorkForm: React.FC = () => {
  const navigate = useNavigate();
  const [title, setTitle] = useState('');
  const [author, setAuthor] = useState('');
  const [language, setLanguage] = useState('');
  const [country, setCountry] = useState('');
  const [publicationDate, setPublicationDate] = useState('');
  const [status, setStatus] = useState<WorkStatus>('draft');
  const [isLoading, setIsLoading] = useState(false);
  const [error, setError] = useState('');

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setIsLoading(true);

    try {
      const workData = {
        title,
        author,
        language,
        country,
        publication_date: publicationDate || undefined,
        status,
        file_path: '', // Will be set during file upload
      };

      const response = await worksApi.create(workData);
      toast.success('Работа создана!');
      navigate(workPath(response.data));
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось создать работу'));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="form-container">
      <div className="form-header">
        <h1>Новая работа</h1>
        <p>Заполните сведения, чтобы создать новую работу.</p>
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
            placeholder="Заглавие работы"
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
            placeholder="Имя автора"
          />
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
              placeholder="например, English, Русский"
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
              placeholder="например, USA, Россия"
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

        <div className="form-actions">
          <button type="button" onClick={() => navigate('/')} className="cancel-button">
            Отмена
          </button>
          <button type="submit" disabled={isLoading} className="submit-button">
            {isLoading ? 'Создание…' : 'Создать работу'}
          </button>
        </div>
      </form>
    </div>
  );
};
