import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { suggestionsApi, collectionsApi, documentsApi, authApi } from '../services/api';
import type { Collection, Document, SuggestionRow } from '../types';
// useAuth — это и есть zustand-стор (create<AuthState>()), поэтому у него
// есть setState: статус входа в тестах подменяется прямо на нём, без vi.mock.
import { useAuth } from '../hooks/useAuth';
import { MySuggestions } from './MySuggestions';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function row(over: Partial<SuggestionRow>): SuggestionRow {
  return {
    id: 1,
    page_id: 42,
    work_id: 7,
    work_title: 'Том 8',
    page_number: 3,
    page_offset: 0,
    note: '',
    status: 'новое',
    stale: false,
    length_delta: -3,
    created_at: '2026-08-27T10:00:00Z',
    ...over,
  };
}

function collection(over: Partial<Collection>): Collection {
  return {
    id: 1,
    title: 'Материализм',
    slug: 'materializm',
    description: '',
    created_at: '',
    updated_at: '',
    author_nickname: 'читатель',
    items: [],
    ...over,
  };
}

function document(over: Partial<Document>): Document {
  return {
    id: 1,
    slug: 'razbor',
    title: 'Разбор',
    markdown_content: '',
    owner_id: 1,
    author_nickname: 'читатель',
    published_title: '',
    published_markdown: '',
    published_at: null,
    was_published: false,
    review_status: 'черновик',
    created_at: '2026-09-19T00:00:00Z',
    updated_at: '2026-09-19T00:00:00Z',
    ...over,
  };
}

beforeEach(() => {
  useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  // Разделы подборок и разборов запрашиваются тем же экраном — по умолчанию
  // пусты, тесты, которым это важно, переопределяют возврат сами.
  vi.spyOn(collectionsApi, 'mine').mockReturnValue(ok([]) as never);
  vi.spyOn(documentsApi, 'mine').mockReturnValue(ok([]) as never);
});

