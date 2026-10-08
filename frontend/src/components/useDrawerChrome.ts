import { useEffect, useRef, type RefObject } from 'react';

export const FOCUSABLE =
  'button:not([disabled]), [href], input, select, textarea, [tabindex]:not([tabindex="-1"])';

interface DrawerChromeOptions {
  open: boolean;
  panelRef: RefObject<HTMLDivElement>;
  toggleRef: RefObject<HTMLButtonElement>;
  /**
   * Куда увести фокус при открытии. Пустой ref — фокус на первый фокусируемый
   * элемент панели, как было до появления этого поля.
   */
  initialFocusRef?: RefObject<HTMLElement>;
  /** Escape и кнопка закрытия: закрыть и вернуть фокус на кнопку-переключатель. */
  onEscape: () => void;
  /** Клик мимо панели: просто закрыть — читатель уже перевёл внимание сам. */
  onOutsideClick: () => void;
}

/**
 * Модальная обвязка выезжающей панели: фокус внутрь при открытии, трап Tab,
 * Escape и закрытие по клику мимо.
 *
 * Escape перехватывается на document в capture-фазе и гасится
 * stopPropagation: ChapterView слушает Escape на window в фазе всплытия,
 * чтобы выйти из режима чтения, и без этого одно нажатие закрывало бы разом
 * и панель, и режим чтения.
 */
export function useDrawerChrome({
  open,
  panelRef,
  toggleRef,
  initialFocusRef,
  onEscape,
  onOutsideClick,
}: DrawerChromeOptions): void {
  // Колбэки живут в ref, чтобы слушатели не переподписывались на каждый
  // рендер: заново созданная стрелочная функция иначе меняла бы зависимости.
  const escapeRef = useRef(onEscape);
  const outsideRef = useRef(onOutsideClick);
  useEffect(() => {
    escapeRef.current = onEscape;
    outsideRef.current = onOutsideClick;
  });

  // Фокус переезжает внутрь панели, чтобы клавиатурный читатель оказался в ней.
  useEffect(() => {
    if (!open) return;
    const explicit = initialFocusRef?.current;
    const target = explicit ?? panelRef.current?.querySelector<HTMLElement>(FOCUSABLE);
    // Явную цель показывает эффект докрутки — автоскролл от focus() дал бы
    // рывок к краю списка перед плавным полётом к центру.
    target?.focus(explicit ? { preventScroll: true } : undefined);
  }, [open, panelRef, initialFocusRef]);

  useEffect(() => {
    if (!open) return;

    const onKeyDown = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        e.stopPropagation();
        escapeRef.current();
        return;
      }
      if (e.key !== 'Tab') return;
      const items = panelRef.current?.querySelectorAll<HTMLElement>(FOCUSABLE);
      if (!items || items.length === 0) return;
      const first = items[0];
      const last = items[items.length - 1];
      if (e.shiftKey && document.activeElement === first) {
        e.preventDefault();
        last.focus();
      } else if (!e.shiftKey && document.activeElement === last) {
        e.preventDefault();
        first.focus();
      }
    };

    const onPointerDown = (e: MouseEvent) => {
      const target = e.target as Node;
      if (panelRef.current?.contains(target)) return;
      if (toggleRef.current?.contains(target)) return;
      outsideRef.current();
    };

    document.addEventListener('keydown', onKeyDown, true);
    document.addEventListener('mousedown', onPointerDown);
    return () => {
      document.removeEventListener('keydown', onKeyDown, true);
      document.removeEventListener('mousedown', onPointerDown);
    };
  }, [open, panelRef, toggleRef]);
}
