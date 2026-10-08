import { describe, it, expect, afterEach, vi } from 'vitest';
import { act, renderHook } from '@testing-library/react';
import { useVisiblePage } from './useVisiblePage';

interface FakeEntry {
  target: Element;
  isIntersecting: boolean;
}

interface ObserverState {
  callback?: (entries: FakeEntry[]) => void;
  observed: Element[];
  disconnects: number;
  rootMargin?: string;
  created: number;
}

// jsdom не реализует IntersectionObserver, а его глобальная заглушка
// (src/test/setup.ts) молчит: колбэк некому вызвать. Подменяем заглушку
// собственной, которая отдаёт колбэк тесту.
function installObserverMock(): { state: ObserverState; restore: () => void } {
  const state: ObserverState = { observed: [], disconnects: 0, created: 0 };
  const original = window.IntersectionObserver;

  class Mock {
    constructor(callback: (entries: FakeEntry[]) => void, options?: IntersectionObserverInit) {
      state.callback = callback;
      state.rootMargin = options?.rootMargin;
      state.created += 1;
    }
    observe(element: Element) {
      state.observed.push(element);
    }
    unobserve() {}
    disconnect() {
      state.disconnects += 1;
    }
    takeRecords() {
      return [];
    }
  }

  window.IntersectionObserver = Mock as unknown as typeof IntersectionObserver;
  return {
    state,
    restore: () => {
      window.IntersectionObserver = original;
    },
  };
}

function addSection(pageNumber: number): HTMLElement {
  const section = document.createElement('div');
  section.className = 'chapter-page-section';
  section.id = `chapter-page-${pageNumber}`;
  document.body.appendChild(section);
  return section;
}

let restoreObserver: (() => void) | null = null;

afterEach(() => {
  restoreObserver?.();
  restoreObserver = null;
  document.body.innerHTML = '';
  vi.clearAllMocks();
});

