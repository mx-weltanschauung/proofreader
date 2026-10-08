import { describe, it, expect } from 'vitest';
import { arrowOffset, ARROW_INSET } from './arrowOffset';

describe('хвостик пузырька', () => {
  it('встаёт под центром якоря, когда тот внутри пузырька', () => {
    // Пузырёк от 100 до 380, центр якоря на 200.
    expect(arrowOffset(200, 100, 280)).toBe(100);
  });

  it('не съезжает за левый угол', () => {
    expect(arrowOffset(102, 100, 280)).toBe(ARROW_INSET);
  });

  it('не съезжает за правый угол', () => {
    expect(arrowOffset(379, 100, 280)).toBe(280 - ARROW_INSET);
  });

  // Кнопка «№» стоит у правого края узкого экрана, пузырёк прижат к краю
  // окна: центр якоря может оказаться правее пузырька вовсе.
  it('якорь правее пузырька прижимает хвостик к правому углу', () => {
    expect(arrowOffset(500, 100, 280)).toBe(280 - ARROW_INSET);
  });
});
