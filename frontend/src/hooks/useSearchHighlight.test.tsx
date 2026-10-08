import { describe, expect, it, vi } from 'vitest';
import { render } from '@testing-library/react';
import { useRef } from 'react';
import { useSearchHighlight } from './useSearchHighlight';

/**
 * Проба повторяет форму настоящих потребителей: реф на контейнер, а текст —
 * в дочернем .page-html-content (после сужения области обхода подсветка
 * смотрит только туда, не в весь контейнер).
 */
const Probe: React.FC<{
  terms: string[];
  scrollToFirst: boolean;
  html: string;
  depKey: string;
}> = ({ terms, scrollToFirst, html, depKey }) => {
  const ref = useRef<HTMLDivElement>(null);
  useSearchHighlight(ref, terms, scrollToFirst, [depKey]);
  return (
    <div ref={ref}>
      <div className="page-html-content" dangerouslySetInnerHTML={{ __html: html }} />
    </div>
  );
};

describe('useSearchHighlight: прокрутка к первому совпадению', () => {
  // jsdom не реализует scrollIntoView — src/test/setup.ts подменяет его на
  // no-op глобально, так что счётчик вызовов нужен свой, тот же приём, что и
  // в Help.test.tsx.
  it('прокручивает один раз и не повторяет это ни на смене лемм, ни на смене содержимого', () => {
    const spy = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(function (
      this: Element,
    ) {
      void this;
    });

    const { rerender } = render(
      <Probe
        terms={['гегел']}
        scrollToFirst={true}
        html="<p>Гегеля и Маркса читали</p>"
        depKey="a"
      />,
    );
    expect(spy).toHaveBeenCalledTimes(1);

    // Смена лемм: находится новое совпадение («Маркса»), но прокрутка не
    // должна повториться — «один раз за жизнь компонента», а не «один раз на
    // набор лемм».
    rerender(
      <Probe
        terms={['маркс']}
        scrollToFirst={true}
        html="<p>Гегеля и Маркса читали</p>"
        depKey="a"
      />,
    );
    expect(spy).toHaveBeenCalledTimes(1);

    // Смена содержимого (аналог нового окна потока/новой главы) с теми же
    // леммами — тоже не должна повторить прокрутку.
    rerender(
      <Probe terms={['маркс']} scrollToFirst={true} html="<p>Маркса читали снова</p>" depKey="b" />,
    );
    expect(spy).toHaveBeenCalledTimes(1);

    spy.mockRestore();
  });
});
