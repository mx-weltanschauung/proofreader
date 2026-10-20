import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventsApi } from '../services/api';
import { ChapterEvents } from './ChapterEvents';
import type { ChapterEventLink } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

describe('ChapterEvents', () => {
  afterEach(() => vi.restoreAllMocks());

  it('ссылка «событие — пункт» ведёт на якорь пункта', async () => {
    const links: ChapterEventLink[] = [
      {
        organization_slug: 'org-a',
        organization_title: 'Организация А',
        event_slug: 'event-6',
        event_title: 'VI событие',
        section_title: '3. Программа',
        section_position: 2,
      },
    ];
    vi.spyOn(eventsApi, 'chapterEvents').mockImplementation(() => ok(links));
    render(
      <MemoryRouter>
        <ChapterEvents workId={1} chapterId={2} />
      </MemoryRouter>,
    );
    const link = await screen.findByRole('link', { name: 'VI событие — 3. Программа' });
    expect(link).toHaveAttribute('href', '/organizations/org-a/event-6#section-3');
    expect(screen.getByText(/Входит в:/)).toBeInTheDocument();
  });

  it('пустой ответ — ничего', async () => {
    const spy = vi.spyOn(eventsApi, 'chapterEvents').mockImplementation(() => ok([]));
    const { container } = render(
      <MemoryRouter>
        <ChapterEvents workId={1} chapterId={2} />
      </MemoryRouter>,
    );
    await vi.waitFor(() => expect(spy).toHaveBeenCalled());
    expect(container).toBeEmptyDOMElement();
  });
});
