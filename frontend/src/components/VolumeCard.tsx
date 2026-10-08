import React from 'react';
import type { VolumeSummary } from '../types';
import { chapterLabel, spineNumber, plural, worksPlural } from '../utils/volumeLabel';
import { cardPosition } from '../utils/volumeCardPosition';
import './VolumeCard.css';

interface Props {
  volume: VolumeSummary;
  /** Прямоугольник корешка в координатах окна. */
  anchor: DOMRect;
  /** id для aria-describedby на корешке. */
  id: string;
}

export const VolumeCard: React.FC<Props> = ({ volume, anchor, id }) => {
  // Только ручная подпись, а не volumeLabel(): выведенная подпись — это
  // заглавия первых работ, а они и так перечислены ниже, с объёмом каждой.
  // Повторённая, она вытесняла из карточки всё остальное: у ленинского тома 1
  // подпись в 243 знака занимала 164 пикселя из 220, и ни списка работ, ни
  // сводки читатель не видел.
  const label = (volume.shelf_label ?? '').trim();
  const works = volume.top_chapters ?? [];
  const { left, top } = cardPosition(anchor);

  return (
    <div className="volume-card" id={id} role="tooltip" style={{ left, top }}>
      <div className="volume-card-number">Том {spineNumber(volume)}</div>
      {label && <div className="volume-card-label">{label}</div>}

      {works.length > 0 && (
        <ul className="volume-card-works">
          {works.map((work) => (
            <li key={work.title} className="volume-card-work">
              <span className="volume-card-work-title">{chapterLabel(work.title)}</span>
              <span className="volume-card-work-pages">{work.pages} с.</span>
            </li>
          ))}
        </ul>
      )}

      <div className="volume-card-stats">
        {plural(volume.pages_total, ['страница', 'страницы', 'страниц'])}
        {' · '}
        {worksPlural(volume.chapters_total)}
      </div>
    </div>
  );
};
