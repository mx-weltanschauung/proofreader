/**
 * Недавние запросы читателя. Живут только в его браузере: сервер о них не
 * знает и знать не должен — это след чтения, а не данные читальни.
 *
 * Каждое обращение к хранилищу обёрнуто: в приватном окне и при запрещённых
 * данных сайта доступ бросает, и панель поиска не должна из-за этого белеть.
 */
const KEY = 'reading-room.recent-queries';
const LIMIT = 10;

export function readRecentQueries(): string[] {
  try {
    const raw = localStorage.getItem(KEY);
    if (!raw) return [];
    const parsed: unknown = JSON.parse(raw);
    if (!Array.isArray(parsed)) return [];
    return parsed.filter((v): v is string => typeof v === 'string').slice(0, LIMIT);
  } catch {
    return [];
  }
}

export function rememberQuery(q: string): void {
  const trimmed = q.trim();
  if (!trimmed) return;
  const next = [trimmed, ...readRecentQueries().filter((v) => v !== trimmed)].slice(0, LIMIT);
  try {
    localStorage.setItem(KEY, JSON.stringify(next));
  } catch {
    // Молча: читатель просил найти, а не сохранить историю.
  }
}

export function clearRecentQueries(): void {
  try {
    localStorage.removeItem(KEY);
  } catch {
    // см. выше
  }
}
