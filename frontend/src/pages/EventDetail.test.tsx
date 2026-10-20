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

function renderPage(path = '/organizations/org-a/e1') {
  return render(
    <MemoryRouter initialEntries={[path]}>
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

  it('адрес с #section-2 после загрузки прокручивает к этому пункту', async () => {
    const scroll = vi.fn();
    Element.prototype.scrollIntoView = scroll;
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(DETAIL));
    const { container } = renderPage('/organizations/org-a/e1#section-2');
    await screen.findByRole('heading', { level: 1, name: 'I съезд' });
    await vi.waitFor(() => expect(scroll).toHaveBeenCalled());
    expect(scroll.mock.contexts[0]).toBe(container.querySelector('h2#section-2'));
  });
  it('сотруднику — «читателям не видно» у пустого и закрытого, у открытого нет', async () => {
    useAuth.setState({
      user: { role: 'editor' } as never,
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    const marked: Detail = {
      ...DETAIL,
      sections: [
        {
          position: 0,
          title: 'Открытый пункт',
          entries: [
            { label: 'Открытая строка', targets: [target({})] },
            { label: 'Закрытая строка', targets: [target({ chapter_id: 74, closed: true })] },
            { label: 'Строка без целей', targets: [] },
          ],
        },
        { position: 1, title: 'Пустой пункт', entries: [] },
        {
          position: 2,
          title: 'Битый пункт',
          entries: [
            {
              label: 'Битая и закрытая',
              targets: [
                target({ chapter_id: null, broken: true, title: 'Старое' }),
                target({ chapter_id: 75, closed: true, label: 'засед. 1' }),
              ],
            },
          ],
        },
      ],
    };
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(marked));
    const { container } = renderPage();
    await screen.findByRole('heading', { level: 1, name: 'I съезд' });
    const sectionMarked = (n: number) =>
      container
        .querySelector(`h2#section-${n}`)
        ?.parentElement?.querySelector(':scope > .event-hidden-mark') != null;
    expect(sectionMarked(1)).toBe(false);
    expect(sectionMarked(2)).toBe(true);
    expect(sectionMarked(3)).toBe(true);
    const entryMarked = (text: RegExp) =>
      [...container.querySelectorAll('li.event-entry')]
        .find((li) => text.test(li.textContent ?? ''))
        ?.querySelector('.event-hidden-mark') != null;
    expect(entryMarked(/Открытая строка/)).toBe(false);
    expect(entryMarked(/Закрытая строка/)).toBe(true);
    expect(entryMarked(/Строка без целей/)).toBe(true);
    expect(entryMarked(/Битая и закрытая/)).toBe(true);
  });

  it('читателю пометки «читателям не видно» нет', async () => {
    const withEmpty: Detail = {
      ...DETAIL,
      sections: [...DETAIL.sections, { position: 2, title: 'Пустой пункт', entries: [] }],
    };
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(withEmpty));
    renderPage();
    await screen.findByRole('heading', { level: 1, name: 'I съезд' });
    expect(screen.queryByText('читателям не видно')).not.toBeInTheDocument();
  });

  it('битая %-последовательность в адресе не роняет страницу', async () => {
    Element.prototype.scrollIntoView = vi.fn();
    vi.spyOn(eventsApi, 'event').mockImplementation(() => ok(DETAIL));
    renderPage('/organizations/org-a/e1#%E0');
    expect(await screen.findByRole('heading', { level: 1, name: 'I съезд' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Доклад' })).toBeInTheDocument();
  });
});
