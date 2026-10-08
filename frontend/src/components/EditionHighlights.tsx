import React from 'react';
import { Link } from 'react-router-dom';
import type { EditionHighlight } from '../types';
import { highlightCoordinate, highlightLabel } from '../utils/highlightLabel';
import { chapterPath } from '../utils/paths';
import './EditionHighlights.css';

interface Props {
  highlights: EditionHighlight[];
}

/**
 * Избранные работы собрания строкой ссылок — прямо на главу, минуя том.
 * Новичку это точка входа: он не знает, что «Капитал» — это т. 23.
 *
 * Координата тома в строке не печатается, чтобы строка оставалась короткой, —
 * она в имени ссылки и в подсказке: «Капитал, т. I, т. 23». Тот же компонент
 * рисует предпросмотр на экране правки — подобия там нет.
 */
export const EditionHighlights: React.FC<Props> = ({ highlights }) => {
  if (highlights.length === 0) return null;
  return (
    <ul className="edition-highlights" aria-label="Избранные работы">
      {highlights.map((h) => {
        const label = highlightLabel(h);
        const coordinate = highlightCoordinate(h);
        const full = coordinate ? `${label}, ${coordinate}` : label;
        return (
          <li key={h.chapter_id}>
            <Link
              to={chapterPath(
                { id: h.work_id, slug: h.work_slug },
                { id: h.chapter_id, slug: h.chapter_slug },
              )}
              aria-label={full}
              title={full}
            >
              {label}
            </Link>
          </li>
        );
      })}
    </ul>
  );
};
