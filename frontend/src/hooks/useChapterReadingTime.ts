import { useLayoutEffect, useState, type RefObject } from 'react';
import type { ChapterPage } from '../types';
import { countReadingWords } from '../utils/readingTime';

interface ReadingTime {
  chapterId: number | null;
  totalWords: number;
  remainingWords: number;
  ready: boolean;
}

interface PageMeasure {
  element: HTMLElement;
  words: number;
  before: number;
  top: number;
  bottom: number;
}

const EMPTY: ReadingTime = { chapterId: null, totalWords: 0, remainingWords: 0, ready: false };

/** Counts only the main text; the separate footnote block is outside this ref. */
function collectPages(root: HTMLDivElement): { measures: PageMeasure[]; totalWords: number } {
  let totalWords = 0;
  const measures = Array.from(root.querySelectorAll<HTMLElement>('.chapter-page-section')).map(
    (element) => {
      const rendered = element.querySelector('.page-html-content');
      const copy = rendered?.cloneNode(true) as HTMLElement | undefined;
      copy?.querySelectorAll('.page-marker').forEach((marker) => marker.remove());
      const text = copy?.textContent ?? '';
      const words = countReadingWords(text);
      const measure = { element, words, before: totalWords, top: 0, bottom: 0 };
      totalWords += words;
      return measure;
    },
  );
  return { measures, totalWords };
}

function remainingAt(measures: PageMeasure[], totalWords: number, position: number): number {
  for (const page of measures) {
    if (position < page.top) return totalWords - page.before;
    if (position < page.bottom) {
      const fraction = (page.bottom - position) / Math.max(1, page.bottom - page.top);
      return Math.ceil(totalWords - page.before - page.words + page.words * fraction);
    }
  }
  return 0;
}

export function useChapterReadingTime(
  contentRef: RefObject<HTMLDivElement>,
  toolbarRef: RefObject<HTMLDivElement>,
  pages: ChapterPage[],
  chapterId: number | null,
  enabled: boolean,
): { totalWords: number; remainingWords: number; ready: boolean } {
  const [value, setValue] = useState<ReadingTime>(EMPTY);

  useLayoutEffect(() => {
    const root = contentRef.current;
    if (!enabled || chapterId === null || !root) return;

    const { measures, totalWords } = collectPages(root);
    let frame = 0;

    const updateRemaining = () => {
      const toolbarBottom = toolbarRef.current?.getBoundingClientRect().bottom ?? 0;
      const readingLine = Math.min(toolbarBottom + 24, window.innerHeight * 0.4);
      // A sticky toolbar can condense while an anchor scrolls the document.
      // That moves the whole text without changing its size, so page geometry
      // is cached relative to the live top of the content, not the document.
      const rootRect = root.getBoundingClientRect();
      const position = Math.max(0, readingLine) - rootRect.top;
      const documentRoot = document.documentElement;
      const maxScroll = documentRoot.scrollHeight - documentRoot.clientHeight;
      const atEnd =
        maxScroll > 0 &&
        window.scrollY >= maxScroll - 2 &&
        rootRect.bottom <= window.innerHeight + 2;
      const remainingWords = atEnd ? 0 : remainingAt(measures, totalWords, position);
      setValue((previous) =>
        previous.chapterId === chapterId &&
        previous.totalWords === totalWords &&
        previous.remainingWords === remainingWords &&
        previous.ready
          ? previous
          : { chapterId, totalWords, remainingWords, ready: true },
      );
    };

    const queueUpdate = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        updateRemaining();
      });
    };

    const refreshGeometry = () => {
      const rootTop = root.getBoundingClientRect().top;
      for (const page of measures) {
        const rect = page.element.getBoundingClientRect();
        page.top = rect.top - rootTop;
        page.bottom = rect.bottom - rootTop;
      }
      queueUpdate();
    };

    window.addEventListener('scroll', queueUpdate, { passive: true });
    window.addEventListener('resize', refreshGeometry);
    const observer =
      typeof ResizeObserver === 'undefined' ? null : new ResizeObserver(refreshGeometry);
    observer?.observe(root);
    // Header metadata sits before the text. When its height changes (for
    // example after the total duration appears), the text moves without
    // changing its own size; the enclosing article does change height.
    if (root.parentElement) observer?.observe(root.parentElement);
    refreshGeometry();

    return () => {
      window.removeEventListener('scroll', queueUpdate);
      window.removeEventListener('resize', refreshGeometry);
      observer?.disconnect();
      if (frame) window.cancelAnimationFrame(frame);
    };
  }, [contentRef, toolbarRef, pages, chapterId, enabled]);

  return enabled && value.chapterId === chapterId ? value : EMPTY;
}
