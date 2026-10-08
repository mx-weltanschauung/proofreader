import React from 'react';
import { Link } from 'react-router-dom';
import type { Edition, VolumeSummary } from '../types';
import { editionCaption } from '../utils/editionCaption';
import './EditionSummary.css';

interface Props {
  edition: Edition;
  volumes: VolumeSummary[];
  /** Задан — заглавие становится ссылкой (главная); нет — остаётся заголовком. */
  titleHref?: string;
  /**
   * Уровень заголовка. На странице собрания оно одно и заглавие — h1; на
   * главной собраний пять, и пять h1 на одной странице сломали бы обход по
   * заголовкам.
   */
  level?: 'h1' | 'h2';
}

export const EditionSummary: React.FC<Props> = ({ edition, volumes, titleHref, level = 'h1' }) => {
  const Heading = level;

  return (
    <header className="edition-summary">
      <Heading className="edition-summary-title">
        {titleHref ? <Link to={titleHref}>{edition.title}</Link> : edition.title}
      </Heading>

      {edition.description && <p className="edition-summary-description">{edition.description}</p>}

      {/* Три факта одной фразой: сколько томов из скольких, сколько страниц,
          какой пришёл последним. Прежде счётчик стоял здесь, а «последний
          загруженный — т. 11» — в подвале полки, у правого края страницы, в
          девяти сотнях пикселей от всего, к чему относится. */}
      <p className="edition-summary-counts">{editionCaption(volumes, edition.volumes_planned)}</p>
    </header>
  );
};
