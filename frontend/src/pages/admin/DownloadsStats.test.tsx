import { render, screen, within } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import { expect, it, vi } from 'vitest';
import { statsApi } from '../../services/api';
import { DownloadsStats } from './DownloadsStats';

vi.mock('../../services/api', () => ({ statsApi: { downloads: vi.fn() } }));

it('показывает форматы, архив и топ только известных томов и глав', async () => {
  vi.mocked(statsApi.downloads).mockResolvedValue({
    data: {
      summary: [
        { kind: 'work', format: 'epub', views: 4 },
        { kind: 'chapter', format: 'pdf', views: 2 },
        { kind: 'collection', format: 'm3u', views: 3 },
        { kind: 'archive', format: '', views: 1 },
      ],
      top: [
        {
          kind: 'work',
          work_id: 49,
          entity_id: 0,
          format: 'epub',
          title: 'Том',
          missing: false,
          views: 4,
        },
        {
          kind: 'chapter',
          work_id: 49,
          entity_id: 101,
          format: 'pdf',
          title: 'Глава',
          missing: false,
          views: 2,
        },
        {
          kind: 'work',
          work_id: 999,
          entity_id: 0,
          format: 'fb2',
          title: '',
          missing: true,
          views: 1,
        },
      ],
    },
  } as never);
  render(
    <MemoryRouter>
      <DownloadsStats days={30} />
    </MemoryRouter>,
  );
  const section = await screen.findByRole('region', { name: 'Выдачи файлов' });
  expect(within(section).getAllByText('EPUB').length).toBeGreaterThan(0);
  expect(within(section).getAllByText('PDF').length).toBeGreaterThan(0);
  expect(within(section).getByText('M3U')).toBeInTheDocument();
  expect(within(section).getByText('Архив')).toBeInTheDocument();
  expect(within(section).getByRole('link', { name: 'Том' })).toHaveAttribute('href', '/works/49');
  expect(within(section).getByRole('link', { name: 'Глава' })).toHaveAttribute(
    'href',
    '/works/49/chapters/101',
  );
  expect(within(section).getByText(/снято/)).toBeInTheDocument();
  expect(within(section).queryByRole('link', { name: /подборк/i })).toBeNull();
});

it('ошибка раздела не скрывает остальную страницу', async () => {
  vi.mocked(statsApi.downloads).mockRejectedValueOnce(new Error('boom'));
  render(
    <MemoryRouter>
      <DownloadsStats days={7} />
    </MemoryRouter>,
  );
  expect(await screen.findByText('Не удалось загрузить выдачи')).toBeInTheDocument();
});
