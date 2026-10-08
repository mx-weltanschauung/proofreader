import { describe, expect, it } from 'vitest';
import { render } from '@testing-library/react';
import { useRef } from 'react';
import { useKatex } from './useKatex';

const Probe: React.FC<{ html: string }> = ({ html }) => {
  const ref = useRef<HTMLDivElement>(null);
  useKatex(ref, [html]);
  return <div ref={ref} dangerouslySetInnerHTML={{ __html: html }} />;
};

describe('useKatex', () => {
  it('рендерит формулу в переданном контейнере', () => {
    const { container } = render(<Probe html={'<p>формула $a^2$ в строке</p>'} />);

    // KaTeX подменяет разметку на свою — признак, что проход состоялся.
    expect(container.querySelector('.katex')).not.toBeNull();
  });

  it('на контейнере без формул ничего не ломает', () => {
    const { container } = render(<Probe html={'<p>обычный текст</p>'} />);

    expect(container.textContent).toBe('обычный текст');
    expect(container.querySelector('.katex')).toBeNull();
  });
});
