import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { ScrollDock } from './ScrollDock';
import { ScrollDockContext, type DockAction } from '../contexts/scrollDockContext';

function scrollTo(y: number) {
  act(() => {
    Object.defineProperty(window, 'scrollY', { value: y, writable: true, configurable: true });
    window.dispatchEvent(new Event('scroll'));
  });
}

function setup(action: DockAction | null = null) {
  return render(
    <ScrollDockContext.Provider value={{ action, setAction: () => {} }}>
      <ScrollDock />
    </ScrollDockContext.Provider>,
  );
}

describe('ScrollDock', () => {
  beforeEach(() => {
    window.scrollTo = vi.fn();
    Object.defineProperty(window, 'innerHeight', { value: 800, configurable: true });
    Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true });
    // jsdom реализует requestAnimationFrame через реальный ~16-мс таймер, а не
    // синхронно и не микротаском — без этой замены обновление видимости не
    // успевает произойти внутри act() до синхронных проверок ниже.
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    }) as typeof window.requestAnimationFrame;
    window.cancelAnimationFrame = vi.fn();
    // Значение по умолчанию: движение не ограничено.
    window.matchMedia = vi.fn().mockReturnValue({
      matches: false,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }) as unknown as typeof window.matchMedia;
  });

  // Короткая страница не должна обзаводиться плавающей кнопкой: прокрутки
  // на ней нет, а угол экрана она займёт.
  it('молчит, пока прокручено меньше полутора экранов', () => {
    setup();
    scrollTo(800);
    expect(screen.queryByRole('button', { name: 'Наверх' })).not.toBeInTheDocument();
  });

  it('показывает «Наверх» после порога', () => {
    setup();
    scrollTo(1300);
    expect(screen.getByRole('button', { name: 'Наверх' })).toBeInTheDocument();
  });

  it('по нажатию «Наверх» прокручивает в начало плавно', async () => {
    setup();
    scrollTo(1300);
    await userEvent.click(screen.getByRole('button', { name: 'Наверх' }));
    expect(window.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'smooth' });
  });

  // CSS scroll-behavior не влияет на опцию behavior в JS, поэтому настройку
  // приходится читать вручную — иначе она просто игнорируется.
  it('при prefers-reduced-motion прокручивает мгновенно', async () => {
    window.matchMedia = vi.fn().mockReturnValue({
      matches: true,
      addEventListener: vi.fn(),
      removeEventListener: vi.fn(),
    }) as unknown as typeof window.matchMedia;
    setup();
    scrollTo(1300);
    await userEvent.click(screen.getByRole('button', { name: 'Наверх' }));
    expect(window.scrollTo).toHaveBeenCalledWith({ top: 0, behavior: 'auto' });
  });

  it('без зарегистрированного действия второй кнопки нет', () => {
    setup();
    scrollTo(1300);
    expect(screen.getAllByRole('button')).toHaveLength(1);
  });

  it('рисует зарегистрированное действие и вызывает его', async () => {
    const onActivate = vi.fn();
    setup({ label: 'II. Манифест', onActivate });
    scrollTo(1300);
    const button = screen.getByRole('button', { name: 'В начало подглавы: II. Манифест' });
    await userEvent.click(button);
    expect(onActivate).toHaveBeenCalledOnce();
  });

  // Док показывает подглаву, которую читатель читает сейчас, и потому это
  // самое естественное место взять на неё ссылку. Настоящая ссылка нужна
  // ради контекстного меню («копировать адрес ссылки»); обычный клик
  // по-прежнему обрабатывает onActivate — он умеет и прокрутку, и отступной
  // путь для подглавы вне загруженного диапазона.
  it('с адресом рисует ссылку, а не кнопку, и всё равно зовёт действие', async () => {
    const onActivate = vi.fn();
    setup({ label: 'II. Манифест', onActivate, href: '#chapter-page-71' });
    scrollTo(1300);

    const link = screen.getByRole('link', { name: 'В начало подглавы: II. Манифест' });
    expect(link).toHaveAttribute('href', '#chapter-page-71');

    await userEvent.click(link);
    expect(onActivate).toHaveBeenCalledOnce();
  });

  it('без адреса остаётся кнопкой', () => {
    setup({ label: 'II. Манифест', onActivate: vi.fn() });
    scrollTo(1300);

    expect(
      screen.getByRole('button', { name: 'В начало подглавы: II. Манифест' }),
    ).toBeInTheDocument();
  });

  // Тесты ниже используют "перехватить, но не вызвать" стаб rAF (в отличие
  // от стаба из beforeEach, вызывающего колбэк немедленно): не сбрасывая
  // frame в 0, он держит "кадр в полёте" и тем самым проверяет ветку
  // `if (frame) return`, которую немедленно вызывающий стаб делает мёртвым
  // кодом с точки зрения остальных тестов файла.
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('схлопывает несколько scroll-событий в один кадр анимации', () => {
    const rafSpy = vi.fn(() => 1);
    window.requestAnimationFrame = rafSpy as unknown as typeof window.requestAnimationFrame;
    setup();
    act(() => {
      Object.defineProperty(window, 'scrollY', { value: 1300, writable: true, configurable: true });
      window.dispatchEvent(new Event('scroll'));
      window.dispatchEvent(new Event('scroll'));
    });
    // Монтирование уже поставило один кадр в очередь; оба последующих
    // scroll-события должны застать его "в полёте" и не запросить новый.
    expect(rafSpy).toHaveBeenCalledTimes(1);
  });

  it('на размонтировании снимает слушатель scroll и отменяет кадр в полёте', () => {
    window.requestAnimationFrame = vi.fn(
      () => 42,
    ) as unknown as typeof window.requestAnimationFrame;
    const cancelSpy = vi.fn();
    window.cancelAnimationFrame = cancelSpy;
    const removeSpy = vi.spyOn(window, 'removeEventListener');
    const { unmount } = setup();
    act(() => {
      Object.defineProperty(window, 'scrollY', { value: 1300, writable: true, configurable: true });
      window.dispatchEvent(new Event('scroll'));
    });
    unmount();
    expect(removeSpy).toHaveBeenCalledWith('scroll', expect.any(Function));
    expect(cancelSpy).toHaveBeenCalledWith(42);
  });
});
