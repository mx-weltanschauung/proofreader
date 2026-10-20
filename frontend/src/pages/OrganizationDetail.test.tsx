import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventsApi } from '../services/api';
import { OrganizationDetail } from './OrganizationDetail';
import type { EventOrganizationDetail } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const ORG: EventOrganizationDetail = {
  id: 1,
  slug: 'org-a',
  title: 'Организация А',
  description: 'Описание организации',
  position: 0,
  kinds: [
    {
      id: 1,
      organization_id: 1,
      title: 'Съезды',
      position: 0,
      events: [
        { id: 1, slug: 'e1', title: 'I съезд', dates: '1920', materials_total: 5 },
        { id: 2, slug: 'e2', title: 'II съезд', dates: '1921', materials_total: 1 },
      ],
    },
    {
      id: 2,
      organization_id: 1,
      title: 'Пленумы',
      position: 1,
      events: [{ id: 3, slug: 'p1', title: 'I пленум', dates: '', materials_total: 2 }],
    },
  ],
};

describe('OrganizationDetail', () => {
  afterEach(() => vi.restoreAllMocks());

  it('виды заголовками, события ссылками в порядке ответа', async () => {
    vi.spyOn(eventsApi, 'organization').mockImplementation(() => ok(ORG));
    render(
      <MemoryRouter initialEntries={['/organizations/org-a']}>
        <Routes>
          <Route path="/organizations/:slug" element={<OrganizationDetail />} />
        </Routes>
      </MemoryRouter>,
    );
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Организация А' }),
    ).toBeInTheDocument();
    expect(screen.getByText('Описание организации')).toBeInTheDocument();
    const headings = screen.getAllByRole('heading', { level: 2 }).map((h) => h.textContent);
    expect(headings).toEqual(['Съезды', 'Пленумы']);
    const links = screen
      .getAllByRole('link')
      .filter((a) => a.getAttribute('href')?.includes('/org-a/'));
    expect(links.map((a) => a.getAttribute('href'))).toEqual([
      '/organizations/org-a/e1',
      '/organizations/org-a/e2',
      '/organizations/org-a/p1',
    ]);
    const first = links[0].closest('li') as HTMLElement;
    expect(within(first).getByText(/5 материалов/)).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Править' })).not.toBeInTheDocument();
  });
});
