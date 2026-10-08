import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { EditionHighlights } from './EditionHighlights';
import type { EditionHighlight } from '../types';

const capital: EditionHighlight = {
  chapter_id: 2066,
  chapter_slug: 'kapital',
  chapter_title: 'КАПИТАЛ. Критика политической экономии',
  work_id: 47,
  work_slug: 'mae-t23',
  volume_number: 23,
  volume_part: null,
  label: 'Капитал, т. I',
};
const ideology: EditionHighlight = {
  chapter_id: 11,
  chapter_slug: '',
  chapter_title: 'Немецкая идеология. Критика',
  work_id: 3,
  work_slug: 'mae-t03',
  volume_number: 3,
  volume_part: null,
  label: '',
};

function renderList(list: EditionHighlight[]) {
  return render(
    <MemoryRouter>
      <EditionHighlights highlights={list} />
    </MemoryRouter>,
  );
}

describe('EditionHighlights', () => {
  it('ведёт прямо на главу и называет том в имени ссылки', () => {
    renderList([capital, ideology]);
    const link = screen.getByRole('link', { name: 'Капитал, т. I, т. 23' });
    expect(link).toHaveAttribute('href', '/works/47-mae-t23/chapters/2066-kapital');
    expect(link).toHaveTextContent('Капитал, т. I');
    // Пустой слаг главы законен: адрес из одного номера.
    expect(screen.getByRole('link', { name: 'Немецкая идеология, т. 3' })).toHaveAttribute(
      'href',
      '/works/3-mae-t03/chapters/11',
    );
  });

  it('сохраняет порядок пунктов', () => {
    renderList([ideology, capital]);
    expect(screen.getAllByRole('link').map((a) => a.textContent)).toEqual([
      'Немецкая идеология',
      'Капитал, т. I',
    ]);
  });

  it('без избранного ничего не рисует', () => {
    const { container } = renderList([]);
    expect(container).toBeEmptyDOMElement();
  });
});
