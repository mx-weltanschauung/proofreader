import { useState } from 'react';
import { Navigate, useNavigate, useSearchParams } from 'react-router-dom';
import { useAuth } from '../hooks/useAuth';
import { safeReturnPath } from '../utils/returnUrl';
import { apiErrorMessage } from '../utils/apiError';
import './Login.css';

export const Login: React.FC = () => {
  const [email, setEmail] = useState('');
  const [password, setPassword] = useState('');
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(false);
  const navigate = useNavigate();
  const [searchParams] = useSearchParams();
  const { login, isAuthenticated } = useAuth();

  const returnPath = safeReturnPath(searchParams.get('next'));

  // Ранний возврат обслуживает приход на /login уже залогиненным; navigate
  // ниже, в handleSubmit, — отправку формы. Оба ведут по одному и тому же
  // returnPath с replace, так что дублирование не баг — убирать ни один
  // из них не нужно.
  if (isAuthenticated) return <Navigate to={returnPath} replace />;

  const handleSubmit = async (e: React.FormEvent) => {
    e.preventDefault();
    setError('');
    setIsLoading(true);

    try {
      await login(email, password);
      navigate(returnPath, { replace: true });
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось войти'));
    } finally {
      setIsLoading(false);
    }
  };

  return (
    <div className="login-container">
      <div className="login-card">
        <h1>Вход</h1>
        <form onSubmit={handleSubmit} className="login-form">
          <div className="form-group">
            <label htmlFor="email">Почта</label>
            <input
              type="email"
              id="email"
              value={email}
              onChange={(e) => setEmail(e.target.value)}
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
              required
            />
          </div>

          {error && <div className="error-message">{error}</div>}

          <button type="submit" disabled={isLoading} className="login-button">
            {isLoading ? 'Вход…' : 'Войти'}
          </button>
        </form>
      </div>
    </div>
  );
};
