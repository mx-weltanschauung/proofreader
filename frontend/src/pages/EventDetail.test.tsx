import { render, screen } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { EventDetail } from './EventDetail';
import type { EventDetail as Detail, EventTarget } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function target(over: Partial<EventTarget>): EventTarget {
  return {
    kind: 'chapter',
    work_id: 7,
    chapter_id: 70,
    label: '',
    title: 'Глава',
    work_title: 'Том',
    broken: false,
    closed: false,
    ...over,
  };
}

const DETAIL: Detail = {
  id: 1,
  kind_id: 1,
  slug: 'e1',
  title: 'I съезд',
  dates: '1920',
  place: 'Город',
  description: 'Описание события',
  position: 0,
  organization: { id: 1, slug: 'org-a', title: 'Организация А', description: '', position: 0 },
  kind_title: 'Съезды',
  sections: [
    {
      position: 0,
      title: '1. Открытие',
      entries: [
        { label: 'Доклад', targets: [target({ label: 'стенограмма' })] },
        {
          label: 'Прения',
          targets: [
            target({ chapter_id: 71, label: 'засед. 25' }),
            target({ chapter_id: 72, label: 'засед. 26' }),
            target({ chapter_id: 73, label: 'засед. 27' }),
          ],
        },
      ],
    },
    {
      position: 1,
      title: '2. Закрытие',
      entries: [
        {
          label: 'Резолюция',
          targets: [target({ chapter_id: null, broken: true, title: 'Старое заглавие' })],
        },
      ],
    },
  ],
};

function renderPage() {
  return render(
    <MemoryRouter initialEntries={['/organizations/org-a/e1']}>
      <Routes>
        <Route path="/organizations/:slug/:eventSlug" element={<EventDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('EventDetail', () => {
  afterEach(() => {
    vi.restoreAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('пункты с якорями, строка с одной целью — ссылка, с тремя — три ссылки', async () => {
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(DETAIL));
    const { container } = renderPage();
    expect(await screen.findByRole('heading', { level: 1, name: 'I съезд' })).toBeInTheDocument();
    expect(container.querySelector('h2#section-1')).toHaveTextContent('1. Открытие');
    expect(container.querySelector('h2#section-2')).toHaveTextContent('2. Закрытие');
    expect(screen.getByRole('link', { name: 'Доклад' })).toHaveAttribute(
      'href',
      '/works/7/chapters/70',
    );
    for (const [i, n] of [25, 26, 27].entries()) {
      expect(screen.getByRole('link', { name: `засед. ${n}` })).toHaveAttribute(
        'href',
        `/works/7/chapters/${71 + i}`,
      );
    }
    expect(screen.getByText(/^Прения:/)).toBeInTheDocument();
  });

  it('якорь пункта следует хранимой позиции, а не порядку в ответе', async () => {
    const shifted: Detail = {
      ...DETAIL,
      sections: [{ ...DETAIL.sections[1], position: 4 }],
    };
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(shifted));
    const { container } = renderPage();
    await screen.findByRole('heading', { level: 1, name: 'I съезд' });
    expect(container.querySelector('h2#section-5')).not.toBeNull();
  });

  it('сотруднику битая цель — текст без ссылки и «Править»', async () => {
    useAuth.setState({
      user: { role: 'editor' } as never,
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(DETAIL));
    renderPage();
    expect(await screen.findByText('глава не найдена: Старое заглавие')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /Старое заглавие/ })).not.toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Править' })).toHaveAttribute(
      'href',
      '/organizations/org-a/e1/edit',
    );
  });
});
