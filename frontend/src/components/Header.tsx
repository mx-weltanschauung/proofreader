import React, { useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useAuth, isAdmin, canEdit, isReader } from '../hooks/useAuth';
import { useSiteHeaderHeight } from '../hooks/useSiteHeaderHeight';
import { suggestionsApi } from '../services/api';
import { ReadingSettings } from './ReadingSettings';
import { feedbackApi } from '../services/api';
import { roleLabel } from '../utils/roleLabel';
import { joinPathFor } from '../utils/returnUrl';
import { SearchTrigger } from './SearchTrigger';
import { useSite } from '../services/site';
import './Header.css';

// Самый длинный адрес шапки, накрывающий текущий путь: «Разборы на проверку»
// (/documents/review) лежат внутри «Разборов» (/documents), и префиксное
// сравнение NavLink подсветило бы обе ссылки разом.
const HEADER_PATHS = [
  '/concepts',
  '/collections',
  '/documents',
  '/suggestions/queue',
  '/documents/review',
  '/admin/audio',
  '/admin/users',
  '/admin/readers',
  '/admin/feedback',
  '/admin/cache',
  '/admin/stats',
];

function currentHeaderPath(pathname: string): string | null {
  let best: string | null = null;
  for (const path of HEADER_PATHS) {
    const covers = pathname === path || pathname.startsWith(`${path}/`);
    if (covers && (best === null || path.length > best.length)) best = path;
  }
  return best;
}

const HeaderLink: React.FC<{ to: string; pathname: string; children: React.ReactNode }> = ({
  to,
  pathname,
  children,
}) => (
  <Link
    to={to}
    className="header-nav-link"
    aria-current={currentHeaderPath(pathname) === to ? 'page' : undefined}
  >
    {children}
  </Link>
);

export const Header: React.FC = () => {
  const { siteName, siteTagline } = useSite();
  const { user, logout, isAuthenticated } = useAuth();
  const headerRef = useRef<HTMLElement>(null);
  useSiteHeaderHeight(headerRef);
  const location = useLocation();
  const [unreadFeedback, setUnreadFeedback] = useState(0);
  const admin = isAdmin(user);

  useEffect(() => {
    if (!admin) {
      return;
    }
    // Счётчик — украшение: молчаливый отказ лучше сообщения об ошибке в шапке
    // на каждой странице.
    void feedbackApi
      .unreadCount()
      .then((response) => setUnreadFeedback(response.data.count))
      .catch(() => setUnreadFeedback(0));
  }, [admin]);

  // Значок очереди — только у того, кто её вообще может разобрать. Счётчик
  // читает total из того же запроса, которым открывается сама очередь
  // (limit=0 — нужен только total, не строки).
  const [pendingCount, setPendingCount] = useState<number | null>(null);
  const [prevUserId, setPrevUserId] = useState(user?.id ?? null);
  if (prevUserId !== (user?.id ?? null)) {
    setPrevUserId(user?.id ?? null);
    setPendingCount(null);
  }

  useEffect(() => {
    if (!canEdit(user)) return;
    let cancelled = false;
    suggestionsApi
      .queue('новое', 0, 0)
      .then((response) => {
        if (!cancelled) setPendingCount(response.data.total);
      })
      .catch((err: unknown) => {
        console.error('Failed to load pending suggestions count:', err);
      });
    return () => {
      cancelled = true;
    };
  }, [user]);

  return (
    <header className="site-header" ref={headerRef}>
      <div className="header-content">
        <div className="header-left">
          {/* Буквица в рамке — родом с полки собрания: та же идея, что у
              плашки номера на корешке тома (.volume-spine-plate). */}
          <Link to="/" className="logo">
            <span className="logo-initial" aria-hidden="true">
              {siteName.charAt(0)}
            </span>
            <span className="logo-words">
              <span className="logo-text">{siteName}</span>
              {siteTagline && <span className="logo-tagline">{siteTagline}</span>}
            </span>
          </Link>
          <nav className="header-nav" aria-label="Разделы">
            <HeaderLink to="/concepts" pathname={location.pathname}>
              Указатель
            </HeaderLink>
            <HeaderLink to="/collections" pathname={location.pathname}>
              Подборки
            </HeaderLink>
            <HeaderLink to="/documents" pathname={location.pathname}>
              Разборы
            </HeaderLink>
          </nav>
        </div>

        {/* Строка поиска в шапке — та же форма, что на карточке тома и
            странице собрания, только компактная. Прямой потомок
            header-content (не header-left): order в мобильной раскладке
            переставляет её среди flex-wrap-элементов header-content, а не
            внутри невёрстающегося в несколько строк header-left. */}
        <div className="header-search">
          <SearchTrigger
            compact
            initialQuery={
              location.pathname === '/search'
                ? (new URLSearchParams(location.search).get('q') ?? '')
                : ''
            }
          />
        </div>

        <div className="header-right">
          <ReadingSettings />

          {isAuthenticated && user ? (
            <>
              <span className="header-divider" aria-hidden="true" />
              {isReader(user) ? (
                <Link to="/mine" className="login-link">
                  {user.nickname}
                </Link>
              ) : (
                <span className="user-info">
                  <span className="user-email">{user.email}</span>
                  <span className="user-role">{roleLabel(user.role)}</span>
                </span>
              )}
              <button onClick={logout} className="btn btn-secondary btn-sm">
                Выйти
              </button>
            </>
          ) : (
            <Link to={joinPathFor(location.pathname, location.search)} className="login-link">
              Записаться
            </Link>
          )}
        </div>
      </div>

      {/* Служебная полоса — вторым рядом той же липкой шапки, а не выпадающим
          меню: в читальне ничего скрытого за иконками нет. Основной ряд у
          сотрудника тот же, что у читателя, — десять ссылок в одном ряду
          переносились на вторую строку и выдавливали поиск под «Aa». */}
      {canEdit(user) && (
        <nav className="header-staff" aria-label="Служебное">
          <div className="header-staff-content">
            <div className="header-staff-group">
              <span className="header-staff-label">Очередь</span>
              <HeaderLink to="/suggestions/queue" pathname={location.pathname}>
                Предложения
                {pendingCount !== null && pendingCount > 0 && (
                  <span className="header-badge">{pendingCount}</span>
                )}
              </HeaderLink>
              <HeaderLink to="/documents/review" pathname={location.pathname}>
                Разборы на проверку
              </HeaderLink>
              <HeaderLink to="/admin/audio" pathname={location.pathname}>
                Озвучка
              </HeaderLink>
            </div>
            {admin && (
              <div className="header-staff-group">
                <span className="header-staff-label">Управление</span>
                <HeaderLink to="/admin/users" pathname={location.pathname}>
                  Пользователи
                </HeaderLink>
                <HeaderLink to="/admin/readers" pathname={location.pathname}>
                  Читатели
                </HeaderLink>
                <HeaderLink to="/admin/feedback" pathname={location.pathname}>
                  Обращения
                  {unreadFeedback > 0 && <span className="header-badge">{unreadFeedback}</span>}
                </HeaderLink>
                <HeaderLink to="/admin/cache" pathname={location.pathname}>
                  Кэш
                </HeaderLink>
                <HeaderLink to="/admin/stats" pathname={location.pathname}>
                  Статистика
                </HeaderLink>
              </div>
            )}
          </div>
        </nav>
      )}
    </header>
  );
};
