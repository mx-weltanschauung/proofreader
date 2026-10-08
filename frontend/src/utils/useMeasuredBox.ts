import { useCallback, useMemo, useRef } from 'react';
import type { RefObject } from 'react';

/**
 * Кладёт прямоугольник элемента и ширину окна в data-атрибут — крюк для
 * замерщика вёрстки (`frontend/scripts/measure-feature-hint.mjs`). Родня
 * useMeasuredHeight: `--dump-dom` геометрии не отдаёт, поэтому её считает
 * сама страница.
 *
 * Формат: `left,top,width,height,viewportWidth`, всё в целых пикселях.
 *
 * В отличие от useMeasuredHeight измеряемый узел не обязан существовать с
 * первого рендера — у FeatureHint пузырёк смонтирован условно (portal рисуется
 * только при open=true). Эффект с зависимостью только от имени атрибута
 * отработал бы один раз, ещё до появления узла, и больше никогда: реальный
 * замер (`npm run measure:feature-hint`) как раз это и вскрыл — пузырёк в
 * дампе есть, а атрибута нет. Поэтому измерение запускается не в эффекте
 * монтирования хука, а в момент, когда React подключает узел к самому ref —
 * возвращаемый объект переопределяет сеттер `current`, оставаясь по форме
 * обычным RefObject, так что `ref={bubbleRef}` в JSX не меняется.
 */
export function useMeasuredBox<T extends HTMLElement>(attribute: string): RefObject<T> {
  const nodeRef = useRef<T | null>(null);

  const measure = useCallback(
    (node: T) => {
      const r = node.getBoundingClientRect();
      const values = [r.left, r.top, r.width, r.height, window.innerWidth].map((v) =>
        Math.round(v),
      );
      node.setAttribute(attribute, values.join(','));
    },
    [attribute],
  );

  const attach = useCallback(
    (node: T | null) => {
      nodeRef.current = node;
      if (!node) return;
      // Узел мог отключиться (а на его месте оказаться другой) раньше, чем
      // разрешится document.fonts.ready — сверяем nodeRef перед записью,
      // чтобы не измерить чужой или уже отсоединённый элемент.
      const settle = () => {
        if (nodeRef.current === node) measure(node);
      };
      if (document.fonts) {
        // Второй аргумент .then — обработчик отказа, а не .catch(settle)
        // следом: .catch выполнил бы settle и при отказе исходного промиса, и
        // при исключении из самогó settle (например, если measure бросит) —
        // тогда settle позвал бы себя же второй раз на том же узле.
        void document.fonts.ready.then(settle, settle);
      } else {
        settle();
      }
    },
    [measure],
  );

  // Форма обычного RefObject (стабильна, пока стабилен attribute), но
  // присваивание current — это и есть точка подключения/отключения узла.
  return useMemo(
    () =>
      ({
        get current() {
          return nodeRef.current;
        },
        set current(node: T | null) {
          attach(node);
        },
      }) as RefObject<T>,
    [attach],
  );
}
