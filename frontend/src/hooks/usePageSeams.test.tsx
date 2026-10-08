import { describe, it, expect } from 'vitest';
import { act, render } from '@testing-library/react';
import { useRef } from 'react';
import { MemoryRouter } from 'react-router-dom';
import { usePageSeams } from './usePageSeams';

const SEAM_HTML =
  '<p class="merged">то очевидно, что я ' +
  '<span class="chapter-page-section page-seam" id="chapter-page-177" data-page="177" tabindex="-1">' +
  '<a class="page-marker" href="/works/62/pages/177" aria-label="Страница 177">177</a>' +
  'никогда бы не решился</span></p>';

const Host: React.FC = () => {
  const ref = useRef<HTMLDivElement>(null);
  usePageSeams(ref, []);
  return <div ref={ref} dangerouslySetInnerHTML={{ __html: SEAM_HTML }} />;
};

/** jsdom не считает раскладку: прямоугольники подставляются вручную. */
function stubRects(element: Element, rects: { top: number; left: number }[]): void {
  const boxes = rects.map((r) => ({ ...r, right: 0, bottom: 0, width: 0, height: 0 }));
  Object.defineProperty(element, 'getClientRects', {
    configurable: true,
    value: () => boxes,
  });
  Object.defineProperty(element, 'getBoundingClientRect', {
    configurable: true,
    value: () => boxes[0],
  });
}

describe('usePageSeams', () => {
  // Маркер номера absolute-позиционирован относительно склеенного абзаца, а
  // страница начинается в его середине: без замера номер сел бы на первую
  // строку абзаца, то есть на предыдущую страницу.
  it('ставит маркер на строку, с которой начинается новая полоса', async () => {
    const { container } = render(
      <MemoryRouter>
        <Host />
      </MemoryRouter>,
    );

    const paragraph = container.querySelector('p.merged') as HTMLElement;
    const seam = container.querySelector('.page-seam') as HTMLElement;
    const marker = container.querySelector('.page-marker') as HTMLElement;

    stubRects(paragraph, [{ top: 100, left: 0 }]);
    // Шов начинается на четвёртой строке абзаца и тянется на две строки.
    stubRects(seam, [
      { top: 172, left: 300 },
      { top: 196, left: 0 },
    ]);

    // Пересчёт отложен до кадра отрисовки — сотня швов главы не должна
    // пересчитываться на каждое событие изменения размера.
    await act(async () => {
      window.dispatchEvent(new Event('resize'));
      await new Promise((resolve) => requestAnimationFrame(resolve));
    });

    expect(marker.style.top).toBe('72px');
  });
});
