import { useEffect, useRef } from 'react';

/**
 * Кладёт высоту элемента в data-атрибут — крюк для замерщика вёрстки
 * (`frontend/scripts/measure-toc-bar.mjs`, он же `npm run measure:toc-bar`).
 * `--dump-dom` геометрии не отдаёт, поэтому высоту считает сама страница.
 *
 * Замер один на всю жизнь элемента: замерщик открывает страницу заново, а
 * без списка зависимостей эффект переписывал атрибут на каждый рендер — в
 * том числе на каждое нажатие клавиши в поиске.
 *
 * Ждём шрифты: Literata меняет высоту строки, и замер до её загрузки
 * занижает полосу.
 */
export function useMeasuredHeight<T extends HTMLElement>(attribute: string) {
  const ref = useRef<T>(null);

  useEffect(() => {
    let cancelled = false;
    const measure = () => {
      const node = ref.current;
      if (cancelled || !node) return;
      node.setAttribute(attribute, String(Math.round(node.getBoundingClientRect().height)));
    };

    if (document.fonts) {
      void document.fonts.ready.then(measure).catch(measure);
    } else {
      measure();
    }

    return () => {
      cancelled = true;
    };
  }, [attribute]);

  return ref;
}
