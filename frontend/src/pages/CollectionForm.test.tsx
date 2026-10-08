import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { collectionsApi, worksApi, shelfApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Collection, Edition, Shelf, VolumeSummary } from '../types';
import { CollectionForm } from './CollectionForm';

vi.mock('../services/api', () => ({
  collectionsApi: {
    get: vi.fn(),
    create: vi.fn(),
    update: vi.fn(),
    addItem: vi.fn(),
    updateItem: vi.fn(),
    removeItem: vi.fn(),
    moveItem: vi.fn(),
    publish: vi.fn(),
    unpublish: vi.fn(),
  },
  worksApi: { list: vi.fn() },
  chaptersApi: { list: vi.fn() },
  shelfApi: { get: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

// `GET /works` без limit отдаёт 50 самых свежих томов — том 14 Плеханова,
// залитый раньше, в них не попадал. Каталог здесь намеренно без него.
const FIRST_FIFTY_WORKS = Array.from({ length: 50 }, (_, i) => ({
  id: 1000 + i,
  title: `В. И. Ленин. Полное собрание сочинений. Том ${i + 1}`,
}));

const SHELF: Shelf = {
  editions: [
    {
      edition: { id: 3, title: 'Г. В. Плеханов. Сочинения' } as Edition,
      volumes: [
        { id: 213, title: 'Г. В. Плеханов. Сочинения. Том 13', author: '' },
        { id: 214, title: 'Г. В. Плеханов. Сочинения. Том 14', author: '' },
      ] as VolumeSummary[],
    },
  ],
  loose_works: [{ id: 900, title: 'Отдельная работа' }],
};

const COLLECTION: Collection = {
  id: 1,
  title: 'Материализм',
  slug: 'materializm',
  description: '',
  created_at: '',
  updated_at: '',
  author_nickname: '',
  items: [
    {
      id: 1,
      kind: 'chapter',
      order_number: 1,
      title: 'Первая',
      work_author: 'К. Маркс',
      author_override: '',
      author: 'К. Маркс',
      broken: false,
    },
    {
      id: 2,
      kind: 'chapter',
      order_number: 2,
      title: 'Вторая',
      work_author: 'Ф. Энгельс',
      author_override: '',
      author: 'Ф. Энгельс',
      broken: false,
    },
  ],
};

function renderForm() {
  return render(
    <MemoryRouter initialEntries={['/collections/materializm/edit']}>
      <Routes>
        <Route path="/collections/:slug/edit" element={<CollectionForm />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('CollectionForm', () => {
  beforeEach(() => {
    vi.mocked(collectionsApi.get).mockReset().mockReturnValue(ok(COLLECTION));
    vi.mocked(collectionsApi.moveItem).mockReset().mockReturnValue(ok(undefined));
    vi.mocked(collectionsApi.updateItem).mockReset().mockReturnValue(ok(undefined));
    vi.mocked(collectionsApi.publish)
      .mockReset()
      .mockReturnValue(ok({ ...COLLECTION, published_at: '2026-09-18T00:00:00Z' }));
    vi.mocked(collectionsApi.unpublish)
      .mockReset()
      .mockReturnValue(ok({ ...COLLECTION, published_at: undefined }));
    vi.mocked(worksApi.list).mockReturnValue(ok(FIRST_FIFTY_WORKS as never));
    vi.mocked(shelfApi.get).mockReturnValue(ok(SHELF));
    vi.mocked(collectionsApi.addItem)
      .mockReset()
      .mockReturnValue(ok(undefined as never));
    // Форма правки состава требует входа — большинство сценариев здесь
    // проверяют поведение владельца-редактора витринной подборки
    // (author_nickname пуст), поэтому nickname у собственных вызовов
    // остаётся undefined.
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
  });

  it('поднимает элемент на строку вверх', async () => {
    renderForm();
    await screen.findByText('Вторая');

    // Стрелка второй строки: у первой она отключена.
    await userEvent.click(screen.getAllByRole('button', { name: /вверх/i })[1]);

    await waitFor(() => {
      expect(collectionsApi.moveItem).toHaveBeenCalledWith('materializm', 2, 1, undefined);
    });
  });

  it('не даёт поднять первый элемент', async () => {
    renderForm();
    await screen.findByText('Первая');

    expect(screen.getAllByRole('button', { name: /вверх/i })[0]).toHaveProperty('disabled', true);
  });

  it('сохраняет переопределение автора', async () => {
    renderForm();
    await screen.findByText('Первая');

    const input = screen.getAllByLabelText(/автор/i)[0];
    await userEvent.clear(input);
    await userEvent.type(input, 'Ф. Энгельс');
    await userEvent.tab();

    await waitFor(() => {
      expect(collectionsApi.updateItem).toHaveBeenCalledWith(
        'materializm',
        1,
        { author_override: 'Ф. Энгельс' },
        undefined,
      );
    });
  });

  it('показывает кнопку «Опубликовать» у черновика владельца', async () => {
    renderForm();

    expect(await screen.findByRole('button', { name: 'Опубликовать' })).toBeTruthy();
  });

  it('говорит, что подборка не попадёт в общий список', async () => {
    renderForm();

    expect(
      await screen.findByText(/в общий список подборок читальни она не попадает/i),
    ).toBeTruthy();
  });

  it('у опубликованной подборки показывает «Снять с публикации»', async () => {
    vi.mocked(collectionsApi.get)
      .mockReset()
      .mockReturnValue(ok({ ...COLLECTION, published_at: '2026-09-18T00:00:00Z' }));

    renderForm();

    expect(await screen.findByRole('button', { name: 'Снять с публикации' })).toBeTruthy();
    expect(screen.queryByRole('button', { name: 'Опубликовать' })).toBeNull();
  });

  it('нажатие «Опубликовать» зовёт collectionsApi.publish', async () => {
    renderForm();
    await userEvent.click(await screen.findByRole('button', { name: 'Опубликовать' }));

    await waitFor(() => {
      expect(collectionsApi.publish).toHaveBeenCalledWith('materializm', undefined);
    });
  });

  it('находит том вне первых пятидесяти каталога и добавляет его целиком', async () => {
    renderForm();
    await screen.findByText('Первая');

    await userEvent.type(screen.getByRole('combobox', { name: 'Том' }), 'плех 14');
    await userEvent.click(
      await screen.findByRole('option', { name: 'Г. В. Плеханов. Сочинения. Том 14' }),
    );
    await userEvent.click(screen.getByRole('button', { name: 'Добавить том целиком' }));

    await waitFor(() => {
      expect(collectionsApi.addItem).toHaveBeenCalledWith(
        'materializm',
        { kind: 'work', work_id: 214 },
        undefined,
      );
    });
  });

  it('тома идут собраниями в порядке полки, отдельные работы — в конце', async () => {
    renderForm();
    await screen.findByText('Первая');

    await userEvent.click(screen.getByRole('combobox', { name: 'Том' }));

    expect((await screen.findAllByRole('option')).map((o) => o.textContent)).toEqual([
      'Г. В. Плеханов. Сочинения. Том 13',
      'Г. В. Плеханов. Сочинения. Том 14',
      'Отдельная работа',
    ]);
    expect(screen.getByText('Отдельные работы')).toBeTruthy();
  });

  it('гостю форма подборки предлагает записаться, а не редактировать', async () => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });

    renderForm();

    expect(
      await screen.findByText(/собирать и публиковать свою подборку может только читатель/i),
    ).toBeTruthy();
    expect(screen.queryByLabelText(/название/i)).toBeNull();
  });
});
