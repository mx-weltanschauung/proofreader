import { useEffect } from 'react';

const DRAWER_WIDTH = 320;
const ATTR = 'data-reading-panel';

/**
 * The drawer sits over the page's right margin, so the reading column never
 * moves. When the margin is too narrow to hold it, the drawer covers the text
 * and needs a scrim instead.
 *
 * This cannot be a media query: the condition compares the column's width
 * — a character count at a user-chosen font size — against the viewport
 * margin, and CSS cannot compare two lengths.
 */
export function useDrawerOverlap(open: boolean): void {
  useEffect(() => {
    const root = document.documentElement;
    if (!open) {
      root.removeAttribute(ATTR);
      return;
    }

    const getColumn = () =>
      document.querySelector('.chapter-pages-content') ??
      document.querySelector('.page-html-content');

    const measure = () => {
      const column = getColumn();
      const columnRight = column ? column.getBoundingClientRect().right : window.innerWidth;
      const covers = columnRight > window.innerWidth - DRAWER_WIDTH;
      root.setAttribute(ATTR, covers ? 'covering' : 'beside');
    };

    measure();
    window.addEventListener('resize', measure);

    // The column resizes when the reader changes font size or measure, with no
    // resize event to go with it. Observe whichever element the measurement
    // above actually used - on the page view that is .page-html-content, not
    // .chapter-pages-content, since that class isn't present there.
    const observer = new ResizeObserver(measure);
    const column = getColumn();
    if (column) observer.observe(column);

    return () => {
      window.removeEventListener('resize', measure);
      observer.disconnect();
      root.removeAttribute(ATTR);
    };
  }, [open]);
}
