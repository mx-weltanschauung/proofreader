import { describe, expect, it } from 'vitest';
import { render, screen } from '@testing-library/react';
import { QuoteNotice } from './QuoteNotice';

describe('QuoteNotice', () => {
  it('попадание молчит', () => {
    render(<QuoteNotice match="hit" />);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('status')).toBeNull();
  });

  it('без ?quote= (match === null) тоже молчит', () => {
    render(<QuoteNotice match={null} />);
    expect(screen.queryByRole('alert')).toBeNull();
    expect(screen.queryByRole('status')).toBeNull();
  });

  // Без даты правки причину не утверждаем: `?quote=` ищет в отрисованном
  // тексте, а head_quote вырезки указателя (задача 14) — срез markdown, и
  // промах там законен без единой правки полосы. Мутация «печатать одну и
  // ту же формулировку в обеих ветках» красит именно этот тест.
  it('промах без даты правки не утверждает, что текст изменился', () => {
    render(<QuoteNotice match="miss" />);
    const notice = screen.getByRole('alert');
    expect(notice).toHaveTextContent('Цитата не нашлась');
    expect(notice).not.toHaveTextContent('изменился');
    expect(notice).toHaveClass('quote-notice-miss');
  });

  it('промах с известной датой утверждает причину и называет дату', () => {
    render(<QuoteNotice match="miss" editedAt="2026-09-12T10:00:00Z" />);
    const notice = screen.getByRole('alert');
    expect(notice).toHaveTextContent('текст этой полосы изменился');
    expect(notice).toHaveTextContent('12 сентября 2026');
  });

  // russianDate() сама дописывает суффикс «г.» (литерал внутри
  // Intl.DateTimeFormat), поэтому фраза не должна приписывать вторую точку
  // поверх — иначе на экране «12 сентября 2026 г..». Проверяем итоговую
  // строку целиком, а не только наличие даты: подстрочная проверка выше
  // прошла бы и с лишней точкой.
  it('промах с известной датой не задваивает точку после «г.»', () => {
    render(<QuoteNotice match="miss" editedAt="2026-09-12T10:00:00Z" />);
    const notice = screen.getByRole('alert');
    expect(notice.textContent).toMatch(/Последняя правка — 12 сентября 2026 г\.$/);
    expect(notice.textContent?.endsWith('г..')).toBe(false);
  });

  // Конец цитаты не нашёлся: место найдено, но подсвечено короче, чем
  // цитировали. Молчать нельзя — читатель не отличил бы это от того, что
  // цитата и была в два слова. Причина у укороченного пролёта одна (полосу
  // правили), поэтому дата тут уместна так же, как у промаха.
  it('ненайденный конец сообщается отдельно от промаха', () => {
    render(<QuoteNotice match="partial" />);
    const notice = screen.getByRole('status');
    expect(notice).toHaveTextContent('Конец цитаты не нашёлся');
    expect(notice).not.toHaveTextContent('не нашлась');
    expect(notice).toHaveClass('quote-notice-partial');
    expect(screen.queryByRole('alert')).toBeNull();
  });

  it('ненайденный конец с известной датой называет дату', () => {
    render(<QuoteNotice match="partial" editedAt="2026-09-12T10:00:00Z" />);
    const notice = screen.getByRole('status');
    expect(notice.textContent).toMatch(/Последняя правка — 12 сентября 2026 г\.$/);
  });

  // Роль и класс — не декорация: промах важнее для скринридера (alert),
  // многозначность — справочная пометка (status). Мутация «всегда
  // quote-notice-miss» красит проверку класса и роли, а не только слов.
  it('несколько вхождений — своё сообщение, не промах, роль справочная', () => {
    render(<QuoteNotice match="multiple" />);
    const notice = screen.getByRole('status');
    expect(notice).toHaveTextContent('несколько раз');
    expect(notice).not.toHaveTextContent('не нашлась');
    expect(notice).toHaveClass('quote-notice-multiple');
    expect(screen.queryByRole('alert')).toBeNull();
  });
});