describe('MySuggestions', () => {
  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('зовёт записаться, если читатель не вошёл', async () => {
    const spy = vi.spyOn(suggestionsApi, 'mine');
    const collectionsSpy = vi.spyOn(collectionsApi, 'mine');
    const documentsSpy = vi.spyOn(documentsApi, 'mine');
    render(
      <MemoryRouter initialEntries={['/mine']}>
        <MySuggestions />
      </MemoryRouter>,
    );

    const link = await screen.findByRole('link', { name: /записаться/i });
    expect(link).toHaveAttribute('href', '/join?next=%2Fmine');
    // Невошедшему читателю ни список правок, ни список подборок, ни список
    // разборов не запрашивается вовсе.
    expect(spy).not.toHaveBeenCalled();
    expect(collectionsSpy).not.toHaveBeenCalled();
    expect(documentsSpy).not.toHaveBeenCalled();
  });

  it('показывает свои правки вошедшему читателю', async () => {
    useAuth.setState({
      user: { id: 1, email: '', nickname: 'читатель', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    const spy = vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([row({})]) as never);
    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Том 8')).toBeInTheDocument();
    // Токен подставляет общий перехватчик — вызов без параметров.
    expect(spy).toHaveBeenCalledWith();
  });

  it('показывает причину отказа', async () => {
    useAuth.setState({
      user: { id: 1, email: '', nickname: 'читатель', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(
      ok([row({ status: 'отклонено', reject_reason: 'так_в_оригинале' })]) as never,
    );
    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );
    expect(await screen.findByText(/так в оригинале/i)).toBeInTheDocument();
  });

  it('показывает пустой список без правок', async () => {
    useAuth.setState({
      user: { id: 1, email: '', nickname: 'читатель', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);
    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );
    expect(await screen.findByText(/пока нет предложенных правок/i)).toBeInTheDocument();
  });

  it('заголовок экрана — «Моё»', async () => {
    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );
    expect(await screen.findByRole('heading', { name: 'Моё' })).toBeInTheDocument();
  });

  it('раздел «мои подборки» показывает состояние', async () => {
    useAuth.setState({
      user: { id: 1, email: '', nickname: 'читатель', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);
    vi.spyOn(collectionsApi, 'mine').mockReturnValue(
      ok([
        collection({ id: 1, slug: 'chernovik', title: 'Черновик подборки' }),
        collection({
          id: 2,
          slug: 'opublikovana',
          title: 'Опубликованная подборка',
          published_at: '2026-09-18T00:00:00Z',
        }),
      ]) as never,
    );

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('heading', { name: 'Мои подборки' })).toBeInTheDocument();
    const draftRow = (await screen.findByText('Черновик подборки')).closest('.my-suggestions-row');
    expect(draftRow?.textContent).toContain('черновик');
    const publishedRow = (await screen.findByText('Опубликованная подборка')).closest(
      '.my-suggestions-row',
    );
    expect(publishedRow?.textContent).toContain('опубликовано');
    expect(screen.getByRole('link', { name: 'Черновик подборки' })).toHaveAttribute(
      'href',
      '/collections/читатель/chernovik',
    );
  });

  it('показывает судьбу своих разборов', async () => {
    useAuth.setState({
      user: { id: 1, email: '', nickname: 'чтец', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);
    vi.spyOn(documentsApi, 'mine').mockReturnValue(
      ok([
        document({
          id: 1,
          slug: 'pro-chernyshevskogo',
          title: 'Про Чернышевского',
          review_status: 'на_рассмотрении',
          published_at: null,
          was_published: false,
        }),
        document({
          id: 2,
          slug: 'pro-lenina',
          title: 'Про Ленина',
          review_status: 'отклонено',
          reject_reason: 'не_по_теме',
          published_at: null,
          was_published: false,
        }),
      ]) as never,
    );

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    expect(await screen.findByText('Про Чернышевского')).toBeInTheDocument();
    expect(screen.getByText(/ждёт проверки/i)).toBeInTheDocument();
    expect(screen.getByText(/не по теме читальни/i)).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Про Чернышевского' })).toHaveAttribute(
      'href',
      '/documents/читатель/pro-chernyshevskogo',
    );
  });

  it('раздел «мои разборы» показывает пустой список без разборов', async () => {
    useAuth.setState({
      user: { id: 1, email: '', nickname: 'чтец', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);
    vi.spyOn(documentsApi, 'mine').mockReturnValue(ok([]) as never);

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    expect(await screen.findByRole('heading', { name: 'Мои разборы' })).toBeInTheDocument();
    expect(screen.getByText(/разборов пока нет/i)).toBeInTheDocument();
  });

  it('не даёт уйти, пока ник не набран верно, и зовёт deleteMe по клику', async () => {
    useAuth.setState({
      user: { id: 5, email: '', nickname: 'уходящий', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);
    const deleteSpy = vi.spyOn(authApi, 'deleteMe').mockReturnValue(ok(undefined) as never);

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    const button = await screen.findByRole('button', { name: /удалить учётную запись/i });
    expect(button).toBeDisabled();

    const input = screen.getByLabelText(/наберите свой ник/i);
    await userEvent.type(input, 'не тот ник');
    expect(button).toBeDisabled();

    await userEvent.clear(input);
    await userEvent.type(input, 'уходящий');
    expect(button).toBeEnabled();

    await userEvent.click(button);
    expect(deleteSpy).toHaveBeenCalled();
  });

  // I4 и задача 11 трекера: экран ухода обещал две неправды. Первая — «вход
  // под тем же ником заведёт новую учётную запись»: ник теперь отставляется
  // навсегда. Вторая — «останется под вашей подписью» про ВСЁ: неопубликованный
  // черновик разбора после ухода не виден никому и не удаляется никем.
  it('обещает уходящему ровно то, что случится: отставку ника и судьбу черновика', async () => {
    useAuth.setState({
      user: { id: 5, email: '', nickname: 'уходящий', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    await screen.findByRole('button', { name: /удалить учётную запись/i });
    // Ник не освобождается.
    expect(screen.getByText(/ник останется занятым навсегда/i)).toBeInTheDocument();
    expect(screen.queryByText(/заведёт новую, чужую этой, учётную запись/i)).toBeNull();
    // Неопубликованный черновик назван отдельно и честно.
    expect(screen.getByText(/неопубликованный черновик разбора/i)).toBeInTheDocument();
    expect(screen.getByText(/удалите его до\s+ухода/i)).toBeInTheDocument();
  });

  it('читателю без ника (случай, которого схема не допускает) пустое поле кнопку не включает', async () => {
    useAuth.setState({
      // Искусственный случай: тип User.nickname необязателен, схема сегодня
      // такого читателя не допускает — тест сторожит именно эту нестыковку
      // типа со схемой, а не сегодняшнее поведение.
      user: { id: 6, email: '', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    const button = await screen.findByRole('button', { name: /удалить учётную запись/i });
    // Поле не тронуто — confirmNick тоже пустая строка. Пустое не должно
    // сойтись с отсутствующим `?? ''`-запасом.
    expect(button).toBeDisabled();
  });

  it('сотруднику дверь ухода не показывает', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@example.org', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(suggestionsApi, 'mine').mockReturnValue(ok([]) as never);

    render(
      <MemoryRouter>
        <MySuggestions />
      </MemoryRouter>,
    );

    await screen.findByRole('heading', { name: 'Моё' });
    expect(screen.queryByText(/уйти из читальни/i)).not.toBeInTheDocument();
  });
});
