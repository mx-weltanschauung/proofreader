import { useEffect } from 'react';
import { useNavigate } from 'react-router-dom';

function typingInField(target: EventTarget | null): boolean {
  if (!(target instanceof HTMLElement)) return false;
  const tag = target.tagName;
  return tag === 'INPUT' || tag === 'TEXTAREA' || tag === 'SELECT' || target.isContentEditable;
}

/**
 * Стрелки листают страницы работы. Не срабатывают в полях ввода и при любом
 * модификаторе: Alt+← — это «назад» браузера, и перехватывать его нельзя.
 *
 * `enabled` выключает слушатель целиком — нужен, пока открыт полноэкранный
 * просмотр скана: там же стрелками возят увеличенную картинку, а если ещё и
 * листать страницу работы под ней, оверлей может размонтироваться вместе со
 * сканом (если у соседней страницы нет превью), унося фокус на <body> в обход
 * ScanViewer.close().
 */
export function usePageKeyboardNav(
  prevHref: string | null,
  nextHref: string | null,
  enabled: boolean,
): void {
  const navigate = useNavigate();

  useEffect(() => {
    if (!enabled) return;

    const onKey = (event: KeyboardEvent) => {
      if (event.altKey || event.ctrlKey || event.metaKey || event.shiftKey) return;
      if (typingInField(event.target)) return;

      if (event.key === 'ArrowLeft' && prevHref) {
        event.preventDefault();
        navigate(prevHref);
      }
      if (event.key === 'ArrowRight' && nextHref) {
        event.preventDefault();
        navigate(nextHref);
      }
    };

    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [prevHref, nextHref, enabled, navigate]);
}
