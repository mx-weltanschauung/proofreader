import { describe, it, expect } from 'vitest';
import { themeColor } from './themeColor';

describe('themeColor', () => {
  // ThemedToaster ветвился по трём темам из шести и зашивал их значения
  // в JS: тосты в oled, gray-dim и high-contrast красились цветами
  // обычной тёмной. Значение должно приходить из активной темы, какой бы
  // она ни была.
  it('читает значение переменной с корневого элемента', () => {
    document.documentElement.style.setProperty('--color-card-bg', '#123456');
    expect(themeColor('--color-card-bg')).toBe('#123456');
  });

  it('возвращает пустую строку для необъявленной переменной', () => {
    expect(themeColor('--color-net-takogo')).toBe('');
  });
});
