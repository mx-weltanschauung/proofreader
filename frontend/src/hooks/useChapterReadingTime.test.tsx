import { act, render, screen } from '@testing-library/react';
import { createRef } from 'react';
import { afterEach, beforeEach, describe, expect, it, vi } from 'vitest';
import type { ChapterPage } from '../types';
import { useChapterReadingTime } from './useChapterReadingTime';

let scrollY = 0;
let layoutShift = 0;
let nextFrame = 1;
const frames = new Map<number, FrameRequestCallback>();
let resizeCallback: ResizeObserverCallback | null = null;
const observed = new Set<Element>();

function flushFrame() {
  act(() => {
    const pending = [...frames.values()];
    frames.clear();
    pending.forEach((callback) => callback(0));
  });
}

function moveTo(y: number) {
  scrollY = y;
  void act(() => window.dispatchEvent(new Event('scroll')));
  flushFrame();
}

function rect(top: number, height: number): DOMRect {
  return new DOMRect(0, top + layoutShift - scrollY, 500, height);
}

function page(number: number, words: number): ChapterPage & { text: string } {
  return { page_number: number, html: '', blank: false, text: 'слово '.repeat(words) };
}

const contentRef = createRef<HTMLDivElement>();
const toolbarRef = createRef<HTMLDivElement>();

function Probe({ pages }: { pages: (ChapterPage & { text?: string })[] }) {
  const time = useChapterReadingTime(contentRef, toolbarRef, pages, 7, true);
  return (
    <article>
      <div ref={toolbarRef} data-testid="toolbar" />
      <div ref={contentRef}>
        {pages.map((p) =>
          p.text === undefined ? null : (
            <section className="chapter-page-section" key={p.page_number}>
              <span className="page-marker">{p.page_number}</span>
              <div
                className="page-html-content"
                {...(p.html
                  ? { dangerouslySetInnerHTML: { __html: p.html } }
                  : { children: p.text })}
              />
            </section>
          ),
        )}
      </div>
      <div className="chapter-footnotes">{'примечание '.repeat(180)}</div>
      <output data-testid="time">
        {time.ready ? `${time.totalWords}/${time.remainingWords}` : 'pending'}
      </output>
    </article>
  );
}

function layOut(
  starts: { top: number; height: number }[],
  notify: 'window' | 'observer' = 'window',
) {
  vi.spyOn(screen.getByTestId('toolbar'), 'getBoundingClientRect').mockImplementation(
    () => new DOMRect(0, 0, 500, 50),
  );
  const rootHeight = Math.max(...starts.map((section) => section.top + section.height)) - 100;
  vi.spyOn(contentRef.current!, 'getBoundingClientRect').mockImplementation(() =>
    rect(100, rootHeight),
  );
  const sections = document.querySelectorAll<HTMLElement>('.chapter-page-section');
  sections.forEach((section, index) => {
    vi.spyOn(section, 'getBoundingClientRect').mockImplementation(() =>
      rect(starts[index].top, starts[index].height),
    );
  });
  if (notify === 'observer') {
    const article = document.querySelector('article');
    if (article && observed.has(article)) {
      act(() => resizeCallback?.([], {} as ResizeObserver));
    }
  } else {
    void act(() => window.dispatchEvent(new Event('resize')));
  }
  flushFrame();
}

beforeEach(() => {
  scrollY = 0;
  layoutShift = 0;
  frames.clear();
  vi.spyOn(window, 'requestAnimationFrame').mockImplementation((callback) => {
    const id = nextFrame++;
    frames.set(id, callback);
    return id;
  });
  vi.spyOn(window, 'cancelAnimationFrame').mockImplementation((id) => {
    frames.delete(id);
  });
  Object.defineProperty(window, 'scrollY', { configurable: true, get: () => scrollY });
  Object.defineProperty(window, 'innerHeight', { configurable: true, value: 800 });
  Object.defineProperty(document.documentElement, 'scrollHeight', {
    configurable: true,
    value: 800,
  });
  Object.defineProperty(document.documentElement, 'clientHeight', {
    configurable: true,
    value: 800,
  });
  resizeCallback = null;
  observed.clear();
  vi.stubGlobal(
    'ResizeObserver',
    class {
      constructor(callback: ResizeObserverCallback) {
        resizeCallback = callback;
      }
      observe(element: Element) {
        observed.add(element);
      }
      disconnect() {}
    },
  );
});

