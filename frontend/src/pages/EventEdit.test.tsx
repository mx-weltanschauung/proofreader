import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { chaptersApi, eventsApi, worksApi } from '../services/api';
import { EventEdit } from './EventEdit';
import type { Chapter, EventDetail, EventTarget, Work } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}
function fail(status: number, message: string) {
  return Promise.reject({ response: { status, data: { message } } });
}

const ORG = {
  id: 1,
  slug: 'org-a',
  title: 'Организация А',
  description: '',
  position: 0,
  kinds: [
    { id: 1, organization_id: 1, title: 'Съезды', position: 0, events: [] },
    { id: 2, organization_id: 1, title: 'Пленумы', position: 1, events: [] },
  ],
};

function target(over: Partial<EventTarget>): EventTarget {
  return {
    kind: 'chapter',
    work_id: 7,
    chapter_id: 70,
    label: '',
    title: 'Снимок',
    work_title: 'Том',
    broken: false,
    closed: false,
    ...over,
  };
}

function detail(sections: EventDetail['sections']): EventDetail {
  return {
    id: 5,
    kind_id: 1,
    slug: 'e1',
    title: 'I съезд',
    dates: '1920',
    place: 'Город',
    description: 'Описание',
    position: 3,
    organization: { id: 1, slug: 'org-a', title: 'Организация А', description: '', position: 0 },
    kind_title: 'Съезды',
    sections,
  };
}

function chapter(id: number, title: string, children: Chapter[] = []): Chapter {
  return { id, work_id: 7, title, is_apparatus: false, children } as unknown as Chapter;
}

function setup(sections: EventDetail['sections'] = []) {
  vi.spyOn(eventsApi, 'organization').mockReturnValue(ok(ORG));
  vi.spyOn(eventsApi, 'event').mockReturnValue(ok(detail(sections)));
  vi.spyOn(worksApi, 'get').mockReturnValue(ok({ id: 7, title: 'Том семь' } as Work));
  vi.spyOn(chaptersApi, 'list').mockReturnValue(
    ok([chapter(70, 'Доклад'), chapter(71, 'Прения', [chapter(72, 'Вложенная')])]),
  );
  return render(
    <MemoryRouter initialEntries={['/organizations/org-a/e1/edit']}>
      <Routes>
        <Route path="/organizations/:slug/:eventSlug/edit" element={<EventEdit />} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => vi.restoreAllMocks());

describe('EventEdit', () => {
  it('собирает повестку из пункта, строки и цели с главой', async () => {
    const replace = vi.spyOn(eventsApi, 'replaceAgenda').mockReturnValue(ok(detail([])));
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка события');
    await u.click(screen.getByRole('button', { name: 'Добавить пункт' }));
    await u.type(screen.getByLabelText('Заглавие пункта 1'), 'Пункт один');
    await u.click(screen.getByRole('button', { name: 'Добавить строку в пункт 1' }));
    await u.type(screen.getByLabelText('Подпись строки 1.1'), 'Доклад');
    await u.click(screen.getByRole('button', { name: 'Добавить цель в строку 1.1' }));
    await u.type(screen.getByLabelText('id тома 1.1.1'), '7');
    await screen.findByText('Том семь');
    await u.selectOptions(await screen.findByLabelText('Глава 1.1.1'), '70');
    await u.type(screen.getByLabelText('Подпись цели 1.1.1'), 'Текст');
    await u.click(screen.getByRole('button', { name: 'Сохранить повестку' }));
    await waitFor(() => expect(replace).toHaveBeenCalled());
    expect(replace).toHaveBeenCalledWith(5, {
      sections: [
        {
          title: 'Пункт один',
          entries: [
            {
              label: 'Доклад',
              targets: [{ kind: 'chapter', work_id: 7, chapter_id: 70, label: 'Текст' }],
            },
          ],
        },
      ],
    });
  });

  it('«весь том» уходит как kind work без chapter_id', async () => {
    const replace = vi.spyOn(eventsApi, 'replaceAgenda').mockReturnValue(ok(detail([])));
    setup([
      {
        position: 0,
        title: 'П',
        entries: [{ label: 'С', targets: [target({ kind: 'work', chapter_id: null })] }],
      },
    ]);
    const u = userEvent.setup();
    await screen.findByText('Правка события');
    await screen.findByText('Том семь');
    await u.click(screen.getByRole('button', { name: 'Сохранить повестку' }));
    await waitFor(() => expect(replace).toHaveBeenCalled());
    const body = replace.mock.calls[0][1];
    expect(body.sections[0].entries[0].targets).toEqual([{ kind: 'work', work_id: 7, label: '' }]);
    expect('chapter_id' in body.sections[0].entries[0].targets[0]).toBe(false);
  });

  it('«вверх» меняет порядок пунктов в теле', async () => {
    const replace = vi.spyOn(eventsApi, 'replaceAgenda').mockReturnValue(ok(detail([])));
    setup([
      { position: 0, title: 'Первый', entries: [] },
      { position: 1, title: 'Второй', entries: [] },
    ]);
    const u = userEvent.setup();
    await screen.findByText('Правка события');
    await u.click(screen.getByRole('button', { name: 'Пункт 2 вверх' }));
    await u.click(screen.getByRole('button', { name: 'Сохранить повестку' }));
    await waitFor(() => expect(replace).toHaveBeenCalled());
    expect(replace.mock.calls[0][1].sections.map((s) => s.title)).toEqual(['Второй', 'Первый']);
  });

  it('показывает текст 400 сервера', async () => {
    vi.spyOn(eventsApi, 'replaceAgenda').mockImplementation(() =>
      fail(400, 'пункт 1, строка 1: не указана подпись строки'),
    );
    setup([{ position: 0, title: 'П', entries: [{ label: '', targets: [] }] }]);
    const u = userEvent.setup();
    await screen.findByText('Правка события');
    await u.click(screen.getByRole('button', { name: 'Сохранить повестку' }));
    expect(
      await screen.findByText('пункт 1, строка 1: не указана подпись строки'),
    ).toBeInTheDocument();
  });

  it('при битой цели сохранение неактивно, пока глава не выбрана заново', async () => {
    setup([
      {
        position: 0,
        title: 'П',
        entries: [
          {
            label: 'С',
            targets: [target({ chapter_id: null, broken: true, title: 'Старая глава' })],
          },
        ],
      },
    ]);
    const u = userEvent.setup();
    await screen.findByText('Правка события');
    expect(screen.getByText(/глава не найдена: Старая глава/)).toBeInTheDocument();
    expect(screen.getByText('выберите главу заново')).toBeInTheDocument();
    const save = screen.getByRole('button', { name: 'Сохранить повестку' });
    expect(save).toBeDisabled();
    await screen.findByText('Том семь');
    await u.selectOptions(await screen.findByLabelText('Глава 1.1.1'), '70');
    expect(save).toBeEnabled();
  });

  it('шапка уходит полной заменой', async () => {
    const update = vi.spyOn(eventsApi, 'updateEvent').mockReturnValue(ok({} as never));
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка события');
    await u.selectOptions(screen.getByLabelText('Вид'), '2');
    await u.click(screen.getByRole('button', { name: 'Сохранить' }));
    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update).toHaveBeenCalledWith(5, {
      kind_id: 2,
      title: 'I съезд',
      dates: '1920',
      place: 'Город',
      description: 'Описание',
      position: 3,
    });
  });
});
