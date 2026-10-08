import { describe, it, expect, vi, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { editionsApi, worksApi } from '../services/api';
import type { Edition, Work } from '../types';
import { WorkEdit } from './WorkEdit';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const EDITIONS: Edition[] = [
  {
    id: 1,
    title: 'Сочинения, 2-е изд.',
    slug: 'mae-2',
    description: '',
    created_at: '',
    updated_at: '',
  },
];

const WORK: Work = {
  id: 5,
  title: 'Немецкая идеология',
  author: 'К. Маркс, Ф. Энгельс',
  language: 'ru',
  country: 'DE',
  file_path: '',
  status: 'draft',
  owner_id: 1,
  edition_id: 1,
  volume_number: 4,
  page_offset: 12,
  created_at: '',
  updated_at: '',
};

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Routes>
        <Route path="/works/:id/edit" element={<WorkEdit />} />
        <Route path="/works/:id" element={<div>страница работы</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('WorkEdit', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('форма подписана по-русски', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));

    renderAt('/works/5/edit');

    expect(await screen.findByRole('heading', { name: 'Изменить работу' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Сохранить' })).toBeInTheDocument();
  });

  it('очищенные координаты уходят как явные null, а не undefined или 0', async () => {
    // Сервер различает отсутствующий ключ (сохранить прежнее) и null
    // (очистить); undefined из JSON просто исчезает, а 0 — валидное, но
    // отвергаемое сервером число для volume_number.
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));
    const update = vi.spyOn(worksApi, 'update').mockImplementation(() => ok(WORK));

    renderAt('/works/5/edit');

    await waitFor(() => expect(screen.getByLabelText(/Номер тома/)).toHaveValue(4));

    await userEvent.selectOptions(screen.getByLabelText(/Собрание/), '');
    await userEvent.clear(screen.getByLabelText(/Номер тома/));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    const body = update.mock.calls[0][1];
    expect(body.edition_id).toBeNull();
    expect(body.volume_number).toBeNull();
    expect(body.volume_part).toBeNull();
  });

  it('код 409 показывает текст про занятый том, а не общий текст «Не удалось сохранить работу»', async () => {
    // Тело ответа приходит строкой от http.Error, а не JSON с полем
    // message — apiErrorMessage до серверного текста не дотягивается, и
    // единственный надёжный признак «том занят» — код ответа.
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));
    vi.spyOn(worksApi, 'update').mockImplementation(() =>
      Promise.reject({ response: { status: 409 } }),
    );

    renderAt('/works/5/edit');

    await waitFor(() => expect(screen.getByLabelText(/Номер тома/)).toHaveValue(4));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    expect(
      await screen.findByText('Этот том уже занят другой работой в собрании'),
    ).toBeInTheDocument();
    expect(screen.queryByText('Не удалось сохранить работу')).not.toBeInTheDocument();
  });

  it('показывает сохранённую подпись корешка', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok({ ...WORK, shelf_label: '1845—1846' }));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));

    renderAt('/works/5/edit');

    await waitFor(() => expect(screen.getByLabelText('Подпись корешка')).toHaveValue('1845—1846'));
  });

  it('шлёт подпись корешка вместе с координатами тома', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok({ ...WORK, shelf_label: 'старая' }));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));
    const update = vi.spyOn(worksApi, 'update').mockImplementation(() => ok(WORK));

    renderAt('/works/5/edit');

    const field = await screen.findByLabelText('Подпись корешка');
    await userEvent.clear(field);
    await userEvent.type(field, '1893—1894');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][1].shelf_label).toBe('1893—1894');
  });

  // Пустое поле — это «убрать подпись». Сервер читает "" как сброс, поэтому
  // отдельного null здесь быть не должно: иначе форма шлёт два разных способа
  // сказать одно.
  it('очищенная подпись уходит пустой строкой, а не null', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok({ ...WORK, shelf_label: 'старая' }));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));
    const update = vi.spyOn(worksApi, 'update').mockImplementation(() => ok(WORK));

    renderAt('/works/5/edit');

    await userEvent.clear(await screen.findByLabelText('Подпись корешка'));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][1].shelf_label).toBe('');
  });

  it('показывает сохранённое описание', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, description: 'Первая часть собрания.' }),
    );
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));

    renderAt('/works/5/edit');

    await waitFor(() =>
      expect(screen.getByLabelText('Описание')).toHaveValue('Первая часть собрания.'),
    );
  });

  it('шлёт описание вместе с остальными полями', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok({ ...WORK, description: 'старое' }));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));
    const update = vi.spyOn(worksApi, 'update').mockImplementation(() => ok(WORK));

    renderAt('/works/5/edit');

    const field = await screen.findByLabelText('Описание');
    await userEvent.clear(field);
    await userEvent.type(field, 'новое описание');
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][1].description).toBe('новое описание');
  });

  // Пустое поле — это «описания нет». Сервер читает "" как сброс, поэтому
  // отдельного null здесь быть не должно — иначе описание нельзя будет
  // стереть с карточки тома.
  it('очищенное описание уходит пустой строкой, а не null', async () => {
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok({ ...WORK, description: 'старое' }));
    vi.spyOn(editionsApi, 'list').mockImplementation(() => ok(EDITIONS));
    const update = vi.spyOn(worksApi, 'update').mockImplementation(() => ok(WORK));

    renderAt('/works/5/edit');

    await userEvent.clear(await screen.findByLabelText('Описание'));
    await userEvent.click(screen.getByRole('button', { name: 'Сохранить' }));

    await waitFor(() => expect(update).toHaveBeenCalled());
    expect(update.mock.calls[0][1].description).toBe('');
  });

  // Регрессия: загрузчик выходил на id === null, не сбросив isLoading, —
  // форма вечно висела на «Загрузка работы…» вместо уже существующего экрана
  // «Работа не найдена».
  it('на битом id в адресе показывает отказ, а не вечную загрузку, и ничего не грузит', async () => {
    const get = vi.spyOn(worksApi, 'get');
    const update = vi.spyOn(worksApi, 'update');

    renderAt('/works/xyz/edit');

    expect(await screen.findByText('Работа не найдена')).toBeInTheDocument();
    expect(screen.queryByText('Загрузка работы…')).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
    expect(update).not.toHaveBeenCalled();
  });
});
