import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { feedbackApi } from '../../services/api';
import type { Feedback } from '../../types';
import { apiErrorMessage } from '../../utils/apiError';
import { internalPath } from '../../utils/internalPath';
import './FeedbackList.css';

function formatDate(iso: string): string {
  const date = new Date(iso);
  if (Number.isNaN(date.getTime())) return iso;
  return date.toLocaleString('ru-RU', { dateStyle: 'short', timeStyle: 'short' });
}

export const FeedbackList: React.FC = () => {
  const [letters, setLetters] = useState<Feedback[]>([]);
  const [unreadOnly, setUnreadOnly] = useState(false);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');

  // Используется для перезагрузки после действий (пометка/удаление) — там
  // гонки нет, действия идут по одному клику за раз.
  const load = useCallback(async () => {
    try {
      const response = await feedbackApi.list(unreadOnly ? false : undefined);
      setLetters(response.data || []);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось загрузить обращения'));
    }
  }, [unreadOnly]);

  useEffect(() => {
    // Флаг отмены — по образцу ConceptList.tsx (строки 52-76): без него
    // быстрое двойное переключение «только новые» выпускает два запроса, и
    // в состояние попадает тот, что ответит позже, а не тот, что запущен
    // позже — список молча замирает не в том виде, что показывает
    // переключатель.
    let cancelled = false;

    feedbackApi
      .list(unreadOnly ? false : undefined)
      .then((response) => {
        if (cancelled) return;
        setLetters(response.data || []);
        setError('');
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(apiErrorMessage(err, 'Не удалось загрузить обращения'));
      })
      .finally(() => {
        if (cancelled) return;
        setIsLoading(false);
      });

    return () => {
      cancelled = true;
    };
  }, [unreadOnly]);

  const handleToggle = async (letter: Feedback) => {
    const handled = letter.handled_at === null;
    try {
      await feedbackApi.setHandled(letter.id, handled);
      toast.success(handled ? 'Помечено разобранным' : 'Возвращено в новые');
      await load();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось изменить пометку'));
    }
  };

  const handleDelete = async (letter: Feedback) => {
    if (!window.confirm('Удалить обращение? Это навсегда.')) return;

    try {
      await feedbackApi.remove(letter.id);
      toast.success('Обращение удалено');
      await load();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось удалить обращение'));
    }
  };

  if (isLoading) {
    return <div className="loading-state">Загрузка обращений…</div>;
  }

  return (
    <div className="feedback-list-container">
      <h1>Обращения</h1>

      {error && <div className="error-message">{error}</div>}

      <label className="feedback-filter">
        <input
          type="checkbox"
          checked={unreadOnly}
          onChange={(e) => setUnreadOnly(e.target.checked)}
        />
        Только новые
      </label>

      {letters.length === 0 ? (
        <p className="feedback-empty">Писем пока нет.</p>
      ) : (
        <ul className="feedback-letters">
          {letters.map((letter) => {
            // Ссылка рисуется только если source_path — свой, внутренний путь.
            // Письмо кладёт в базу неавторизованный человек: на приёме бэкенд
            // чистит source_path (normalizeSourcePath), но админка не должна
            // полагаться на то, что в базу ничего не попало другим путём —
            // правкой руками, будущим каналом записи, переносом старых
            // данных. Невалидный путь показываем текстом, без <a>.
            const safePath = internalPath(letter.source_path);

            return (
              <li
                key={letter.id}
                className={
                  letter.handled_at === null ? 'feedback-letter is-new' : 'feedback-letter'
                }
              >
                <div className="feedback-letter-meta">
                  <span>{formatDate(letter.created_at)}</span>

                  {letter.source_path &&
                    (safePath ? (
                      <Link to={safePath}>{letter.source_path}</Link>
                    ) : (
                      <span>{letter.source_path}</span>
                    ))}
                </div>

                {/* Текст выводится как текст: ни markdown, ни сырого HTML. */}
                <p className="feedback-letter-body">{letter.message}</p>

                <div className="feedback-letter-actions">
                  <button type="button" onClick={() => void handleToggle(letter)}>
                    {letter.handled_at === null ? 'Разобрано' : 'Вернуть в новые'}
                  </button>
                  <button type="button" onClick={() => void handleDelete(letter)}>
                    Удалить
                  </button>
                </div>
              </li>
            );
          })}
        </ul>
      )}
    </div>
  );
};
