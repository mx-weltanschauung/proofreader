/** Ширина карточки; она же в `.volume-card` — расчёт и стиль обязаны совпадать. */
export const CARD_WIDTH = 280;

/**
 * Высота, на которую расчёт закладывается, выбирая «под корешком» или «над
 * ним». Настоящая высота известна только после отрисовки, а решать надо до
 * неё; `.volume-card` ограничена этой же величиной сверху, поэтому запас
 * никогда не оказывается меньше нужного.
 *
 * Величина не круглая, а взята с запасом от самой высокой настоящей карточки:
 * замер по всем 69 томам трёх собраний даёт максимум 226px — номер, четыре
 * работы по две строки и сводка. Прежние 220px были меньше этого максимума, и
 * у десяти томов сводка уходила под обрезку.
 */
export const CARD_MAX_HEIGHT = 232;

/** Зазор до корешка и до краёв окна. */
export const CARD_GAP = 8;

interface Viewport {
  width: number;
  height: number;
}

/**
 * Куда поставить карточку тома. Позиция считается от прямоугольника корешка в
 * координатах окна, поэтому карточка рисуется с `position: fixed`.
 *
 * Прижим к краям — то, ради чего расчёт вообще существует: крайний корешок
 * ряда стоит у правого края страницы, и карточка, выровненная по нему,
 * вылезла бы наружу. CSS этого сделать не может — он не знает, где корешок.
 */
export function cardPosition(
  anchor: DOMRect,
  view: Viewport = { width: window.innerWidth, height: window.innerHeight },
): { left: number; top: number } {
  const left = Math.min(Math.max(CARD_GAP, anchor.left), view.width - CARD_WIDTH - CARD_GAP);

  const below = anchor.bottom + CARD_GAP;
  const fitsBelow = below + CARD_MAX_HEIGHT <= view.height;
  const top = fitsBelow ? below : Math.max(CARD_GAP, anchor.top - CARD_GAP - CARD_MAX_HEIGHT);

  return { left, top };
}
