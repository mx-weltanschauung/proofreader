import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { EditionPrefaces } from './EditionPrefaces';
import type { VolumeSummary } from '../types';

function preface(over: Partial<VolumeSummary>): VolumeSummary {
  return {
    id: 101,
    title: 'К. Маркс и Ф. Энгельс. Сочинения. Предисловие ко второму изданию',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    owner_id: 1,
    created_at: '',
    updated_at: '',
    role: 'edition_front_matter',
    precedes_volume: 1,
    pages_total: 5,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

describe('EditionPrefaces', () => {
  it('показывает заглавие ссылкой на работу', () => {
    render(
      <MemoryRouter>
        <EditionPrefaces prefaces={[preface({})]} />
      </MemoryRouter>,
    );
    const link = screen.getByRole('link', {
      name: /Предисловие ко второму изданию/,
    });
    expect(link).toHaveAttribute('href', '/works/101');
  });

  it('не рисует ярлык «Том»: номера у такой работы нет', () => {
    render(
      <MemoryRouter>
        <EditionPrefaces prefaces={[preface({})]} />
      </MemoryRouter>,
    );
    expect(screen.queryByText(/^Том\s/)).toBeNull();
  });

  it('ничего не рисует, когда предваряющих работ нет', () => {
    const { container } = render(
      <MemoryRouter>
        <EditionPrefaces prefaces={[]} />
      </MemoryRouter>,
    );
    expect(container).toBeEmptyDOMElement();
  });
});
