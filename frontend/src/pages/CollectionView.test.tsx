import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { collectionsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Collection } from '../types';
import { CollectionView } from './CollectionView';
import { shareFrom, stubShare, unstubShare } from '../test/shareStub';

vi.mock('../services/api', () => ({
  collectionsApi: { get: vi.fn(), remove: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

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
      title: 'Людвиг Фейербах',
      work_author: 'К. Маркс, Ф. Энгельс',
      author_override: 'Ф. Энгельс',
      author: 'Ф. Энгельс',
      broken: false,
      source: {
        work_id: 41,
        work_title: 'Том 21',
        edition_title: 'Сочинения, 2-е изд.',
        volume_number: 21,
        page_start: 269,
        page_end: 317,
      },
      children: [
        { chapter_id: 101, title: 'I', page_start: 269 },
        {
          chapter_id: 102,
          title: 'II. Идеализм и материализм',
          page_start: 276,
          children: [{ chapter_id: 103, title: '1. Гегель', page_start: 280 }],
        },
      ],
    },
    {
      id: 2,
      kind: 'chapter',
      order_number: 2,
      title: 'Пропавшая глава',
      work_author: '',
      author_override: '',
      author: '',
      broken: true,
    },
  ],
};

function renderView() {
  return render(
    <MemoryRouter initialEntries={['/collections/materializm']}>
      <Routes>
        <Route path="/collections/:slug" element={<CollectionView />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('CollectionView', () => {
  beforeEach(() => {
    vi.mocked(collectionsApi.get).mockReset().mockReturnValue(ok(COLLECTION));
    vi.mocked(collectionsApi.remove)
      .mockReset()
      .mockResolvedValue({} as AxiosResponse<void>);
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  // Раздел зовётся «Подборки» и в шапке, и в подвале — возврат из карточки
  // обязан вести туда же теми же словами.
  it('возвращает «к подборкам»', async () => {
    renderView();

    expect(await screen.findByRole('link', { name: '← К подборкам' })).toHaveAttribute(
      'href',
      '/collections',
    );
  });

  it('показывает строку оглавления с автором и печатными страницами', async () => {
    renderView();

    expect(await screen.findByText('Людвиг Фейербах')).toBeTruthy();
    expect(screen.getByText(/Ф\. Энгельс/)).toBeTruthy();
    expect(screen.getByText(/269[—-]317/)).toBeTruthy();
  });

  it('раскрывает поддерево подглав', async () => {
    renderView();

    expect(await screen.findByText('II. Идеализм и материализм')).toBeTruthy();
  });

  it('вложенная глава — ссылка в главу тома-источника', async () => {
    renderView();

    expect(
      await screen.findByRole('link', { name: /II\. Идеализм и материализм/ }),
    ).toHaveAttribute('href', '/works/41/chapters/102');
  });

  it('глава второго уровня ведёт по своему адресу, а не по родительскому', async () => {
    renderView();

    expect(await screen.findByRole('link', { name: /1\. Гегель/ })).toHaveAttribute(
      'href',
      '/works/41/chapters/103',
    );
  });

  it('помечает битую строку и не ведёт её в чтение', async () => {
    const { container } = renderView();

    const broken = await screen.findByText('Пропавшая глава');
    expect(broken.closest('.collection-entry')?.className).toContain('is-broken');
    expect(container.querySelector('a[href="/collections/materializm/read/2"]')).toBeNull();
  });

  it('редактору видна ссылка «Изменить» на правку состава', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });

    renderView();

    expect(await screen.findByRole('link', { name: 'Изменить' })).toHaveAttribute(
      'href',
      '/collections/materializm/edit',
    );
  });

  it('гостю ссылка «Изменить» не показывается', async () => {
    renderView();

    await screen.findByText('Людвиг Фейербах');
    expect(screen.queryByRole('link', { name: 'Изменить' })).toBeNull();
  });

  it('редактору видна кнопка «Удалить»', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });

    renderView();

    expect(await screen.findByRole('button', { name: 'Удалить' })).toBeTruthy();
  });

  it('гостю кнопка «Удалить» не показывается', async () => {
    renderView();

    await screen.findByText('Людвиг Фейербах');
    expect(screen.queryByRole('button', { name: 'Удалить' })).toBeNull();
  });

  it('удаление спрашивает подтверждение и зовёт collectionsApi.remove со слагом', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(true);
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    const user = userEvent.setup();

    renderView();
    await user.click(await screen.findByRole('button', { name: 'Удалить' }));

    expect(confirmSpy).toHaveBeenCalled();
    expect(collectionsApi.remove).toHaveBeenCalledWith('materializm', undefined);
    confirmSpy.mockRestore();
  });

  it('не показывает правки чужой подборки', async () => {
    // Подборка принадлежит читателю vasya; смотрит на неё вошедший читатель
    // с другим ником — тот же приём различения, что и canEditCollection на
    // сервере (mayEdit), только здесь по нику, а не по владельцу.
    vi.mocked(collectionsApi.get)
      .mockReset()
      .mockReturnValue(ok({ ...COLLECTION, author_nickname: 'vasya' }));
    useAuth.setState({
      user: { id: 2, email: 'petya@b.c', role: 'reader', nickname: 'petya' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });

    renderView();

    await screen.findByText('Людвиг Фейербах');
    expect(screen.queryByRole('link', { name: 'Изменить' })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Удалить' })).toBeNull();
  });

  it('отказ от подтверждения не зовёт collectionsApi.remove', async () => {
    const confirmSpy = vi.spyOn(window, 'confirm').mockReturnValue(false);
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    const user = userEvent.setup();

    renderView();
    await user.click(await screen.findByRole('button', { name: 'Удалить' }));

    expect(collectionsApi.remove).not.toHaveBeenCalled();
    confirmSpy.mockRestore();
  });
});

describe('CollectionView: Поделиться', () => {
  afterEach(unstubShare);

  it('отдаёт опубликованную читательскую подборку по длинному адресу', async () => {
    vi.mocked(collectionsApi.get)
      .mockReset()
      .mockReturnValue(
        ok({ ...COLLECTION, author_nickname: 'ivan', published_at: '2026-10-01T00:00:00Z' }),
      );
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    const share = stubShare();
    renderView();
    await screen.findByRole('heading', { level: 1, name: 'Материализм' });
    const data = await shareFrom(share);
    expect(data.title).toBe('Материализм — подборка');
    expect(data.url).toBe(`${window.location.origin}/collections/ivan/materializm`);
  });

  // Ссылка на черновик открылась бы получателю как 404 — кнопка, обещающая
  // отправить то, что не откроется, хуже отсутствующей.
  it('не предлагает поделиться черновиком', async () => {
    vi.mocked(collectionsApi.get)
      .mockReset()
      .mockReturnValue(ok({ ...COLLECTION, published_at: undefined }));
    renderView();
    await screen.findByRole('heading', { level: 1, name: 'Материализм' });
    expect(screen.queryByRole('button', { name: 'Поделиться' })).toBeNull();
  });
});
