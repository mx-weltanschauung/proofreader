import { describe, expect, it } from 'vitest';
import { russianDate } from './russianDate';

describe('russianDate', () => {
  it('печатает дату тем же видом, что и бэкенд', () => {
    expect(russianDate('2026-09-12T10:00:00Z')).toBe('12 сентября 2026 г.');
  });

  it('без ведущего нуля у дня', () => {
    expect(russianDate('2026-01-01T00:00:00Z')).toBe('1 января 2026 г.');
  });

  // Родной ru-RU формат уже кладёт «г.» сам — суффикс не должен удвоиться.
  // Мутация «всегда дописывать суффикс» ловится именно этой строкой: без
  // защиты в russianDate.ts результат стал бы «...2026 г. г.».
  it('не удваивает суффикс «г.»', () => {
    expect(russianDate('2026-09-12T10:00:00Z')).not.toMatch(/г\.\s*г\.$/);
  });

  // Обратная мутация — выбросить дописывание суффикса целиком — переживает
  // все три теста выше, потому что ru-RU в этом рантайме уже даёт «г.» сам:
  // ветка мертва без своего сценария. Подменяем сам формат (а не строку
  // после форматирования), чтобы дописывание реально исполнилось.
  it('дописывает «г.», если формат этого рантайма его сам не поставил', () => {
    // vi.spyOn(..., 'format', 'get') не типизируется: lib.es5 описывает
    // `format` обычным методом, а нативный V8 отдаёт его геттером — поэтому
    // подмена идёт через дескриптор напрямую, без vi.spyOn.
    const descriptor = Object.getOwnPropertyDescriptor(Intl.DateTimeFormat.prototype, 'format');
    if (!descriptor?.get) throw new Error('ожидался геттер format у Intl.DateTimeFormat.prototype');
    Object.defineProperty(Intl.DateTimeFormat.prototype, 'format', {
      ...descriptor,
      get: () => () => '12 сентября 2026',
    });
    try {
      expect(russianDate('2026-09-12T10:00:00Z')).toBe('12 сентября 2026 г.');
    } finally {
      Object.defineProperty(Intl.DateTimeFormat.prototype, 'format', descriptor);
    }
  });

  // Кривая/пустая дата с сервера не должна ронять экран целиком: маршрут
  // страницы не ловит исключения, а Intl.DateTimeFormat.format бросает
  // RangeError на Invalid Date.
  it('не бросает на неразбираемой дате — возвращает исходную строку', () => {
    expect(russianDate('не дата')).toBe('не дата');
    expect(russianDate('')).toBe('');
  });
});
