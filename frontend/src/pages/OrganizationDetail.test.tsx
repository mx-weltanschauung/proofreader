import { render, screen, within } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
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
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  function renderOrg(org: EventOrganizationDetail) {
    vi.spyOn(eventsApi, 'organization').mockImplementation(() => ok(org));
    return render(
      <MemoryRouter initialEntries={['/organizations/org-a']}>
        <Routes>
          <Route path="/organizations/:slug" element={<OrganizationDetail />} />
        </Routes>
      </MemoryRouter>,
    );
  }

  const WITH_EMPTY: EventOrganizationDetail = {
    ...ORG,
    kinds: [
      {
        ...ORG.kinds[0],
        events: [
          ...ORG.kinds[0].events,
          { id: 4, slug: 'e3', title: 'III съезд', dates: '', materials_total: 0 },
        ],
      },
      { id: 3, organization_id: 1, title: 'Пустой вид', position: 2, events: [] },
    ],
  };

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
  it('сотруднику — «читателям не видно» у события без материалов и у пустого вида', async () => {
    useAuth.setState({
      user: { role: 'editor' } as never,
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    const { container } = renderOrg(WITH_EMPTY);
    await screen.findByRole('heading', { level: 1, name: 'Организация А' });
    const eventMarked = (name: string) =>
      screen.getByRole('link', { name }).closest('li')?.querySelector('.org-hidden-mark') != null;
    expect(eventMarked('I съезд')).toBe(false);
    expect(eventMarked('III съезд')).toBe(true);
    const kindMarked = [...container.querySelectorAll('section.org-kind')].map(
      (sec) => sec.querySelector(':scope > .org-hidden-mark') != null,
    );
    expect(kindMarked).toEqual([false, true]);
  });

  it('читателю пометки «читателям не видно» нет', async () => {
    renderOrg(WITH_EMPTY);
    await screen.findByRole('heading', { level: 1, name: 'Организация А' });
    expect(screen.queryByText('читателям не видно')).not.toBeInTheDocument();
  });
});
