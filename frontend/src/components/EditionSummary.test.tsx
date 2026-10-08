import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { EditionSummary } from './EditionSummary';
import type { Edition, VolumeSummary } from '../types';

const EDITION: Edition = {
  id: 2,
  title: 'К. Маркс и Ф. Энгельс. Сочинения',
  slug: 'mae-2',
  description: 'Второе издание',
  created_at: '',
  updated_at: '',
};

function volume(over: Partial<VolumeSummary>): VolumeSummary {
  return {
    id: 1,
    title: 'Том',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '2026-01-01T00:00:00Z',
    pages_total: 0,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

const VOLUMES = [
  volume({ id: 1, volume_number: 1, pages_total: 600, pages_by_status: { вычитана: 300 } }),
  volume({
    id: 16,
    volume_number: 16,
    pages_total: 400,
    pages_by_status: { вычитано_машиной: 200 },
    updated_at: '2026-08-01T00:00:00Z',
  }),
];

describe('EditionSummary', () => {
  it('показывает объём собрания и молчит про вычитку', () => {
    render(
      <MemoryRouter>
        <EditionSummary edition={EDITION} volumes={VOLUMES} />
      </MemoryRouter>,
    );

    expect(screen.getByRole('heading', { name: EDITION.title })).toBeInTheDocument();
    expect(screen.getByText('Второе издание')).toBeInTheDocument();
    expect(screen.getByText(/^2 тома, 1 000 страниц\./)).toBeInTheDocument();
    // Читальня показывает собрание, а не ход работы над ним: доли вычитки
    // здесь нет ни цифрой, ни полоской.
    expect(screen.queryByText(/вычитан/i)).toBeNull();
    expect(screen.queryByText(/%/)).toBeNull();
    expect(document.querySelector('.edition-summary-bar')).toBeNull();
  });

  it('с titleHref делает заглавие ссылкой', () => {
    render(
      <MemoryRouter>
        <EditionSummary edition={EDITION} volumes={VOLUMES} titleHref="/editions/2" />
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: EDITION.title })).toHaveAttribute(
      'href',
      '/editions/2',
    );
  });

  it('пустое собрание говорит об этом прямо', () => {
    render(
      <MemoryRouter>
        <EditionSummary edition={EDITION} volumes={[]} />
      </MemoryRouter>,
    );
    expect(screen.getByText(/Пока ни одного тома/)).toBeInTheDocument();
    expect(screen.queryByText('0%')).toBeNull();
  });
});
