import { useEffect } from 'react';
// @ts-expect-error - auto-render doesn't have type definitions
import renderMathInElement from 'katex/dist/contrib/auto-render';

// Разделители перечислены один раз на весь проект: страница тома и блок
// фрагмента понятия обязаны понимать формулы одинаково, а две копии конфига
// разъезжаются молча.
const DELIMITERS = [
  { left: '$$', right: '$$', display: true },
  { left: '$', right: '$', display: false },
  { left: '\\[', right: '\\]', display: true },
  { left: '\\(', right: '\\)', display: false },
];

/** Прогоняет KaTeX по содержимому контейнера при каждой смене deps. */
export function useKatex(containerRef: React.RefObject<HTMLElement>, deps: unknown[]): void {
  useEffect(() => {
    const container = containerRef.current;
    if (!container) return;
    try {
      renderMathInElement(container, { delimiters: DELIMITERS, throwOnError: false });
    } catch (err) {
      // Битая формула не должна ронять страницу целиком.
      console.error('Math rendering error:', err);
    }
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
}
