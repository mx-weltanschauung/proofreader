import React from 'react';
import { Link } from 'react-router-dom';
import type { VolumeSummary } from '../types';
import { plural } from '../utils/volumeLabel';
import { workPath } from '../utils/paths';
import './EditionPrefaces.css';

interface Props {
  prefaces: VolumeSummary[];
}

/**
 * Предисловия, относящиеся не к тому, а к группе томов: ко всему собранию или
 * к его части. Стоят перед томами, как в печати, и номера тома не несут.
 */
export const EditionPrefaces: React.FC<Props> = ({ prefaces }) => {
  if (prefaces.length === 0) return null;

  return (
    <section className="edition-prefaces" aria-label="Предваряющие материалы">
      <ul className="edition-prefaces-list">
        {prefaces.map((preface) => (
          <li key={preface.id} className="edition-prefaces-item">
            <Link to={workPath(preface)}>{preface.title}</Link>
            <span className="edition-prefaces-pages">
              {plural(preface.pages_total, ['страница', 'страницы', 'страниц'])}
            </span>
          </li>
        ))}
      </ul>
    </section>
  );
};
