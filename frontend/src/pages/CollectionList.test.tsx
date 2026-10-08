import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { collectionsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Collection } from '../types';
import { CollectionList } from './CollectionList';
import { SITE_NAME } from '../hooks/useDocumentTitle';

vi.mock('../services/api', () => ({
  collectionsApi: { list: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const COLLECTIONS: Collection[] = [
  {
    id: 1,
    title: 'О государстве',
    slug: 'o-gosudarstve',
    description: 'Работы о природе государства.',
    created_at: '',
    updated_at: '',
    author_nickname: '',
  },
];

function renderList() {
  return render(
    <MemoryRouter initialEntries={['/collections']}>
      <CollectionList />
    </MemoryRouter>,
  );
}

describe('CollectionList', () => {
  beforeEach(() => {
    vi.mocked(collectionsApi.list)
      .mockReset()
      .mockReturnValue(ok([] as Collection[]));
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  // Ссылка из шапки и подвала обещает «Подборки» — страница обязана
  // называться так же, а не «Подборки».
  it('озаглавлен «Подборки»', async () => {
    renderList();
    expect(await screen.findByRole('heading', { name: 'Подборки' })).toBeInTheDocument();
  });

  it('на пустом каталоге говорит о подборках', async () => {
    renderList();
    expect(await screen.findByText('Подборок пока нет.')).toBeInTheDocument();
  });

  it('редактору предлагает создать подборку', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@proofreader.local', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderList();
    expect(await screen.findByRole('link', { name: 'Создать подборку' })).toHaveAttribute(
      'href',
      '/collections/new',
    );
  });

  it('показывает подборки ссылками на их страницы', async () => {
    vi.mocked(collectionsApi.list).mockReturnValue(ok(COLLECTIONS));

    renderList();

    expect(await screen.findByRole('link', { name: /О государстве/ })).toHaveAttribute(
      'href',
      '/collections/o-gosudarstve',
    );
  });

  // F4 итогового ревью: заголовок вкладки не был проложен вовсе — читатель
  // видел «Читальня» на индексируемой странице, тогда как сервер
  // (internal/seo/render_index.go, CollectionList) отдаёт краулеру
  // «Подборки — Читальня».
  it('ставит заголовок вкладки, совпадающий с серверным', async () => {
    document.title = SITE_NAME;
    vi.mocked(collectionsApi.list).mockReturnValue(ok(COLLECTIONS));

    renderList();

    await screen.findByRole('link', { name: /О государстве/ });
    // Заголовок ставится эффектом, а проверка идёт сразу за ожиданием
    // разметки — под нагрузкой полного прогона эффект успевает не всегда.
    // Синхронная проверка здесь давала нестабильный провал примерно раз
    // на два прогона: ждём сам заголовок, а не разметку рядом с ним.
    await waitFor(() => {
      expect(document.title).toBe(`Подборки — ${SITE_NAME}`);
    });
  });
});
