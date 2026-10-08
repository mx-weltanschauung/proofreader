import { useEffect, useLayoutEffect } from 'react';
import { useNavigate } from 'react-router-dom';
import { isPlainClick } from '../utils/plainClick';

/**
 * Сажает маркер номера на ту строку склеенного абзаца, с которой начинается
 * новая полоса.
 *
 * Маркер живёт в поле слева (`position: absolute` относительно абзаца — у
 * него в CSS стоит `position: relative`), поэтому горизонталь берётся из
 * стилей, а вертикаль замеряется: страница начинается в середине абзаца, и
 * без замера номер сел бы на его первую строку, то есть напротив чужого
 * текста. Первый прямоугольник строчного шва — как раз его первая строка.
 */
function placeSeamMarkers(container: HTMLElement): void {
  container.querySelectorAll<HTMLElement>('.page-seam').forEach((seam) => {
    const marker = seam.querySelector<HTMLElement>('a.page-marker');
    const paragraph = seam.closest('p');
    if (!marker || !paragraph) return;
    const line = seam.getClientRects()[0];
    if (!line) return;
    marker.style.top = `${Math.round(line.top - paragraph.getBoundingClientRect().top)}px`;
  });
}

/**
 * Швы склеенных полос: маркер номера внутри отрисованного html.
 *
 * Маркер лежит в середине абзаца, собранного `stitchPages`, то есть внутри
 * `dangerouslySetInnerHTML`, и потому не может быть `<Link>`: обычная ссылка
 * перезагрузила бы приложение целиком. Клик перехватывается делегированно —
 * тем же приёмом, каким `useNoteXrefs` оживляет ссылки примечаний.
 */
export function usePageSeams(
  containerRef: React.RefObject<HTMLElement>,
  deps: unknown[],
  /**
   * Что делать с адресом маркера. По умолчанию — переход внутри приложения:
   * так ведёт себя якорь полосы в главе. Потоковому чтению нужно другое —
   * там адрес маркера это адрес самого потока, и переход по нему пересобрал
   * бы поток с этой полосы, выбросив всё загруженное.
   */
  onActivate?: (href: string) => void,
): void {
  const navigate = useNavigate();

  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    const onClick = (event: MouseEvent) => {
      if (!isPlainClick(event)) return;
      const target = event.target as HTMLElement | null;
      const marker = target?.closest?.('.page-seam > a.page-marker');
      const href = marker?.getAttribute('href');
      if (!href) return;
      event.preventDefault();
      if (onActivate) onActivate(href);
      else navigate(href);
    };

    container.addEventListener('click', onClick);
    return () => container.removeEventListener('click', onClick);
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [navigate, onActivate, ...deps]);

  useLayoutEffect(() => {
    const container = containerRef.current;
    if (!container) return;

    let frame = 0;
    const place = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => placeSeamMarkers(container));
    };
    placeSeamMarkers(container);

    // Строка шва переезжает от всего, что меняет раскладку колонки: ширина
    // окна, кегль и интерлиньяж из настроек чтения, доехавший шрифт. Первые
    // два ловит наблюдатель размера самого контейнера, третий — событие
    // загрузки шрифтов: ширина колонки при нём не меняется, а строки едут.
    const observer = new ResizeObserver(place);
    observer.observe(container);
    window.addEventListener('resize', place);
    void document.fonts?.ready.then(place);

    return () => {
      cancelAnimationFrame(frame);
      observer.disconnect();
      window.removeEventListener('resize', place);
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
