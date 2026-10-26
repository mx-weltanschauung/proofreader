import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventsApi, shelfApi } from '../services/api';
import { OrganizationEdit } from './OrganizationEdit';
import { EventEdit } from './EventEdit';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const ORG = {
  id: 1,
  slug: 'org-a',
  title: 'Организация А',
  description: 'Описание',
  position: 0,
  kinds: [
    {
      id: 1,
      organization_id: 1,
      title: 'Съезды',
      position: 0,
      events: [
        { id: 5, slug: 'e1', title: 'I съезд', dates: '1920', materials_total: 0 },
        { id: 6, slug: 'e2', title: 'II съезд', dates: '1921', materials_total: 0 },
      ],
    },
  ],
};

function setup(path = '/organizations/org-a/edit') {
  if (!vi.isMockFunction(shelfApi.get)) {
    vi.spyOn(shelfApi, 'get').mockReturnValue(
      ok({ editions: [], loose_works: [], journals: [], organizations: [] }),
    );
  }
  if (!vi.isMockFunction(eventsApi.organization)) {
    vi.spyOn(eventsApi, 'organization').mockReturnValue(ok(ORG));
  }
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/organizations/:slug/edit" element={<OrganizationEdit />} />
        <Route path="/organizations/:slug/:eventSlug/edit" element={<EventEdit />} />
        <Route path="/organizations/:slug/:eventSlug" element={<div>Страница события</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

afterEach(() => vi.restoreAllMocks());

describe('OrganizationEdit', () => {
  it('адрес /edit остаётся экраном редактора, а не событием со слагом edit', async () => {
    setup();
    expect(await screen.findByText('Правка организации')).toBeInTheDocument();
    expect(screen.queryByText('Страница события')).not.toBeInTheDocument();
  });

  it('показывает сообщение сервера при слаге edit', async () => {
    vi.spyOn(eventsApi, 'createEvent').mockRejectedValue({
      response: {
        status: 400,
        data: { message: 'Слова edit и new заняты адресами экрана редактора' },
      },
    });
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    await u.type(screen.getByLabelText('Слаг (Съезды)'), 'edit');
    await u.type(screen.getByLabelText('Заглавие (Съезды)'), 'Новый');
    await u.click(screen.getByRole('button', { name: 'Создать событие (Съезды)' }));
    expect(
      await screen.findByText('Слова edit и new заняты адресами экрана редактора'),
    ).toBeInTheDocument();
  });

  it('удаляет событие только после подтверждения', async () => {
    const del = vi.spyOn(eventsApi, 'deleteEvent').mockReturnValue(ok(undefined));
    const confirm = vi.spyOn(window, 'confirm');
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    confirm.mockReturnValueOnce(false);
    await u.click(screen.getByRole('button', { name: 'Удалить событие I съезд' }));
    expect(del).not.toHaveBeenCalled();
    confirm.mockReturnValueOnce(true);
    await u.click(screen.getByRole('button', { name: 'Удалить событие I съезд' }));
    await waitFor(() => expect(del).toHaveBeenCalledWith(5));
  });

  it('создание события шлёт вид, слаг и заглавие', async () => {
    const create = vi.spyOn(eventsApi, 'createEvent').mockReturnValue(ok({} as never));
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    await u.type(screen.getByLabelText('Слаг (Съезды)'), 'iii');
    await u.type(screen.getByLabelText('Заглавие (Съезды)'), 'III съезд');
    await u.click(screen.getByRole('button', { name: 'Создать событие (Съезды)' }));
    await waitFor(() =>
      expect(create).toHaveBeenCalledWith(1, {
        slug: 'iii',
        title: 'III съезд',
        dates: '',
        place: '',
        description: '',
        position: 2,
      }),
    );
  });

  const fullEvent = (id: number, slug: string, title: string, position: number) =>
    ({
      id,
      kind_id: 1,
      slug,
      title,
      dates: '',
      place: '',
      description: '',
      position,
      organization: ORG,
      kind_title: 'Съезды',
      sections: [],
    }) as never;

  const THREE = {
    ...ORG,
    kinds: [
      {
        ...ORG.kinds[0],
        events: [
          ...ORG.kinds[0].events,
          { id: 7, slug: 'e3', title: 'III съезд', dates: '', materials_total: 0 },
        ],
      },
    ],
  };

  function stubEvents(positions: number[]) {
    const byslug: Record<string, never> = {
      e1: fullEvent(5, 'e1', 'I съезд', positions[0]),
      e2: fullEvent(6, 'e2', 'II съезд', positions[1]),
      e3: fullEvent(7, 'e3', 'III съезд', positions[2]),
    };
    vi.spyOn(eventsApi, 'organization').mockReturnValue(ok(THREE));
    vi.spyOn(eventsApi, 'event').mockImplementation((_o, slug) => ok(byslug[slug]));
  }

  it('перестановка нумерует по новому порядку: хранимые 10/20/30', async () => {
    stubEvents([10, 20, 30]);
    const upd = vi.spyOn(eventsApi, 'updateEvent').mockReturnValue(ok({} as never));
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    const loads = vi.mocked(eventsApi.organization).mock.calls.length;
    await u.click(screen.getByRole('button', { name: 'Событие II съезд вниз' }));
    await waitFor(() => expect(upd).toHaveBeenCalledTimes(3));
    expect(upd.mock.calls.map((c) => [c[0], c[1].position])).toEqual([
      [5, 0],
      [7, 1],
      [6, 2],
    ]);
    await waitFor(() =>
      expect(vi.mocked(eventsApi.organization).mock.calls.length).toBeGreaterThan(loads),
    );
  });

  it('перестановка при равных хранимых позициях разводит все', async () => {
    stubEvents([0, 0, 0]);
    const upd = vi.spyOn(eventsApi, 'updateEvent').mockReturnValue(ok({} as never));
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    await u.click(screen.getByRole('button', { name: 'Событие I съезд вниз' }));
    await waitFor(() => expect(upd).toHaveBeenCalledTimes(2));
    expect(upd.mock.calls.map((c) => [c[0], c[1].position])).toEqual([
      [5, 1],
      [7, 2],
    ]);
  });

  it('провал записи при перестановке всё равно перечитывает и показывает ошибку', async () => {
    stubEvents([0, 1, 2]);
    vi.spyOn(eventsApi, 'updateEvent').mockRejectedValue({
      response: { status: 500, data: { message: 'База недоступна' } },
    });
    setup();
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    const loads = vi.mocked(eventsApi.organization).mock.calls.length;
    await u.click(screen.getByRole('button', { name: 'Событие I съезд вниз' }));
    expect(await screen.findByText('База недоступна')).toBeInTheDocument();
    expect(vi.mocked(eventsApi.organization).mock.calls.length).toBeGreaterThan(loads);
  });

  it('перестановка видов с равными позициями меняет порядок', async () => {
    const two = {
      ...ORG,
      kinds: [
        { ...ORG.kinds[0], position: 0 },
        { id: 2, organization_id: 1, title: 'Пленумы', position: 0, events: [] },
      ],
    };
    vi.spyOn(eventsApi, 'organization').mockReturnValue(ok(two));
    const upd = vi.spyOn(eventsApi, 'updateKind').mockReturnValue(ok({} as never));
    render(
      <MemoryRouter initialEntries={['/organizations/org-a/edit']}>
        <Routes>
          <Route path="/organizations/:slug/edit" element={<OrganizationEdit />} />
        </Routes>
      </MemoryRouter>,
    );
    const u = userEvent.setup();
    await screen.findByText('Правка организации');
    await u.click(screen.getByRole('button', { name: 'Вид Съезды вниз' }));
    await waitFor(() => expect(upd).toHaveBeenCalled());
    expect(upd.mock.calls.map((c) => [c[0], c[1].position])).toEqual([[1, 1]]);
  });

  it('раздел на главной уходит в правку организации вместе с заглавием', async () => {
    vi.spyOn(eventsApi, 'organization').mockReturnValue(ok({ ...ORG, shelf_group: 'Раздел Б' }));
    const update = vi.spyOn(eventsApi, 'updateOrganization').mockReturnValue(ok(ORG));
    setup();
    const u = userEvent.setup();
    const field = await screen.findByLabelText('Раздел на главной');
    expect(field).toHaveValue('Раздел Б');
    await u.clear(field);
    await u.type(field, 'Раздел В');
    await u.click(screen.getByRole('button', { name: 'Сохранить организацию' }));
    expect(update).toHaveBeenCalledWith(1, {
      title: 'Организация А',
      description: 'Описание',
      position: 0,
      shelf_group: 'Раздел В',
    });
  });
});
