/** Насколько хвостик не доходит до угла пузырька, в пикселях. */
export const ARROW_INSET = 14;

/**
 * Где рисовать хвостик — в координатах пузырька.
 *
 * Пузырёк выравнивается по левому краю якоря, но у края экрана его
 * отжимает обратно (computePlacement). Хвостик обязан остаться под кнопкой,
 * а не уехать вместе с пузырьком, — и не вылезти за скруглённый угол.
 */
export function arrowOffset(
  anchorCenterX: number,
  bubbleLeft: number,
  bubbleWidth: number,
): number {
  const raw = anchorCenterX - bubbleLeft;
  return Math.max(ARROW_INSET, Math.min(raw, bubbleWidth - ARROW_INSET));
}
