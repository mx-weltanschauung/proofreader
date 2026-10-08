import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { documentsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Document } from '../types';
import { DocumentList } from './DocumentList';

vi.mock('../services/api', () => ({
  documentsApi: { list: vi.fn() },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function document(over: Partial<Document> = {}): Document {
  return {
    id: 1,
    slug: 'o-gosudarstve-i-revolyutsii',
    title: 'О государстве и революции',
    markdown_content: '',
    owner_id: 1,
    author_nickname: 'читатель',
    published_title: '',
    published_markdown: '',
    published_at: '2026-09-19T00:00:00Z',
    was_published: true,
    review_status: 'одобрено',
    created_at: '2026-09-01T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    ...over,
  };
}

function renderList() {
  return render(
    <MemoryRouter initialEntries={['/documents']}>
      <DocumentList />
    </MemoryRouter>,
  );
}

describe('DocumentList', () => {
  beforeEach(() => {
    vi.mocked(documentsApi.list)
      .mockReset()
      .mockReturnValue(ok([] as Document[]));
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  // Читальня говорит от лица библиотеки: «документ» — слово из API, не из
  // витрины.
  it('озаглавлена «Разборы»', async () => {
    renderList();
    expect(await screen.findByRole('heading', { name: 'Разборы' })).toBeInTheDocument();
  });

  it('на пустом каталоге говорит о разборах, а не о документах', async () => {
    renderList();
    expect(await screen.findByText('Разборов пока нет.')).toBeInTheDocument();
  });

  it('печатает подпись читателя и дату публикации, а не создания', async () => {
    // documentsApi.list отдаёт разбор с published_at на день позже created_at;
    // на карточке обязана стоять дата ПУБЛИКАЦИИ: витрина показывает, когда
    // разбор вышел к читателю, а не когда автор завёл черновик.
    vi.mocked(documentsApi.list).mockReturnValue(
      ok([
        document({
          author_nickname: 'чтец',
          created_at: '2026-09-01T00:00:00Z',
          published_at: '2026-09-19T00:00:00Z',
        }),
      ]),
    );

    renderList();

    expect(await screen.findByText('Собрал читатель чтец')).toBeInTheDocument();
    const publishedDate = new Date('2026-09-19T00:00:00Z').toLocaleDateString();
    const createdDate = new Date('2026-09-01T00:00:00Z').toLocaleDateString();
    expect(await screen.findByText(new RegExp(publishedDate))).toBeInTheDocument();
    expect(screen.queryByText(new RegExp(createdDate))).not.toBeInTheDocument();
  });

  it('показывает разборы ссылками, построенными documentPath', async () => {
    vi.mocked(documentsApi.list).mockReturnValue(
      ok([document({ author_nickname: 'чтец', slug: 'razbor' })]),
    );

    renderList();

    expect(await screen.findByRole('link', { name: /О государстве и революции/ })).toHaveAttribute(
      'href',
      '/documents/чтец/razbor',
    );
  });

  it('вошедшему предлагает собрать разбор', async () => {
    useAuth.setState({
      user: { id: 2, email: '', nickname: 'чтец', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderList();
    expect(await screen.findByRole('link', { name: 'Собрать разбор' })).toHaveAttribute(
      'href',
      '/documents/new',
    );
  });
});
