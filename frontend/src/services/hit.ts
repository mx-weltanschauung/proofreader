import { API_BASE_URL } from './api';

export type DeviceClass = 'narrow' | 'medium' | 'wide';

// Класс ширины окна — корзиной, не числом: точная ширина вместе с прочим
// сужала бы круг до человека, а читальне нужна только доля телефонов. Границы
// совпадают с сервером (internal/stats/event.go, DeviceClass).
export function deviceClass(width: number): DeviceClass {
  if (width < 600) return 'narrow';
  if (width < 1024) return 'medium';
  return 'wide';
}

// Класс текущего окна или '' — если ширину узнать нельзя.
function currentDevice(): DeviceClass | '' {
  try {
    const w = window.innerWidth;
    return typeof w === 'number' && w > 0 ? deviceClass(w) : '';
  } catch {
    return '';
  }
}

// Маячок посещаемости (POST /api/hit). fetch с keepalive, а не sendBeacon:
// sendBeacon не умеет заголовков, а по токену сервер отличает сотрудника,
// чьи проверки иначе забили бы топ. Ответ не читается, ошибка глотается —
// статистика не имеет права мешать чтению.
export function sendHit(path: string, ref: string): void {
  const headers: Record<string, string> = {};
  try {
    // Хранилище может бросить (приватное окно, заблокированные данные сайта):
    // тогда маячок уходит без токена, а чтение не ломается.
    const token = localStorage.getItem('token');
    if (token) headers.Authorization = `Bearer ${token}`;
  } catch {
    // без токена сервер сочтёт посетителя читателем — это допустимо
  }
  try {
    void fetch(`${API_BASE_URL}/api/hit`, {
      method: 'POST',
      keepalive: true,
      headers,
      body: JSON.stringify({ path, ref, device: currentDevice() }),
    }).catch(() => {});
  } catch {
    // fetch недоступен (старый браузер) — без статистики
  }
}

// Источник захода: только чужой хост, свой переход по сайту — не источник.
export function externalReferrer(): string {
  try {
    const ref = document.referrer;
    if (!ref) return '';
    return new URL(ref).host === window.location.host ? '' : ref;
  } catch {
    return '';
  }
}
