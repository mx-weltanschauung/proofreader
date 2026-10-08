import { useState } from 'react';
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';
import { safeReturnPath } from '../utils/returnUrl';
import { apiErrorMessage } from '../utils/apiError';
import './Join.css';

// Тот же порог, что сервер держит на пароле читателя (400 короче него).
const MIN_PASSWORD_LENGTH = 8;
// Тот же предел, что сервер держит сверху (400 длиннее него) — граница
// bcrypt, 72 БАЙТА, а не знака: кириллица занимает по два байта на букву.
// maxLength на поле ниже считает знаки (UTF-16), а не байты, и потому лишь
// приблизительная подсказка при вводе; настоящая проверка — byteLength ниже,
// той же мерой, что и сервер.
const MAX_PASSWORD_BYTES = 72;

function byteLength(s: string): number {
  return new TextEncoder().encode(s).length;
}

export const Join: React.FC = () => {
  const [nickname, setNickname] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { join, isAuthenticated } = useAuth();

  const returnPath = safeReturnPath(searchParams.get('next'));

  // Ранний возврат обслуживает приход на /join уже вошедшим читателем;
  // navigate ниже, в handleSubmit, — саму отправку формы. Оба ведут по
  // одному и тому же returnPath с replace — как в Login.tsx.
  if (isAuthenticated) return <Navigate to={returnPath} replace />;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');

    if (password.length < MIN_PASSWORD_LENGTH) {
      setError(`Пароль должен быть не короче ${MIN_PASSWORD_LENGTH} знаков`);
      return;
    }
    if (byteLength(password) > MAX_PASSWORD_BYTES) {
      setError(
        `Пароль длиннее ${MAX_PASSWORD_BYTES} байт (кириллица и другие не-латинские буквы ` +
          'занимают по два байта на знак — сократите пароль)',
      );
      return;
    }

    setIsLoading(true);

    try {
      await join(nickname, password);
      navigate(returnPath, { replace: true });
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось записаться'));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="join-container">
      <div className="join-card">
        <h1>Записаться в читальню</h1>
        <form onSubmit={handleSubmit} className="join-form">
          <div className="form-group">
            <label htmlFor="nickname">Имя</label>
            <input
              type="text"
              id="nickname"
              value={nickname}
              onChange={(e) => setNickname(e.target.value)}
              required
            />
          </div>

          <div className="form-group">
            <label htmlFor="password">Пароль</label>
            <input
              type="password"
              id="password"
              value={password}
              onChange={(e) => setPassword(e.target.value)}
              maxLength={MAX_PASSWORD_BYTES}
              required
            />
          </div>

          {error && <div className="error-message">{error}</div>}

          <button type="submit" disabled={isLoading} className="join-button">
            {isLoading ? 'Секунду…' : 'Записаться'}
          </button>
        </form>

        <p className="join-warning">
          Пароль восстановить нечем: почты мы не спрашиваем и сброса не делаем. Запишите его. Если
          такое имя уже есть — введите свой пароль и войдёте; если нет — читатель заведётся сейчас.
        </p>
      </div>
    </div>
  );
};
