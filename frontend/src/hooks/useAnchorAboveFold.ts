import { useLayoutEffect, useState } from 'react';

/**
 * Ушёл ли верх элемента `#{anchorId}` выше кромки читаемого текста.
 *
 * Кромка — не верх вьюпорта: над текстом стоят две липкие полосы, шапка сайта
 * и шапка главы, а в режиме чтения первая исчезает. Нижний край `chromeRef` —
 * ровно та линия, с которой текст становится виден, и он уже учитывает обе:
 * шапка главы липнет на `top: var(--site-header-height)`. Та же величина
 * зашита в `scroll-margin-top` секций, но измерять надёжнее, чем повторять
 * расчёт.
 *
 * Считается на кадрах прокрутки, а не через IntersectionObserver: наблюдателю
 * нужны пересечения порогов, а секция подглавы бывает выше экрана — её верх
 * уходит за кромку, доля пересечения при этом меняется непрерывно, ни 0, ни 1
 * не пересекает, и колбэк не приходит вовсе. Считать в колбэке
 * `boundingClientRect` не помогает: колбэка нет. Владелец хука и так
 * перерисовывается на каждый кадр прокрутки (useReadingProgress), так что
 * лишней цены здесь нет.
 */
export function useAnchorAboveFold(
  anchorId: string | null,
  chromeRef: React.RefObject<HTMLElement | null>,
): boolean {
  const [above, setAbove] = useState(false);

  // Замер — в layout-эффекте: якорь меняется вместе с подглавой, под которой
  // читатель находится, и значение, посчитанное для прежнего якоря, не должно
  // успеть попасть на экран. Первый замер делается прямо здесь, до отрисовки,
  // а не по ближайшему событию прокрутки — иначе кнопка моргала бы на каждом
  // стыке подглав.
  useLayoutEffect(() => {
    if (anchorId === null) return;

    let frame = 0;
    const measure = () => {
      const anchor = document.getElementById(anchorId);
      // Якоря нет — диапазон подглавы вне загруженных страниц, прыгать некуда.
      if (!anchor) {
        setAbove(false);
        return;
      }
      const fold = chromeRef.current?.getBoundingClientRect().bottom ?? 0;
      setAbove(anchor.getBoundingClientRect().top < fold);
    };

    const onScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        measure();
      });
    };

    measure();
    window.addEventListener('scroll', onScroll, { passive: true });
    window.addEventListener('resize', onScroll);
    return () => {
      window.removeEventListener('scroll', onScroll);
      window.removeEventListener('resize', onScroll);
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [anchorId, chromeRef]);

  // Без якоря ответ известен и без замера — и заодно так наружу не уходит
  // значение, посчитанное для прежнего якоря: эффект при `null` не измеряет
  // ничего и оставляет состояние как есть.
  return anchorId === null ? false : above;
}
