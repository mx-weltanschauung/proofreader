/**
 * Единственный слушатель горячей клавиши «/» на всё приложение.
 *
 * Итоговая рецензия ветки (находка 1): затравок поиска (SearchTrigger) на
 * одной странице бывает несколько одновременно — шапка сайта (пустая
 * область), затравка карточки тома/собрания (том или собрание) и на самой
 * /search сразу две (форма страницы и «Изменить» в строке области). Прежде
 * каждый смонтированный SearchTrigger вешал СВОЙ слушатель keydown на
 * document — нажатие «/» открывало две-три панели разом, каждую со своей
 * предвыбранной областью, и Enter в одной закрывал только её, оставляя
 * остальные висеть поверх результатов.
 *
 * Слушатель здесь один — модуль-синглтон того же толка, что и loadShelfOnce
 * в shelfCache.ts (ES-модуль загружается один раз на всё приложение, никакого
 * React-контекста заводить не пришлось: работает и без обёртки Layout/App,
 * в том числе в изолированных тестах отдельных страниц). Затравки не вешают
 * слушатель сами, а регистрируются кандидатами; сработавшее «/» выбирает
 * ОДНОГО и зовёт его open().
 *
 * Чья область открывается — решает приоритет, который передаёт вызывающая
 * сторона (SearchTrigger: 0 у компактной затравки шапки, 1 у затравки
 * страницы — том, собрание, форма /search). Побеждает самая конкретная
 * затравка страницы, а не пустая из шапки: читатель, нажавший «/» на
 * карточке тома, ждёт панель с этим томом, а не с пустой областью. При
 * равенстве приоритетов побеждает первая зарегистрированная — на /search
 * это форма страницы и «Изменить» в строке области, у которых scope и так
 * совпадает, так что выбор между ними не наблюдаем.
 */

interface Candidate {
  priority: number;
  open: () => void;
}

const candidates = new Map<string, Candidate>();
let listenerAttached = false;

function isTypingTarget(element: Element | null): boolean {
  return (
    element instanceof HTMLElement &&
    (element.tagName === 'INPUT' || element.tagName === 'TEXTAREA' || element.isContentEditable)
  );
}

function handleKeyDown(event: KeyboardEvent): void {
  if (event.key !== '/') return;
  // Горячая клавиша не должна мешать набору «/» в любом поле читальни —
  // проверка идёт по activeElement, а не по источнику события, потому что
  // слушатель висит на document и получает уже всплывшее событие.
  if (isTypingTarget(document.activeElement)) return;

  let best: Candidate | null = null;
  for (const candidate of candidates.values()) {
    if (best === null || candidate.priority > best.priority) best = candidate;
  }
  if (best === null) return;
  event.preventDefault();
  best.open();
}

function ensureListener(): void {
  if (listenerAttached) return;
  listenerAttached = true;
  document.addEventListener('keydown', handleKeyDown);
}

/** Зарегистрировать затравку кандидатом на открытие по «/». */
export function registerSearchHotkeyCandidate(
  id: string,
  priority: number,
  open: () => void,
): void {
  ensureListener();
  candidates.set(id, { priority, open });
}

/** Снять затравку с регистрации (размонтирование, смена приоритета). */
export function unregisterSearchHotkeyCandidate(id: string): void {
  candidates.delete(id);
}
