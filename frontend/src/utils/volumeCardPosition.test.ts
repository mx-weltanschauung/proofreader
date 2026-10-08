import { describe, it, expect } from 'vitest';
import { cardPosition, CARD_WIDTH, CARD_MAX_HEIGHT, CARD_GAP } from './volumeCardPosition';

const VIEW = { width: 1200, height: 900 };

function rect(over: Partial<DOMRect>): DOMRect {
  return {
    left: 0,
    top: 0,
    right: 64,
    bottom: 212,
    width: 64,
    height: 212,
    x: 0,
    y: 0,
    ...over,
  } as DOMRect;
}

describe('cardPosition', () => {
  it('ставит карточку под корешком, выровняв по его левому краю', () => {
    const p = cardPosition(rect({ left: 100, bottom: 300, top: 88 }), VIEW);
    expect(p).toEqual({ left: 100, top: 300 + CARD_GAP });
  });

  // Крайний корешок ряда: карточка, выровненная по нему, вылезла бы за правый
  // край страницы. Ровно этого не умеет чисто-CSS вариант.
  it('прижимает карточку к правому краю окна', () => {
    const p = cardPosition(rect({ left: 1150, bottom: 300, top: 88 }), VIEW);
    expect(p.left).toBe(VIEW.width - CARD_WIDTH - CARD_GAP);
  });

  it('не уводит карточку за левый край', () => {
    const p = cardPosition(rect({ left: -30, bottom: 300, top: 88 }), VIEW);
    expect(p.left).toBe(CARD_GAP);
  });

  // Нижний ряд полки: под корешком места нет, карточка встаёт над ним.
  it('переносит карточку наверх, когда снизу не помещается', () => {
    const p = cardPosition(rect({ left: 100, bottom: 880, top: 668 }), VIEW);
    expect(p.top).toBe(668 - CARD_GAP - CARD_MAX_HEIGHT);
  });

  // Окно ниже карточки: наверху тоже не помещается, но за край уходить нельзя.
  it('в тесном окне не уводит карточку за верхний край', () => {
    const p = cardPosition(rect({ left: 100, bottom: 190, top: 10 }), { width: 1200, height: 200 });
    expect(p.top).toBe(CARD_GAP);
  });
});
