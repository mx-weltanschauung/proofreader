import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook } from '@testing-library/react';
import { useFootnotePreview } from './useFootnotePreview';
import { closeActivePopover, getActivePopover, openPopover } from './popoverPlacement';

const enter = (el: Element) => el.dispatchEvent(new MouseEvent('mouseenter'));
const leave = (el: Element) => el.dispatchEvent(new MouseEvent('mouseleave'));
const popover = () => document.querySelector('.footnote-preview-popover');

describe('useFootnotePreview', () => {
  let container: HTMLDivElement;
  let marker: Element;

  beforeEach(() => {
    vi.useFakeTimers();
    container = document.createElement('div');
    container.innerHTML = `
      <p>Текст<sup class="footnote-ref"><a href="#fn:1">1</a></sup>
      и ещё<sup class="footnote-ref"><a href="#fn:99">2</a></sup></p>
      <ol><li id="fn:1"><a class="fn-back" href="#">↑</a> Тело примечания</li></ol>
    `;
    document.body.appendChild(container);
    marker = container.querySelector('sup.footnote-ref a')!;
    renderHook(() => useFootnotePreview({ current: container }, []));
  });

  afterEach(() => {
    closeActivePopover();
    vi.useRealTimers();
    document.body.innerHTML = '';
  });

  it('opens a preview on hovering the marker', () => {
    enter(marker);
    expect(popover()?.textContent).toContain('Тело примечания');
  });

  it('drops the back-link marker from the preview', () => {
    enter(marker);
    expect(popover()?.querySelector('a.fn-back')).toBeNull();
  });

  it('holds the preview open for a moment after the pointer leaves the marker', () => {
    enter(marker);
    leave(marker);
    expect(popover()).not.toBeNull();
    vi.advanceTimersByTime(300);
    expect(popover()).toBeNull();
  });

  it('keeps the preview open when the pointer moves into it', () => {
    enter(marker);
    leave(marker);
    enter(popover()!);
    vi.advanceTimersByTime(300);
    expect(popover()).not.toBeNull();
  });

  it('closes the preview once the pointer leaves it', () => {
    enter(marker);
    leave(marker);
    const pop = popover()!;
    enter(pop);
    leave(pop);
    vi.advanceTimersByTime(300);
    expect(popover()).toBeNull();
  });

  // Экран чтения (и режим главы, и поток) выходит по Escape оконным
  // обработчиком, а window — последняя остановка всплытия. Пока подсказка
  // открыта, Escape принадлежит ей: в потоке иначе одно нажатие уносит весь
  // накопленный текст. useNoteXrefs так уже умеет.
  it('Escape при открытой подсказке не доходит до оконного обработчика', () => {
    const onWindowKey = vi.fn();
    window.addEventListener('keydown', onWindowKey);
    try {
      enter(marker);
      expect(popover()).not.toBeNull();

      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));

      expect(popover()).toBeNull();
      expect(onWindowKey).not.toHaveBeenCalled();
    } finally {
      window.removeEventListener('keydown', onWindowKey);
    }
  });

  // Обратная половина: без открытой подсказки Escape ничей, и глушить его
  // нельзя — иначе выход из чтения перестанет работать вовсе.
  it('Escape без открытой подсказки уходит дальше', () => {
    const onWindowKey = vi.fn();
    window.addEventListener('keydown', onWindowKey);
    try {
      document.dispatchEvent(new KeyboardEvent('keydown', { key: 'Escape', bubbles: true }));
      expect(onWindowKey).toHaveBeenCalledTimes(1);
    } finally {
      window.removeEventListener('keydown', onWindowKey);
    }
  });

  it('leaves a popover it did not open alone', () => {
    // Someone else - the cross-reference hook - owns the shared slot.
    const foreign = document.createElement('div');
    foreign.className = 'popover-panel';
    openPopover(foreign, container);

    // Sweeping the pointer across a marker with no note body opens nothing,
    // so it must schedule nothing either.
    const orphan = container.querySelectorAll('sup.footnote-ref a')[1];
    enter(orphan);
    leave(orphan);
    vi.advanceTimersByTime(300);

    expect(getActivePopover()).toBe(foreign);
  });
});

describe('useFootnotePreview на тач-экране', () => {
  let container: HTMLDivElement;
  let marker: HTMLAnchorElement;
  let scrollIntoView: ReturnType<typeof vi.fn>;

  const sheet = () => document.querySelector('.note-sheet');
  // jsdom не знает PointerEvent, а хук решает по типу указателя: мышь получает
  // прежнюю панель по наведению, палец — лист.
  const down = (el: Element, pointerType: string) => {
    const e = new MouseEvent('pointerdown', { bubbles: true });
    Object.defineProperty(e, 'pointerType', { value: pointerType });
    el.dispatchEvent(e);
  };
  const tap = (el: Element) => {
    down(el, 'touch');
    const click = new MouseEvent('click', { bubbles: true, cancelable: true });
    el.dispatchEvent(click);
    return click;
  };

  beforeEach(() => {
    vi.useFakeTimers();
    scrollIntoView = vi.fn();
    Element.prototype.scrollIntoView = scrollIntoView;
    container = document.createElement('div');
    container.innerHTML = `
      <p>Текст<sup class="footnote-ref footnote-ref--endnote"><a href="#fn:12">12</a></sup>
      и звёздочка<sup class="footnote-ref footnote-ref--subscript"><a href="#fn:s1">*</a></sup></p>
      <ol><li id="fn:12"><a class="fn-back" href="#">↑</a> Тело примечания</li>
      <li id="fn:s1"><a class="fn-back" href="#">↑</a> Тело сноски</li></ol>
    `;
    document.body.appendChild(container);
    marker = container.querySelector('sup.footnote-ref a')!;
    renderHook(() => useFootnotePreview({ current: container }, []));
  });

  afterEach(() => {
    closeActivePopover();
    vi.useRealTimers();
    document.body.innerHTML = '';
  });

  it('по тапу открывает лист вместо прыжка в конец полосы', () => {
    const click = tap(marker);

    expect(sheet()?.textContent).toContain('Тело примечания');
    // Без этого браузер уедет по якорю #fn:12 ещё до того, как лист появится.
    expect(click.defaultPrevented).toBe(true);
  });

  it('не тащит в лист обратную стрелку примечания', () => {
    tap(marker);
    expect(sheet()?.querySelector('a.fn-back')).toBeNull();
  });

  it('зовёт эндноут примечанием с номером, а звёздочную сноску — сноской', () => {
    tap(marker);
    expect(sheet()?.textContent).toContain('Примечание 12');

    closeActivePopover();
    tap(container.querySelectorAll('sup.footnote-ref a')[1]);
    expect(sheet()?.textContent).toContain('Сноска');
  });

  it('переход из листа прокручивает к примечанию и закрывает лист', () => {
    tap(marker);

    document.querySelector<HTMLElement>('.note-sheet-goto')!.click();

    expect(scrollIntoView).toHaveBeenCalled();
    expect(sheet()).toBeNull();
  });

  it('не мигает панелью наведения на эмулированных событиях мыши после тапа', () => {
    down(marker, 'touch');
    marker.dispatchEvent(new MouseEvent('mouseenter'));

    expect(document.querySelector('.footnote-preview-popover')).toBeNull();
  });

  it('оставляет мыши прежнюю панель по наведению', () => {
    down(marker, 'mouse');
    marker.dispatchEvent(new MouseEvent('mouseenter'));

    expect(document.querySelector('.footnote-preview-popover')?.textContent).toContain(
      'Тело примечания',
    );
    expect(sheet()).toBeNull();
  });
});