afterEach(() => {
  vi.restoreAllMocks();
  vi.unstubAllGlobals();
});

describe('useChapterReadingTime', () => {
  it('weights remaining time by text across unequal pages and within a page', () => {
    render(<Probe pages={[page(1, 180), page(2, 360)]} />);
    layOut([
      { top: 100, height: 200 },
      { top: 300, height: 200 },
    ]);
    expect(screen.getByTestId('time')).toHaveTextContent('540/540');

    moveTo(126); // reading line 74 px below viewport top, at document y=200
    expect(screen.getByTestId('time')).toHaveTextContent('540/450');

    moveTo(326); // middle of the longer second page
    expect(screen.getByTestId('time')).toHaveTextContent('540/180');

    moveTo(450); // past the main text, inside footnotes
    expect(screen.getByTestId('time')).toHaveTextContent('540/0');
    moveTo(0);
    expect(screen.getByTestId('time')).toHaveTextContent('540/540');
  });

  it('counts only rendered main text, including a stitched page once', () => {
    const stitched = {
      ...page(1, 0),
      html: `<p>${'слово '.repeat(180)}<span class="chapter-page-section page-seam"><a class="page-marker">2</a> ${'слово '.repeat(180)}</span></p>`,
    };
    render(<Probe pages={[stitched, { ...page(2, 0), text: undefined }]} />);
    layOut([
      { top: 100, height: 400 },
      { top: 300, height: 100 },
    ]);
    expect(screen.getByTestId('time')).toHaveTextContent('360/360');
  });

  it('returns no estimate when rendered pages have no readable words', () => {
    render(<Probe pages={[page(1, 0)]} />);
    layOut([{ top: 100, height: 200 }]);
    expect(screen.getByTestId('time')).toHaveTextContent('0/0');
  });

  it('refreshes page geometry after typography changes without changing total words', () => {
    render(<Probe pages={[page(1, 180), page(2, 360)]} />);
    layOut([
      { top: 100, height: 200 },
      { top: 300, height: 200 },
    ]);
    moveTo(326);
    expect(screen.getByTestId('time')).toHaveTextContent('540/180');
    layOut(
      [
        { top: 100, height: 200 },
        { top: 500, height: 200 },
      ],
      'observer',
    );
    expect(screen.getByTestId('time')).toHaveTextContent('540/360');
  });

  it('uses the moved text position after a sticky toolbar condenses during an anchor jump', () => {
    render(<Probe pages={[page(1, 180), page(2, 180)]} />);
    layOut([
      { top: 100, height: 200 },
      { top: 300, height: 200 },
    ]);
    layoutShift = -40;
    moveTo(186); // page 2 now starts at document y=260, on the reading line
    expect(screen.getByTestId('time')).toHaveTextContent('360/180');
  });

  it('finishes at the scroll limit and restores the remainder when scrolling back', () => {
    render(<Probe pages={[page(1, 180)]} />);
    layOut([{ top: 100, height: 1000 }]);
    Object.defineProperty(document.documentElement, 'scrollHeight', {
      configurable: true,
      value: 1250,
    });
    expect(screen.getByTestId('time')).toHaveTextContent('180/180');
    moveTo(450); // max scroll; the last text is visible below the reading line
    expect(screen.getByTestId('time')).toHaveTextContent('180/0');
    moveTo(200);
    expect(screen.getByTestId('time')).not.toHaveTextContent('180/0');
  });

  it('does not round the last unread word down to zero', () => {
    render(<Probe pages={[page(1, 1)]} />);
    layOut([{ top: 100, height: 1000 }]);
    moveTo(900); // 126 px of the page still lie below the reading line
    expect(screen.getByTestId('time')).toHaveTextContent('1/1');
  });
});
