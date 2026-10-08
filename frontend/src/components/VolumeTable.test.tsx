import { describe, it, expect } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { VolumeTable } from './VolumeTable';
import type { VolumeSummary } from '../types';

const VOLUMES = [
  {
    id: 3,
    title: 'Немецкая идеология',
    volume_number: 3,
    page_offset: 0,
    pages_total: 600,
    pages_by_status: { вычитана: 300, вычитано_машиной: 120 },
    chapters_total: 18,
    updated_at: '',
  },
  {
    id: 90,
    title: 'Предметный указатель',
    page_offset: 0,
    pages_total: 0,
    pages_by_status: {},
    chapters_total: 0,
    updated_at: '',
  },
] as VolumeSummary[];

describe('VolumeTable', () => {
  it('строкой на том: координата, название-ссылка, объём', () => {
    render(
      <MemoryRouter>
        <VolumeTable volumes={VOLUMES} />
      </MemoryRouter>,
    );

    expect(screen.getByRole('link', { name: 'Немецкая идеология' })).toHaveAttribute(
      'href',
      '/works/3',
    );
    expect(screen.getByText('т. 3')).toBeInTheDocument();
    expect(screen.getByText('600')).toBeInTheDocument();
    expect(screen.getByText('18')).toBeInTheDocument();
  });

  // Колонка «Вычитано» убрана вместе с остальными следами хода работы.
  it('не показывает долю вычитки', () => {
    render(
      <MemoryRouter>
        <VolumeTable volumes={VOLUMES} />
      </MemoryRouter>,
    );
    expect(screen.queryByRole('columnheader', { name: 'Вычитано' })).toBeNull();
    expect(screen.queryByText('70%')).toBeNull();
  });
});
