import { createContext, useContext, useEffect, useRef } from 'react';

/** Дополнительная кнопка в блоке навигации. */
export interface DockAction {
  /** Подпись на кнопке. Она же доступное имя. */
  label: string;
  onActivate: () => void;
  /**
   * Адрес того же места, если оно адресуемо. С ним док рисует настоящую
   * ссылку: он показывает подглаву, которую читатель читает сейчас, и потому
   * это самое естественное место взять на неё ссылку — из контекстного меню
   * браузера. Обычный клик всё равно уходит в `onActivate`: он умеет
   * прокрутку и отступной путь, а хэш при повторном нажатии тот же самый.
   */
  href?: string;
}

interface ScrollDockValue {
  action: DockAction | null;
  setAction: (action: DockAction | null) => void;
}

export const ScrollDockContext = createContext<ScrollDockValue>({
  action: null,
  setAction: () => {},
});

export function useScrollDock(): ScrollDockValue {
  return useContext(ScrollDockContext);
}

/**
 * Регистрирует действие на время жизни компонента.
 *
 * Хук безопасен для компонентов, которые создают новый callback на каждый
 * рендер (например, `const onActivate = () => {...}` в теле компонента).
 * Callback лежит в ref, а зарегистрированное действие зовёт его через
 * замыкание на этот ref — поэтому регистрация зависит только от label и не
 * пересчитывается, пока подпись та же, сколько бы новых функций ни создал
 * рендер. Иначе каждый рендер порождал бы новый setAction, тот — новый рендер
 * провайдера, и так по кругу.
 *
 * Эффекты одного коммита выполняются в порядке объявления, поэтому ref
 * записывается РАНЬШЕ регистрации: к моменту, когда действие регистрируется,
 * в ref уже лежит callback этого же рендера. Устареть он тоже не может —
 * следующий коммит перезапишет ref прежде, чем что-либо успеет вызвать
 * действие: вызывает его только клик.
 */
export function useScrollDockAction(action: DockAction | null): void {
  const { setAction } = useScrollDock();
  const callbackRef = useRef<(() => void) | null>(null);
  const label = action?.label ?? null;
  const href = action?.href;

  // Без списка зависимостей: ref обязан догонять каждый рендер. Объявлен
  // первым не случайно — см. про порядок эффектов выше.
  useEffect(() => {
    callbackRef.current = action?.onActivate ?? null;
  });

  useEffect(() => {
    if (label === null) {
      setAction(null);
      return;
    }
    setAction({
      label,
      href,
      onActivate: () => callbackRef.current?.(),
    });
    return () => setAction(null);
  }, [label, href, setAction]);
}
