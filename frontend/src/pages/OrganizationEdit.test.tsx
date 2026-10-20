import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { eventsApi } from '../services/api';
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
  vi.spyOn(eventsApi, 'organization').mockReturnValue(ok(ORG));
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
});
