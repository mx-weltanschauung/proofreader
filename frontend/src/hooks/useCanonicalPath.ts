import { useEffect } from 'react';
import { useLocation, useNavigate } from 'react-router-dom';

/**
 * Тихо подменяет адрес в строке браузера каноническим, когда данные приехали
 * и слаг стал известен. `replace`, а не переход: заход по старой числовой
 * ссылке не должен оставлять в истории лишний шаг, иначе кнопка «назад»
 * возвращает на тот же экран.
 *
 * Строка запроса и хеш переносятся: `?q=` несёт подсветку поиска, хеш —
 * якорь подглавы.
 *
 * `canonical` = null означает «слаг ещё неизвестен» — подменять нечем.
 */
export function useCanonicalPath(canonical: string | null): void {
  const location = useLocation();
  const navigate = useNavigate();

  useEffect(() => {
    if (!canonical || canonical === location.pathname) return;
    navigate(canonical + location.search + location.hash, { replace: true });
  }, [canonical, location.pathname, location.search, location.hash, navigate]);
}
