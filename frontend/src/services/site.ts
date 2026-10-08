import { useEffect } from 'react';
import { create } from 'zustand';

// Тот же источник, что у services/api, но без импорта оттуда: тесты страниц
// подменяют api целиком, и сведения экземпляра не должны от этого ломаться.
const API_BASE_URL: string = import.meta.env.VITE_API_BASE_URL || '';

/**
 * Всё, чем этот экземпляр читальни отличается от любого другого: имя,
 * описание, ссылки подвала. Приходит с сервера
 * (GET /api/site, значения из окружения) — образ фронта у всех один.
 *
 * До ответа и при любой ошибке действует DEFAULT_SITE: первая отрисовка сеть
 * не ждёт, а упавший запрос не оставляет шапку без имени.
 */
export interface SiteInfo {
  siteName: string;
  siteDescription: string;
  supportUrl: string;
  channelUrl: string;
  ageRating: string;
  /** Подпись под именем в шапке; пусто — подписи нет. */
  siteTagline: string;
}

export const DEFAULT_SITE: SiteInfo = {
  siteName: 'Читальня',
  siteDescription: 'Книги, вычитанные по сканам постранично.',
  supportUrl: '',
  channelUrl: '',
  ageRating: '',
  siteTagline: '',
};

/** Страница /legal — доверенный HTML-фрагмент оператора (instance/legal.html). */
export interface LegalState {
  status: 'loading' | 'missing' | 'ready';
  html?: string;
}

interface SiteStore {
  site: SiteInfo;
  legal: LegalState;
}

const useSiteStore = create<SiteStore>(() => ({
  site: DEFAULT_SITE,
  legal: { status: 'loading' },
}));

let siteRequested = false;
let legalRequested = false;

function loadSite(): void {
  if (siteRequested) return;
  siteRequested = true;
  fetch(`${API_BASE_URL}/api/site`)
    .then((r) => (r.ok ? r.json() : null))
    .then((d) => {
      if (!d) return;
      useSiteStore.setState({
        site: {
          siteName: d.site_name || DEFAULT_SITE.siteName,
          siteDescription: d.site_description || DEFAULT_SITE.siteDescription,
          supportUrl: d.support_url || '',
          channelUrl: d.channel_url || '',
          ageRating: d.age_rating || '',
          siteTagline: d.site_tagline || '',
        },
      });
    })
    .catch(() => {
      // Умолчание уже стоит — читальня работает и без сведений об экземпляре.
    });
}

/**
 * Файл есть, только если его отдал nginx из каталога экземпляра: он ставит
 * X-Instance-File. Без этой отметки 200 с HTML — это оболочка SPA (так
 * отвечает dev-сервер Vite на любой адрес), а не правовая информация.
 */
function loadLegal(): void {
  if (legalRequested) return;
  legalRequested = true;
  fetch('/legal.html')
    .then(async (r) => {
      if (r.ok && r.headers.get('x-instance-file')) {
        useSiteStore.setState({ legal: { status: 'ready', html: await r.text() } });
      } else {
        useSiteStore.setState({ legal: { status: 'missing' } });
      }
    })
    .catch(() => useSiteStore.setState({ legal: { status: 'missing' } }));
}

export function useSite(): SiteInfo {
  useEffect(loadSite, []);
  return useSiteStore((s) => s.site);
}

export function useLegal(): LegalState {
  useEffect(loadLegal, []);
  return useSiteStore((s) => s.legal);
}

/** Имя читальни вне React (метаданные проигрывателя): то, что уже известно. */
export function currentSiteName(): string {
  return useSiteStore.getState().site.siteName;
}

/** Только для тестов: вернуть стор к началу сессии. */
export function resetSiteForTests(): void {
  siteRequested = false;
  legalRequested = false;
  useSiteStore.setState({ site: DEFAULT_SITE, legal: { status: 'loading' } });
}
