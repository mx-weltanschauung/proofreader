import { scrollBehavior } from './motion';

/**
 * Прокрутка к якорю с переносом фокуса.
 *
 * Фокус переносится вместе с прокруткой: без него клавиатурный читатель
 * остаётся там, откуда прыгнул, а скринридер не узнаёт, что страница
 * переехала. Секции полос принимают его благодаря `tabIndex={-1}` (у шва —
 * атрибут `tabindex`, его ставит `stitchPages`).
 *
 * Возвращает `false`, если якоря в документе нет: полоса может не попасть в
 * загруженный диапазон, и звавшему бывает куда отступить.
 */
export function jumpToAnchor(anchorId: string): boolean {
  const target = document.getElementById(anchorId);
  if (!(target instanceof HTMLElement)) return false;

  target.focus({ preventScroll: true });
  const behavior = scrollBehavior();
  target.scrollIntoView({ behavior, block: 'start' });
  if (behavior === 'smooth') settleAfterFlight(target);
  return true;
}

/**
 * Сколько ждать конца плавной прокрутки. Дольше она не летит даже через
 * главу в 742 полосы; scrollend позже — уже прокрутка самого читателя, и
 * возвращать его к якорю нельзя.
 */
const FLIGHT_MS = 2000;

/**
 * Поправка места после плавной прокрутки.
 *
 * Плавная прокрутка рассчитывает цель один раз, в начале полёта, а панель
 * главы по дороге сжимается (`.is-condensed`): текст уезжает вверх, и полоса
 * встаёт под липкими полосами вместо места под ними. Замер в Chrome на томе 6:
 * мгновенная прокрутка ставила начало полосы на 125px при нижнем крае панели
 * на 124px, плавная — на 84px. Так входила любая ссылка с якорем, открытая
 * сверху главы: из поиска, по цитате, переходом к странице. Мгновенная
 * прокрутка по окончании полёта довозит цель на место; если она уже там —
 * ничего не сдвигается.
 *
 * Где события scrollend нет (Safari), слушатель не сработает вовсе, и через
 * FLIGHT_MS таймер его снимет: поправки нет — как и было.
 */
function settleAfterFlight(target: HTMLElement): void {
  const done = new AbortController();
  window.addEventListener(
    'scrollend',
    () => {
      done.abort();
      target.scrollIntoView({ behavior: 'instant', block: 'start' });
    },
    { signal: done.signal },
  );
  window.setTimeout(() => done.abort(), FLIGHT_MS);
}

/**
 * Отметить место в адресной строке, не заводя записи в истории.
 *
 * Так ведут себя все якоря читальни: маркер полосы, строка оглавления главы и
 * плавающий док. Это метка места, а не переход, — читатель берёт её из
 * контекстного меню ссылки, — и записи в истории она заводить не должна:
 * иначе «назад» после десятка отмеченных мест десять раз никуда не ведёт.
 *
 * Второе следствие: React Router о правке не узнаёт (replaceState его не
 * будит), поэтому прокрутку делает сам звавший, а `useHashAnchor` остаётся
 * для входящих ссылок — открытых из письма, а не нажатых здесь.
 */
export function markAddress(href: string): void {
  window.history.replaceState(null, '', href);
}
