import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { expect, it, vi } from 'vitest';
import { statsApi } from '../../services/api';
import { VisiblePagesStats } from './VisiblePagesStats';

vi.mock('../../services/api', () => ({ statsApi: { visiblePages: vi.fn() } }));

it('показывает все полосы, посетителе-дни и ссылку на полосу', async () => {
  vi.mocked(statsApi.visiblePages).mockResolvedValue({
    data: {
      collecting_since: '2026-09-28T00:00:00Z',
      pages: [
        { page_number: 1, visitors: 2 },
        { page_number: 2, visitors: 1 },
        { page_number: 3, visitors: 0 },
      ],
    },
  } as never);
  render(
    <MemoryRouter>
      <VisiblePagesStats workId={49} chapterId={101} days={30} />
    </MemoryRouter>,
  );
  expect(await screen.findByText('Посетителе-дни')).toBeInTheDocument();
  expect(screen.getByRole('columnheader', { name: /увидели полосу/i })).toBeInTheDocument();
  expect(screen.getByRole('link', { name: /3/ })).toHaveAttribute('href', '/works/49/pages/3');
  expect(screen.getByText('0')).toBeInTheDocument();
});

it('до первого события показывает пустое состояние', async () => {
  vi.mocked(statsApi.visiblePages).mockResolvedValue({
    data: { collecting_since: null, pages: [{ page_number: 1, visitors: 0 }] },
  } as never);
  render(
    <MemoryRouter>
      <VisiblePagesStats workId={50} chapterId={102} days={30} />
    </MemoryRouter>,
  );
  expect(await screen.findByText('Данных пока нет')).toBeInTheDocument();
});
