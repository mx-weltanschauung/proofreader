const LOGIN_PATH = '/login';
const JOIN_PATH = '/join';

/** Часть адреса до query и хеша. */
function pathOnly(target: string): string {
  return target.split('?')[0].split('#')[0];
}

/**
 * Приводит путь к виду, пригодному для сравнения с /login: percent-decode,
 * нижний регистр, без хвостового слэша. react-router 6 (`matchPath`) по
 * умолчанию регистронезависим и терпим к хвостовому слэшу, так что `/LOGIN`
 * и `/login/` тоже ведут на страницу логина — без нормализации проверка их
 * не ловит. Возвращает `null` при невалидном percent-encoding:
 * `decodeURIComponent` бросает исключение на `/%zz`, а такой адрес считаем
 * подозрительным, а не безопасным для возврата.
 */
function normalizedPathOnly(target: string): string | null {
  const raw = pathOnly(target);
  let decoded: string;
  try {
    decoded = decodeURIComponent(raw);
  } catch {
    return null;
  }
  const lower = decoded.toLowerCase().replace(/\/+$/, '');
  return lower === '' ? '/' : lower;
}

/**
 * Есть ли в строке управляющий ASCII-символ (U+0000..U+001F или U+007F).
 * Парсер URL выкидывает таб, CR и LF из адреса ещё до разбора, поэтому
 * управляющий символ, вклиненный между `/` и `/` (или между `/` и обратным
 * слэшем), маскирует протокол-относительный адрес и уводит на чужой домен.
 */
function hasControlChar(value: string): boolean {
  for (const ch of value) {
    const code = ch.codePointAt(0) ?? 0;
    if (code < 0x20 || code === 0x7f) return true;
  }
  return false;
}

/**
 * Адрес двери входа (`door`), помнящий, откуда пришёл пользователь.
 * `search` приходит из `location.search` — с ведущим `?` либо пустой.
 * Общая основа для `loginPathFor` (сотрудник) и `joinPathFor` (читатель) —
 * дверей у читальни две, и у обеих один и тот же способ помнить возврат.
 */
function authDoorPathFor(door: string, pathname: string, search = ''): string {
  if (pathname === door) return `${door}${search}`;
  const target = `${pathname}${search}`;
  if (target === '/') return door;
  return `${door}?next=${encodeURIComponent(target)}`;
}

/** Адрес страницы входа сотрудника, помнящий, откуда пришёл пользователь. */
export function loginPathFor(pathname: string, search = ''): string {
  return authDoorPathFor(LOGIN_PATH, pathname, search);
}

/** Адрес двери читателя, помнящий, откуда пришёл пользователь. */
export function joinPathFor(pathname: string, search = ''): string {
  return authDoorPathFor(JOIN_PATH, pathname, search);
}

/**
 * Куда возвращать после успешного входа. Всё, что не внутренний путь, — на главную:
 * чужие домены, чужие схемы, протокол-относительные адреса и сам логин.
 */
export function safeReturnPath(raw: string | null | undefined): string {
  if (!raw) return '/';
  if (hasControlChar(raw)) return '/';
  if (!raw.startsWith('/')) return '/';
  if (raw.startsWith('//') || raw.startsWith('/\\')) return '/';
  const normalized = normalizedPathOnly(raw);
  if (normalized === null) return '/';
  if (normalized === LOGIN_PATH || normalized === JOIN_PATH) return '/';
  return raw;
}
