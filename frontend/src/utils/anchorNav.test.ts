import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { jumpToAnchor } from './anchorNav';

// Плавная прокрутка к полосе недолетает: пока страница едет, панель главы
// сжимается, и цель уходит выше рассчитанного места — под липкие полосы
// (замер в Chrome: 84px при нижнем крае панели на 124px). По окончании
// полёта (scrollend) место поправляется мгновенной прокруткой. jsdom событий
// прокрутки не знает, поэтому scrollend здесь шлётся руками.
describe('jumpToAnchor', () => {
  let target: HTMLElement;
  let scroll: ReturnType<typeof vi.fn>;

  beforeEach(() => {
    target = document.createElement('div');
    target.id = 'chapter-page-7';
    target.tabIndex = -1;
    document.body.appendChild(target);
    scroll = vi.fn();
    target.scrollIntoView = scroll;
  });

  afterEach(() => {
    target.remove();
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  it('нет якоря — false, прокрутки нет', () => {
    expect(jumpToAnchor('chapter-page-999')).toBe(false);
    expect(scroll).not.toHaveBeenCalled();
  });

  it('прокручивает плавно и переносит фокус', () => {
    expect(jumpToAnchor('chapter-page-7')).toBe(true);
    expect(scroll).toHaveBeenCalledWith({ behavior: 'smooth', block: 'start' });
    expect(target).toHaveFocus();
  });

  it('по окончании плавной прокрутки ставит цель на место мгновенно', () => {
    jumpToAnchor('chapter-page-7');
    window.dispatchEvent(new Event('scrollend'));
    expect(scroll).toHaveBeenLastCalledWith({ behavior: 'instant', block: 'start' });
    expect(scroll).toHaveBeenCalledTimes(2);
  });

  it('поправляет один раз: следующая прокрутка — уже читателя', () => {
    jumpToAnchor('chapter-page-7');
    window.dispatchEvent(new Event('scrollend'));
    window.dispatchEvent(new Event('scrollend'));
    expect(scroll).toHaveBeenCalledTimes(2);
  });

  it('поздний scrollend — прокрутка читателя, не полёт: не трогает', () => {
    vi.useFakeTimers();
    jumpToAnchor('chapter-page-7');
    vi.advanceTimersByTime(5000);
    window.dispatchEvent(new Event('scrollend'));
    expect(scroll).toHaveBeenCalledTimes(1);
  });

  it('при ограничении движения прокрутка сразу мгновенная, поправлять нечего', () => {
    vi.spyOn(window, 'matchMedia').mockReturnValue({ matches: true } as MediaQueryList);
    jumpToAnchor('chapter-page-7');
    window.dispatchEvent(new Event('scrollend'));
    expect(scroll).toHaveBeenCalledTimes(1);
    expect(scroll).toHaveBeenCalledWith({ behavior: 'auto', block: 'start' });
  });
});
