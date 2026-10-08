/**
 * Клик, который ссылка вправе обработать сама.
 *
 * Всё остальное — средняя кнопка, Ctrl/⌘, Shift, Alt — оставлено браузеру:
 * «открыть в новой вкладке» и «сохранить ссылку» на маркере полосы должны
 * работать как на любой другой ссылке читальни. Ради них маркер и остаётся
 * настоящей ссылкой с адресом, а не кнопкой.
 *
 * Живёт отдельным модулем, потому что перехватчиков два и в разных местах:
 * маркер обычной полосы (`ReadingChunk`) и маркер на шве, лежащий внутри
 * `dangerouslySetInnerHTML` (`usePageSeams`). Разъехавшись, они пропустили бы
 * разные наборы клавиш.
 */
export function isPlainClick(event: {
  defaultPrevented: boolean;
  button: number;
  metaKey: boolean;
  ctrlKey: boolean;
  shiftKey: boolean;
  altKey: boolean;
}): boolean {
  if (event.defaultPrevented) return false;
  return event.button === 0 && !event.metaKey && !event.ctrlKey && !event.shiftKey && !event.altKey;
}