describe('useVisiblePage', () => {
  it('наблюдает за всеми секциями страниц полосой чтения', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    addSection(69);
    addSection(71);

    renderHook(() => useVisiblePage(true, 'pages'));

    expect(state.observed).toHaveLength(2);
    expect(state.rootMargin).toBe('-30% 0px -65% 0px');
  });

  it('отдаёт номер единственной видимой страницы', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    const section = addSection(71);

    const { result } = renderHook(() => useVisiblePage(true, 'pages'));
    expect(result.current).toBeNull();

    act(() => state.callback!([{ target: section, isIntersecting: true }]));
    expect(result.current).toBe(71);
  });

  // Полосу чтения может пересекать сразу несколько секций: стык страниц или
  // короткая страница целиком внутри полосы. Читатель при этом смотрит на
  // верхнюю.
  it('при нескольких видимых отдаёт наименьший номер', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    const first = addSection(71);
    const second = addSection(72);

    const { result } = renderHook(() => useVisiblePage(true, 'pages'));

    act(() =>
      state.callback!([
        { target: second, isIntersecting: true },
        { target: first, isIntersecting: true },
      ]),
    );
    expect(result.current).toBe(71);
  });

  // Склейка полос переносит начало страницы в абзац предыдущей и оборачивает
  // его швом. Шов лежит ВНУТРИ секции предыдущей страницы, и пока читатель
  // смотрит на переехавший текст, объемлющая секция тоже пересекает полосу:
  // «наименьший номер» отдал бы предыдущую страницу на весь склеенный абзац.
  it('при вложенном шве отдаёт страницу шва, а не объемлющей секции', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    const previous = addSection(176);
    const seam = document.createElement('span');
    seam.className = 'chapter-page-section page-seam';
    seam.id = 'chapter-page-177';
    previous.appendChild(seam);

    const { result } = renderHook(() => useVisiblePage(true, 'pages'));

    act(() =>
      state.callback!([
        { target: previous, isIntersecting: true },
        { target: seam, isIntersecting: true },
      ]),
    );
    expect(result.current).toBe(177);
  });

  it('держит последнее значение, когда видимых не осталось', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    const section = addSection(71);

    const { result } = renderHook(() => useVisiblePage(true, 'pages'));

    act(() => state.callback!([{ target: section, isIntersecting: true }]));
    act(() => state.callback!([{ target: section, isIntersecting: false }]));
    expect(result.current).toBe(71);
  });

  it('не создаёт наблюдателя, пока выключен, и отдаёт null', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    addSection(71);

    const { result } = renderHook(() => useVisiblePage(false, 'pages'));

    expect(state.created).toBe(0);
    expect(result.current).toBeNull();
  });

  it('пересоздаёт наблюдателя при смене набора секций', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    addSection(71);

    const { rerender } = renderHook(({ key }: { key: string }) => useVisiblePage(true, key), {
      initialProps: { key: 'первая глава' },
    });
    expect(state.created).toBe(1);

    rerender({ key: 'вторая глава' });
    expect(state.created).toBe(2);
    expect(state.disconnects).toBe(1);
  });

  // Продуктивный вызов передаёт sectionsKey массивом объектов (страницы
  // главы), а не примитивом. Склейка в строку различала бы такие ключи
  // только по длине — эта регрессия ловится именно на одинаковой длине.
  it('сбрасывает страницу при смене sectionsKey на другой массив той же длины', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    const section = addSection(71);

    const { result, rerender } = renderHook(
      ({ key }: { key: unknown[] }) => useVisiblePage(true, key),
      { initialProps: { key: [{ id: 1 }, { id: 2 }] } },
    );

    act(() => state.callback!([{ target: section, isIntersecting: true }]));
    expect(result.current).toBe(71);

    rerender({ key: [{ id: 3 }, { id: 4 }] });
    expect(result.current).toBeNull();
  });

  // Поток дописывает секции в конец, а не заменяет набор: страница, на
  // которую читатель смотрит, от приезда следующего окна не меняется.
  // Обнуление здесь стоило бы полосы прогресса, дёрнутой в ноль, пропавшего
  // заголовка главы и откатившегося на кадр backHref.
  it('в режиме append держит страницу при догрузке секций', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    const section = addSection(43);

    const { result, rerender } = renderHook(
      ({ key }: { key: unknown[] }) => useVisiblePage(true, key, 'append'),
      { initialProps: { key: [{ from: 43 }] } },
    );

    act(() => state.callback!([{ target: section, isIntersecting: true }]));
    expect(result.current).toBe(43);

    addSection(53);
    rerender({ key: [{ from: 43 }, { from: 53 }] });

    expect(result.current).toBe(43);
  });

  // Та самая квадратичность, ради ухода от которой окна и сделаны отдельными
  // компонентами: пересозданный наблюдатель заново регистрирует ВСЕ
  // накопленные секции, и к семисотой странице это дороже, чем сэкономила
  // кусочная загрузка.
  it('в режиме append подписывает только новые секции', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    addSection(43);
    addSection(44);

    const { rerender } = renderHook(
      ({ key }: { key: unknown[] }) => useVisiblePage(true, key, 'append'),
      { initialProps: { key: [{ from: 43 }] } },
    );
    expect(state.observed).toHaveLength(2);

    const third = addSection(53);
    const fourth = addSection(54);
    rerender({ key: [{ from: 43 }, { from: 53 }] });

    expect(state.created).toBe(1);
    expect(state.disconnects).toBe(0);
    expect(state.observed).toEqual([...state.observed.slice(0, 2), third, fourth]);
  });

  it('отписывается при размонтировании', () => {
    const { state, restore } = installObserverMock();
    restoreObserver = restore;
    addSection(71);

    const { unmount } = renderHook(() => useVisiblePage(true, 'pages'));
    unmount();

    expect(state.disconnects).toBe(1);
  });
});
