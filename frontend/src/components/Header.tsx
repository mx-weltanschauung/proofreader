import React, { useCallback, useEffect, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import { useAuth, isAdmin, canEdit, isReader } from '../hooks/useAuth';
import { useSiteHeaderHeight } from '../hooks/useSiteHeaderHeight';
import { readerCommentsApi, suggestionsApi } from '../services/api';
import { ReadingSettings } from './ReadingSettings';
import { feedbackApi } from '../services/api';
import { roleLabel } from '../utils/roleLabel';
import { joinPathFor } from '../utils/returnUrl';
import { STAFF_GROUPS, staffGroups } from '../utils/staffNavigation';
import { SearchTrigger } from './SearchTrigger';
import { useSite } from '../services/site';
import './Header.css';

const DIRECT_PATHS = [
  '/concepts',
  '/collections',
  '/documents',
  '/suggestions/queue',
  '/admin/readers',
  '/admin/stats',
  '/staff',
];

// Служебные страницы, чьи ссылки находятся за «Все разделы». Скрытые
// /documents/review должны перекрыть публичный /documents.
const STAFF_SECTION_PATHS = STAFF_GROUPS.filter((group) => group.id !== 'frequent').flatMap(
  (group) => group.links.map((link) => link.to),
);

function currentHeaderPath(pathname: string): string | null {
  if (STAFF_SECTION_PATHS.some((path) => pathname === path || pathname.startsWith(`${path}/`))) {
    return '/staff';
  }
  let best: string | null = null;
  for (const path of DIRECT_PATHS) {
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
  const menuRef = useRef<HTMLDivElement>(null);
  const menuTriggerRef = useRef<HTMLButtonElement>(null);
  const menuCloseTimer = useRef<number | null>(null);
  const [menuState, setMenuState] = useState({ open: false, clicked: false });
  const menuOpen = menuState.open;
  const openMenu = useCallback(() => {
    if (menuCloseTimer.current !== null) {
      window.clearTimeout(menuCloseTimer.current);
      menuCloseTimer.current = null;
    }
    setMenuState((prev) => (prev.open ? prev : { ...prev, open: true }));
  }, []);
  const closeMenu = useCallback(() => {
    if (menuCloseTimer.current !== null) {
      window.clearTimeout(menuCloseTimer.current);
      menuCloseTimer.current = null;
    }
    setMenuState((prev) => (!prev.open && !prev.clicked ? prev : { open: false, clicked: false }));
  }, []);
  const toggleMenu = useCallback(() => {
    if (menuCloseTimer.current !== null) {
      window.clearTimeout(menuCloseTimer.current);
      menuCloseTimer.current = null;
    }
    setMenuState((prev) =>
      prev.clicked ? { open: false, clicked: false } : { open: true, clicked: true },
    );
  }, []);
  useSiteHeaderHeight(headerRef);
  const location = useLocation();
  useEffect(() => {
    if (!menuOpen) return;
    const closeOutside = (event: PointerEvent) => {
      if (!menuRef.current?.contains(event.target as Node)) closeMenu();
    };
    document.addEventListener('pointerdown', closeOutside);
    return () => document.removeEventListener('pointerdown', closeOutside);
  }, [menuOpen, closeMenu]);
  useEffect(
    () => () => {
      if (menuCloseTimer.current !== null) window.clearTimeout(menuCloseTimer.current);
    },
    [],
  );
  const [unreadFeedback, setUnreadFeedback] = useState(0);
  const admin = isAdmin(user);
  const staff = canEdit(user);
  const [commentCount, setCommentCount] = useState(0);
  useEffect(() => {
    if (!staff) return;
    let alive = true;
    let request = 0;
    const load = () => {
      const current = ++request;
      void readerCommentsApi
        .pendingCount()
        .then((r) => {
          if (alive && current === request) setCommentCount(r.data.count);
        })
        .catch(() => {
          if (alive && current === request) setCommentCount(0);
        });
    };
    load();
    window.addEventListener('reader-comments-changed', load);
    return () => {
      alive = false;
      window.removeEventListener('reader-comments-changed', load);
    };
  }, [staff, user?.id]);

  useEffect(() => {
    if (!admin) {
      return;
    }
    let alive = true;
    let request = 0;
    // Счётчик — украшение: молчаливый отказ лучше сообщения об ошибке в шапке
    // на каждой странице.
    const load = () => {
      const current = ++request;
      void feedbackApi
        .unreadCount()
        .then((response) => {
          if (alive && current === request) setUnreadFeedback(response.data.count);
        })
        .catch(() => {
          if (alive && current === request) setUnreadFeedback(0);
        });
    };
    load();
    window.addEventListener('feedback-changed', load);
    return () => {
      alive = false;
      window.removeEventListener('feedback-changed', load);
    };
  }, [admin, user?.id]);

  // Значок очереди — только у того, кто её вообще может разобрать. Счётчик
  // читает total из того же запроса, которым открывается сама очередь
  // (limit=0 — нужен только total, не строки).
  const [pendingCount, setPendingCount] = useState<number | null>(null);
  const menuCounts = {
    suggestions: pendingCount,
    comments: commentCount,
    feedback: unreadFeedback,
  };
  const [prevUserId, setPrevUserId] = useState(user?.id ?? null);
  if (prevUserId !== (user?.id ?? null)) {
    setPrevUserId(user?.id ?? null);
    setMenuState({ open: false, clicked: false });
    setPendingCount(null);
    setCommentCount(0);
    setUnreadFeedback(0);
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

      {/* Частые служебные разделы остаются в шапке; остальные — на странице
          «Все разделы». Публичная навигация у сотрудника та же, что у читателя. */}
      {canEdit(user) && (
        <nav className="header-staff" aria-label="Служебное">
          <div className="header-staff-content">
            <HeaderLink to="/suggestions/queue" pathname={location.pathname}>
              Предложения
              {pendingCount !== null && pendingCount > 0 && (
                <span className="header-badge">{pendingCount}</span>
              )}
            </HeaderLink>
            {admin && (
              <>
                <HeaderLink to="/admin/readers" pathname={location.pathname}>
                  Читатели
                </HeaderLink>
                <HeaderLink to="/admin/stats" pathname={location.pathname}>
                  Статистика
                </HeaderLink>
              </>
            )}
            <div
              className="header-staff-menu"
              ref={menuRef}
              onPointerEnter={(event) => {
                if (event.pointerType === 'mouse') openMenu();
              }}
              onPointerLeave={(event) => {
                if (event.pointerType === 'mouse') {
                  if (menuCloseTimer.current !== null) window.clearTimeout(menuCloseTimer.current);
                  menuCloseTimer.current = window.setTimeout(() => {
                    closeMenu();
                  }, 180);
                }
              }}
              onFocus={openMenu}
              onBlur={(event) => {
                if (!event.currentTarget.contains(event.relatedTarget)) closeMenu();
              }}
              onKeyDown={(event) => {
                if (event.key === 'Escape') {
                  menuTriggerRef.current?.focus();
                  closeMenu();
                }
              }}
            >
              <button
                type="button"
                ref={menuTriggerRef}
                className="header-nav-link header-staff-trigger"
                aria-expanded={menuOpen}
                aria-controls={menuOpen ? 'header-staff-menu-panel' : undefined}
                aria-current={
                  currentHeaderPath(location.pathname) === '/staff' ? 'page' : undefined
                }
                onClick={toggleMenu}
              >
                Все разделы
                {(commentCount > 0 || (admin && unreadFeedback > 0)) && (
                  <span className="header-staff-dot">
                    <span className="visually-hidden">Есть новые записи</span>
                  </span>
                )}
              </button>
              {menuOpen && (
                <div
                  id="header-staff-menu-panel"
                  className="header-staff-menu-panel"
                  role="group"
                  aria-label="Все служебные разделы"
                >
                  {staffGroups(admin).map((group) => (
                    <section key={group.id} aria-label={group.title}>
                      <h3>{group.title}</h3>
                      {group.links.map((link) => (
                        <Link key={link.to} to={link.to} onClick={closeMenu}>
                          {link.label}
                          {link.count && (menuCounts[link.count] ?? 0) > 0 && (
                            <span className="header-badge">{menuCounts[link.count]}</span>
                          )}
                        </Link>
                      ))}
                    </section>
                  ))}
                  <Link className="header-staff-all-link" to="/staff" onClick={closeMenu}>
                    Открыть страницу всех разделов
                  </Link>
                </div>
              )}
            </div>
          </div>
        </nav>
      )}
    </header>
  );
};
