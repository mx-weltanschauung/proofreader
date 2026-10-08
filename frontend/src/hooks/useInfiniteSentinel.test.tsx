import { useRef } from 'react';
import { describe, expect, it, vi, beforeEach, afterEach } from 'vitest';
import { render } from '@testing-library/react';

import { useInfiniteSentinel } from './useInfiniteSentinel';

// jsdom не знает IntersectionObserver. Подставной запоминает наблюдаемый узел
// и переданные настройки и даёт тесту сообщить о появлении узла в кадре.
class FakeObserver {
  static instances: FakeObserver[] = [];
  callback: IntersectionObserverCallback;
  options?: IntersectionObserverInit;
  observed: Element[] = [];
  disconnected = false;

  constructor(callback: IntersectionObserverCallback, options?: IntersectionObserverInit) {
    this.callback = callback;
    this.options = options;
    FakeObserver.instances.push(this);
  }
  observe(el: Element) {
    this.observed.push(el);
  }
  unobserve() {}
  disconnect() {
    this.disconnected = true;
  }
  enter() {
    this.callback(
      [{ isIntersecting: true } as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
  leave() {
    this.callback(
      [{ isIntersecting: false } as IntersectionObserverEntry],
      this as unknown as IntersectionObserver,
    );
  }
}

interface ProbeProps {
  hasMore: boolean;
  loadMore: () => void;
  rootMargin?: string;
}

const Probe: React.FC<ProbeProps> = ({ hasMore, loadMore, rootMargin }) => {
  const ref = useRef<HTMLDivElement>(null);
  useInfiniteSentinel(ref, hasMore, loadMore, rootMargin);
  return <div ref={ref} data-testid="sentinel" />;
};

let original: typeof IntersectionObserver;

beforeEach(() => {
  original = window.IntersectionObserver;
  FakeObserver.instances = [];
  window.IntersectionObserver = FakeObserver as unknown as typeof IntersectionObserver;
});

afterEach(() => {
  window.IntersectionObserver = original;
});

describe('useInfiniteSentinel', () => {
  it('появление сентинеля в кадре запрашивает следующую порцию', () => {
    const loadMore = vi.fn();
    render(<Probe hasMore loadMore={loadMore} />);

    expect(FakeObserver.instances).toHaveLength(1);
    FakeObserver.instances[0].enter();

    expect(loadMore).toHaveBeenCalledTimes(1);
  });

  it('уход сентинеля из кадра ничего не грузит', () => {
    const loadMore = vi.fn();
    render(<Probe hasMore loadMore={loadMore} />);

    FakeObserver.instances[0].leave();

    expect(loadMore).not.toHaveBeenCalled();
  });

  // Кончились данные — наблюдать не за чем: иначе сентинел, оставшийся в
  // кадре у конца списка, дёргал бы загрузку впустую.
  it('без продолжения наблюдатель не заводится', () => {
    const loadMore = vi.fn();
    render(<Probe hasMore={false} loadMore={loadMore} />);

    expect(FakeObserver.instances).toHaveLength(0);
    expect(loadMore).not.toHaveBeenCalled();
  });

  // Поток тома грузит следующее окно заранее, за экран до конца, чтобы
  // читатель не упирался в паузу; лента фрагментов ждёт самого сентинеля.
  it('запас предзагрузки передаётся наблюдателю', () => {
    render(<Probe hasMore loadMore={vi.fn()} rootMargin="0px 0px 1200px 0px" />);

    expect(FakeObserver.instances[0].options?.rootMargin).toBe('0px 0px 1200px 0px');
  });

  it('без указанного запаса наблюдатель настроек не получает', () => {
    render(<Probe hasMore loadMore={vi.fn()} />);

    expect(FakeObserver.instances[0].options?.rootMargin).toBeUndefined();
  });

  it('размонтирование отключает наблюдателя', () => {
    const { unmount } = render(<Probe hasMore loadMore={vi.fn()} />);

    unmount();

    expect(FakeObserver.instances[0].disconnected).toBe(true);
  });
});
