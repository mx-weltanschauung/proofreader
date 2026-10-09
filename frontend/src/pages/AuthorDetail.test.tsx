import { fireEvent, render, screen, waitFor } from '@testing-library/react';
import { MemoryRouter, Route, Routes } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { afterEach, describe, expect, it, vi } from 'vitest';
import { personsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { AuthorDetail } from './AuthorDetail';
import type { PersonDetail } from '../types';

function ok<T>(data: T) {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const DETAIL: PersonDetail = {
  person: { id: 4, name: 'Гр. Баммель', sort_key: 'баммель гр', slug: 'gr-bammel' },
  articles: [
    {
      chapter_id: 50,
      chapter_slug: 'pamyati-iosifa-didgena',
      title: 'Памяти Иосифа Дидгена',
      article_kind: 'статья',
      role: 'author',
      work_id: 300,
      work_slug: 'pzm-1928-12',
      journal_slug: 'pzm',
      journal_title: 'Под знаменем марксизма',
      year: 1928,
      label: '12',
      start_page: 5,
      end_page: 25,
    },
  ],
};

function asEditor() {
  useAuth.setState({
    user: { id: 1, role: 'editor' } as never,
    token: 't',
    isAuthenticated: true,
    isLoading: false,
  });
}

function renderAt() {
  return render(
    <MemoryRouter initialEntries={['/authors/gr-bammel']}>
      <Routes>
        <Route path="/authors/:slug" element={<AuthorDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('AuthorDetail', () => {
  afterEach(() => vi.restoreAllMocks());

  it('статьи по журналам и годам со ссылкой на главу', async () => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    vi.spyOn(personsApi, 'get').mockImplementation(() => ok(DETAIL));
    renderAt();
    expect(
      await screen.findByRole('heading', { level: 1, name: 'Гр. Баммель' }),
    ).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Памяти Иосифа Дидгена' })).toHaveAttribute(
      'href',
      '/works/300-pzm-1928-12/chapters/50-pamyati-iosifa-didgena',
    );
    expect(screen.getByText(/1928, № 12, с\. 5—25/)).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /Слить/ })).toBeNull();
    expect(screen.queryByRole('button', { name: 'Сохранить имя' })).toBeNull();
  });

  it('статья на одной полосе — «с. 5», без «5—5»', async () => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    vi.spyOn(personsApi, 'get').mockImplementation(() =>
      ok({ ...DETAIL, articles: [{ ...DETAIL.articles[0], end_page: 5 }] }),
    );
    renderAt();
    expect(await screen.findByText(/1928, № 12, с\. 5(?!\d|—)/)).toBeInTheDocument();
    expect(screen.queryByText(/с\. 5—5/)).toBeNull();
  });

  it('редактор сливает найденного автора сюда, себя в выдаче не видит', async () => {
    asEditor();
    vi.spyOn(personsApi, 'get').mockImplementation(() => ok(DETAIL));
    vi.spyOn(personsApi, 'search').mockImplementation(() =>
      ok([
        { id: 4, name: 'Гр. Баммель', sort_key: 'баммель гр', slug: 'gr-bammel' },
        { id: 9, name: 'Г. Баммель', sort_key: 'баммель г', slug: 'g-bammel' },
      ]),
    );
    vi.spyOn(window, 'confirm').mockReturnValue(true);
    const merge = vi.spyOn(personsApi, 'merge').mockImplementation(() => ok(DETAIL.person));
    renderAt();
    fireEvent.change(await screen.findByLabelText('Найти автора для слияния'), {
      target: { value: 'Бам' },
    });
    fireEvent.click(await screen.findByRole('button', { name: 'Слить «Г. Баммель» сюда' }));
    expect(screen.queryByRole('button', { name: 'Слить «Гр. Баммель» сюда' })).toBeNull();
    await waitFor(() => expect(merge).toHaveBeenCalledWith(4, 9));
  });

  it('редактор переименовывает автора', async () => {
    asEditor();
    const get = vi.spyOn(personsApi, 'get').mockImplementation(() => ok(DETAIL));
    const update = vi
      .spyOn(personsApi, 'update')
      .mockImplementation(() => ok({ ...DETAIL.person, name: 'Г. Баммель' }));
    renderAt();
    fireEvent.change(await screen.findByLabelText('Имя автора'), {
      target: { value: '  Г. Баммель ' },
    });
    fireEvent.click(screen.getByRole('button', { name: 'Сохранить имя' }));
    await waitFor(() => expect(update).toHaveBeenCalledWith(4, { name: 'Г. Баммель' }));
    await waitFor(() => expect(get).toHaveBeenCalledTimes(2));
  });
});
