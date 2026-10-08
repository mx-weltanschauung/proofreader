import { useState } from 'react';
import { Link, useLocation, useSearchParams } from 'react-router-dom';
import { feedbackApi } from '../services/api';
import { apiErrorMessage } from '../utils/apiError';
import { internalPath } from '../utils/internalPath';
import './Feedback.css';

/** Путь внутри читальни, откуда пришёл читатель. Сначала ?from=, затем
 *  состояние навигации; чего нет ни там, ни там — пустая строка.
 *
 *  Значение пропускается через internalPath: экран благодарности рисует его
 *  живой ссылкой «Вернуться к чтению» до всякого обращения к серверу, так
 *  что чужой сайт в ?from= без этой проверки увёл бы читателя наружу прямо
 *  со страницы читальни. */
function useSourcePath(): string {
  const [params] = useSearchParams();
  const location = useLocation();
  const fromState = (location.state as { from?: string } | null)?.from;
  return internalPath(params.get('from') ?? fromState ?? '');
}

export const Feedback: React.FC = () => {
  const sourcePath = useSourcePath();

  const [message, setMessage] = useState('');
  // Ловушка: человек этого поля не видит, бот заполняет всё подряд.
  const [bindingRef, setBindingRef] = useState('');
  const [isSending, setIsSending] = useState(false);
  const [error, setError] = useState('');
  const [isSent, setIsSent] = useState(false);

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    if (!message.trim() || isSending) return;

    setIsSending(true);
    setError('');
    try {
      await feedbackApi.send({
        message: message.trim(),
        source_path: sourcePath,
        binding_ref: bindingRef,
      });
      setIsSent(true);
    } catch (err: unknown) {
      // Текст сервера показывается как есть: он объясняет, что именно не так.
      // Написанное при этом остаётся в поле — иначе читатель потеряет письмо.
      setError(apiErrorMessage(err, 'Не удалось отправить письмо. Попробуйте позже.'));
    } finally {
      setIsSending(false);
    }
  };

  if (isSent) {
    return (
      <div className="feedback-page">
        <h1>Письмо ушло</h1>
        <p>Спасибо, мы его прочитаем. Ответить не сможем: письма анонимны.</p>
        <p>
          <Link to={sourcePath || '/'}>Вернуться к чтению</Link>
        </p>
      </div>
    );
  }

  return (
    <div className="feedback-page">
      <h1>Написать нам</h1>
      <p className="feedback-intro">
        Нашли опечатку, не хватает тома, что-то не открывается — напишите. Читальню делают читатели.
      </p>

      {error && <div className="error-message">{error}</div>}

      <form className="feedback-form" onSubmit={handleSubmit}>
        <label htmlFor="feedback-message">Сообщение</label>
        <textarea
          id="feedback-message"
          value={message}
          onChange={(e) => setMessage(e.target.value)}
          rows={10}
          maxLength={4000}
          required
        />

        {/* Ловушка. Уведена с экрана стилями, а не hidden: бот, разбирающий
            разметку, заполняет её охотнее. Читалке с экрана поле не нужно. */}
        <input
          type="text"
          name="binding_ref"
          className="feedback-trap"
          value={bindingRef}
          onChange={(e) => setBindingRef(e.target.value)}
          tabIndex={-1}
          autoComplete="off"
          aria-hidden="true"
        />

        <button type="submit" disabled={isSending}>
          {isSending ? 'Отправка…' : 'Отправить'}
        </button>

        {/* Поля «как ответить» нет намеренно: читальня не собирает персональных
            данных, и предупреждение честнее галочки согласия — согласовывать
            нечего. Подробности — в правовой информации. */}
        <p className="feedback-consent">
          Письма анонимны: обратного адреса читальня не спрашивает и ответить не сможет. Не пишите о
          себе ничего лишнего. <Link to="/legal#personal-data">Почему так</Link>.
        </p>
      </form>

      <p className="feedback-privacy">
        От письма остаётся его текст и адрес страницы, с которой вы пришли. Ваш IP-адрес не
        сохраняется: от него остаётся необратимая отметка, нужная только чтобы отбивать спам.
      </p>
    </div>
  );
};
