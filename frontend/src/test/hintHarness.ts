import { act } from '@testing-library/react';
import { vi } from 'vitest';

/** Наблюдатели, созданные компонентами: тест сам решает, что они увидели. */
const observers: { callback: IntersectionObserverCallback; target: Element | null }[] = [];

class FakeObserver {
  target: Element | null = null;
  constructor(public callback: IntersectionObserverCallback) {
    observers.push(this);
  }
  observe(el: Element) {
    this.target = el;
  }
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
}

/** Ставит управляемый наблюдатель и фальшивые таймеры. Звать в beforeEach. */
export function installHintHarness(): void {
  observers.length = 0;
  vi.useFakeTimers();
  vi.stubGlobal('IntersectionObserver', FakeObserver);
}

/** Звать в afterEach. */
export function removeHintHarness(): void {
  vi.useRealTimers();
  vi.unstubAllGlobals();
}

/** Сообщает всем наблюдателям, виден якорь или нет. */
export function sayVisible(visible: boolean): void {
  act(() => {
    for (const o of observers) {
      o.callback(
        [{ isIntersecting: visible, target: o.target } as unknown as IntersectionObserverEntry],
        {} as IntersectionObserver,
      );
    }
  });
}

/**
 * Сообщает про видимость только тем наблюдателям, что следят за этим узлом.
 *
 * Нужно там, где один и тот же орган смонтирован дважды (кнопка «Aa» живёт и
 * в шапке сайта, и в панели чтения): общий sayVisible сказал бы одно и то же
 * обеим копиям, а весь смысл проверки — что видна ровно одна.
 */
export function sayVisibleFor(target: Element, visible: boolean): void {
  act(() => {
    for (const o of observers) {
      if (o.target !== target) continue;
      o.callback(
        [{ isIntersecting: visible, target: o.target } as unknown as IntersectionObserverEntry],
        {} as IntersectionObserver,
      );
    }
  });
}

/** Проматывает таймеры внутри act. */
export function advance(ms: number): void {
  act(() => void vi.advanceTimersByTime(ms));
}
