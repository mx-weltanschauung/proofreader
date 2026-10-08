import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { documentsApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { DocumentView } from './DocumentView';
import { shareFrom, stubShare, unstubShare } from '../test/shareStub';

vi.mock('../services/api', () => ({
  documentsApi: {
    view: vi.fn(),
    cut: vi.fn(),
  },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const DEFAULT_HTML =
  '<div class="document-cut" data-cut-id="1">' +
  '<p class="document-cut-source">Ленин. Что делать?</p>' +
  '<div class="document-cut-body"><div class="document-cut-page">' +
  '<a class="document-cut-folio" href="/works/1/pages/4">236</a>' +
  '<p>текст корпуса</p></div></div></div>' +
  '<p class="document-cut document-cut--broken" data-cut-id="2">Ленин. Снятая работа</p>' +
  '<p class="document-cut document-cut--stale" data-cut-id="3">Ленин. Уехавшая ' +
  '<a href="/works/1/pages/7">Читать в томе</a></p>';

interface DocumentViewFixtureOpts {
  html_content?: string;
  cut?: ReturnType<typeof vi.fn>;
  author_nickname?: string;
  owner_id?: number | null;
  review_status?: string;
  was_published?: boolean;
  has_unpublished_changes?: boolean;
  published_at?: string | null;
}

function renderViewWith(opts: DocumentViewFixtureOpts = {}) {
  vi.mocked(documentsApi.view)
    .mockReset()
    .mockReturnValue(
      ok({
        id: 5,
        slug: 'razbor',
        title: 'Разбор',
        owner_id: opts.owner_id ?? null,
        author_nickname: opts.author_nickname ?? '',
        published_at: opts.published_at === undefined ? '2026-09-19T00:00:00Z' : opts.published_at,
        created_at: '2026-09-19T00:00:00Z',
        updated_at: '2026-09-19T00:00:00Z',
        html_content: opts.html_content ?? DEFAULT_HTML,
        review_status: opts.review_status ?? '',
        was_published: opts.was_published ?? false,
        has_unpublished_changes: opts.has_unpublished_changes ?? false,
      }),
    );
  vi.mocked(documentsApi.cut).mockReset();
  if (opts.cut) {
    vi.mocked(documentsApi.cut).mockImplementation(opts.cut);
  }
  return render(
    <MemoryRouter initialEntries={['/documents/razbor']}>
      <Routes>
        <Route path="/documents/:slug" element={<DocumentView />} />
      </Routes>
    </MemoryRouter>,
  );
}

function renderView() {
  return renderViewWith();
}

// Показывает текущий путь как текст — прямая проверка адреса, на который
// увела кнопка «Править», а не догадка по тому, что отрисовал маршрут.
function LocationDisplay() {
  const location = useLocation();
  return <div data-testid="location-display">{location.pathname}</div>;
}

// Финальная находка ревью ветки 3: GET /api/documents/{…}/view не нёс slug,
// и documentEditPath(document), читающий document.slug, строил адрес
// .../undefined/edit — путь, который реально существует в SPA (совпадает с
// маршрутом /documents/:slug/edit) и потому не падал явно, а тихо вёл на
// несуществующий разбор. Тест держит и короткий адрес (сотрудник), и длинный
// (читатель) — sever теперь кладёт slug в ответ View, а этот тест проверяет,
// что клиент этим slug действительно пользуется, а не просто получает его.
function renderViewForNavigation(opts: DocumentViewFixtureOpts, initialPath: string) {
  vi.mocked(documentsApi.view)
    .mockReset()
    .mockReturnValue(
      ok({
        id: 5,
        slug: 'razbor',
        title: 'Разбор',
        owner_id: opts.owner_id ?? null,
        author_nickname: opts.author_nickname ?? '',
        published_at: opts.published_at === undefined ? '2026-09-19T00:00:00Z' : opts.published_at,
        created_at: '2026-09-19T00:00:00Z',
        updated_at: '2026-09-19T00:00:00Z',
        html_content: opts.html_content ?? DEFAULT_HTML,
        review_status: opts.review_status ?? '',
        was_published: opts.was_published ?? false,
        has_unpublished_changes: opts.has_unpublished_changes ?? false,
      }),
    );
  vi.mocked(documentsApi.cut).mockReset();
  return render(
    <MemoryRouter initialEntries={[initialPath]}>
      <LocationDisplay />
      <Routes>
        <Route path="/documents/:slug" element={<DocumentView />} />
        <Route path="/documents/:nickname/:slug" element={<DocumentView />} />
        <Route path="/documents/:slug/edit" element={<div>форма правки</div>} />
        <Route path="/documents/:nickname/:slug/edit" element={<div>форма правки</div>} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('DocumentView — вклейки', () => {
  it('колонцифра вклейки стоит в поле и ведёт по номеру полосы', async () => {
    const { container } = renderView();
    await waitFor(() => expect(container.querySelector('.document-cut-folio')).not.toBeNull());
    const folio = container.querySelector('.document-cut-folio') as HTMLAnchorElement;
    // Подпись — печатная колонцифра, адрес — номер полосы: это разные числа.
    expect(folio.textContent).toBe('236');
    expect(folio.getAttribute('href')).toBe('/works/1/pages/4');
  });

  it('битой вклейке идти некуда, отвязавшейся есть', async () => {
    const { container } = renderView();
    await waitFor(() => expect(container.querySelector('.document-cut--broken')).not.toBeNull());
    expect(container.querySelector('.document-cut--broken')!.querySelector('a')).toBeNull();
    expect(container.querySelector('.document-cut--stale')!.querySelector('a')).not.toBeNull();
  });

  // Разметка ниже — дословно то, что печатает renderCutTrimmed
  // (internal/api/document_render.go): кнопка живёт внутри
  // <p class="document-cut-actions">, а не прямо в .document-cut. Обработчик
  // отказа ищет узел для сообщения через
  // button.closest('.document-cut-actions')?.after(notice) — на фикстуре,
  // кладущей кнопку мимо этого <p>, closest() вернул бы null, и сообщение
  // нигде не появилось бы, а тест этого не заметил.
  const TRIMMED_CUT_HTML =
    '<div class="document-cut document-cut--trimmed" data-cut-id="1">' +
    '<div class="document-cut-body"><p>начало вклейки</p></div>' +
    '<p class="document-cut-actions">' +
    '<a href="/works/1/pages/4">Читать в томе</a> ' +
    '<button type="button" class="document-cut-expand" data-cut-id="1">Развернуть здесь</button>' +
    '</p></div>';

  it('«Развернуть здесь» догружает остаток и второй раз не спрашивает', async () => {
    const cut = vi.fn().mockResolvedValue({
      data: { html: '<div class="document-cut" data-cut-id="1">полный текст вклейки</div>' },
    });
    renderViewWith({ cut, html_content: TRIMMED_CUT_HTML });

    await userEvent.click(await screen.findByRole('button', { name: 'Развернуть здесь' }));

    await waitFor(() => expect(screen.getByText('полный текст вклейки')).toBeTruthy());
    expect(cut).toHaveBeenCalledWith({ nickname: undefined, slug: 'razbor' }, 1);
    // Кнопки больше нет — повторного запроса быть не может по построению.
    expect(screen.queryByRole('button', { name: 'Развернуть здесь' })).toBeNull();
    expect(cut).toHaveBeenCalledTimes(1);
  });

  // Отложенная находка задачи 10+12: прежняя фикстура клала кнопку мимо
  // .document-cut-actions, и путь отказа — единственный, который трогает
  // этот узел, — не проверялся ни разу. Текст сообщения обязан прийти из
  // {"message": …} тела ответа (apiErrorMessage), а не из запасной фразы.
  it('«Развернуть здесь» отвечает отказом сервера и возвращает кнопку', async () => {
    const cut = vi.fn().mockRejectedValue({
      response: { status: 500, data: { message: 'вклейка снята вместе с томом' } },
    });
    renderViewWith({ cut, html_content: TRIMMED_CUT_HTML });

    const button = await screen.findByRole('button', { name: 'Развернуть здесь' });
    await userEvent.click(button);

    await waitFor(() => expect(screen.getByText('вклейка снята вместе с томом')).toBeTruthy());
    // Кнопка возвращается в рабочее состояние — отказ не должен запирать
    // читателя от повторной попытки.
    expect(screen.getByRole('button', { name: 'Развернуть здесь' })).not.toBeDisabled();
    expect(cut).toHaveBeenCalledTimes(1);
  });
});

describe('DocumentView — пометка происхождения', () => {
  it('несёт несъёмную пометку происхождения', async () => {
    renderViewWith({ author_nickname: 'чтец' });

    expect(await screen.findByText(/собрал читатель/i)).toBeInTheDocument();
    expect(screen.getByText(/чтец/)).toBeInTheDocument();
  });

  it('у редакционного разбора пометки нет', async () => {
    renderViewWith({ author_nickname: '' });

    await screen.findByText('Разбор');
    expect(screen.queryByText(/собрал читатель/i)).not.toBeInTheDocument();
  });
});

// M7. На странице просмотра состояния не было вовсе: автор видел свой
// черновик и ничем не отличал его от публичного вида — ровно то различие,
// ради которого затеяна вся ветка.
describe('DocumentView — полоса состояния', () => {
  // Сброс в beforeEach, а не в afterEach: наш afterEach отрабатывает РАНЬШЕ
  // авто-очистки RTL, то есть по ещё смонтированному дереву, и setState по
  // нему даёт предупреждение «update not wrapped in act». Предупреждение —
  // ровно тот дребезг, за который дальше цепляется «перезапусти, это
  // случайность».
  beforeEach(() => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  function signInAsReader(id: number, nickname: string) {
    useAuth.setState({
      user: { id, email: '', role: 'reader', nickname },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
  }

  it('автору показывает шестое состояние: правка сохранена, но не отправлена', async () => {
    signInAsReader(5, 'чтец');
    renderViewWith({
      author_nickname: 'чтец',
      owner_id: 5,
      review_status: 'одобрено',
      was_published: true,
      has_unpublished_changes: true,
    });

    expect(await screen.findByText(/не отправлена/i)).toBeInTheDocument();
    expect(screen.getByText(/прежняя редакция/i)).toBeInTheDocument();
  });

  it('автору опубликованного без правок говорит «на людях»', async () => {
    signInAsReader(5, 'чтец');
    renderViewWith({
      author_nickname: 'чтец',
      owner_id: 5,
      review_status: 'одобрено',
      was_published: true,
    });

    expect(await screen.findByText('Разбор на людях.')).toBeInTheDocument();
  });

  it('постороннему полосы состояния нет вовсе', async () => {
    signInAsReader(6, 'посторонний');
    renderViewWith({
      author_nickname: 'чтец',
      owner_id: 5,
      review_status: '',
      was_published: true,
    });

    await screen.findByText(/собрал читатель/i);
    expect(screen.queryByText('Разбор на людях.')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Править' })).toBeNull();
  });
});

// Финальная находка ревью ветки 3: «Править» на самой странице разбора вёл
// на .../undefined/edit (documentEditPath читает document.slug, которого
// View не отдавал). Тест смотрит на итоговый адрес, а не на факт клика —
// прежний баг рендерил бы ту же форму правки под чужим (несуществующим)
// адресом, потому что /documents/:slug/edit ловит и «undefined» тоже.
describe('DocumentView — кнопка «Править»', () => {
  beforeEach(() => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  });

  it('у сотрудника ведёт на короткий адрес правки, а не на /undefined/edit', async () => {
    useAuth.setState({
      user: { id: 1, email: 'ред@example.com', role: 'editor', nickname: '' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderViewForNavigation({ author_nickname: '' }, '/documents/razbor');

    await userEvent.click(await screen.findByRole('button', { name: 'Править' }));

    expect(await screen.findByTestId('location-display')).toHaveTextContent(
      '/documents/razbor/edit',
    );
  });

  it('у читателя ведёт на длинный адрес правки, а не на /ник/undefined/edit', async () => {
    useAuth.setState({
      user: { id: 5, email: '', role: 'reader', nickname: 'чтец' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderViewForNavigation({ author_nickname: 'чтец', owner_id: 5 }, '/documents/чтец/razbor');

    await userEvent.click(await screen.findByRole('button', { name: 'Править' }));

    expect(await screen.findByTestId('location-display')).toHaveTextContent(
      '/documents/чтец/razbor/edit',
    );
  });
});

describe('DocumentView — Поделиться', () => {
  afterEach(unstubShare);

  it('отдаёт опубликованный читательский разбор по длинному адресу', async () => {
    const share = stubShare();
    renderViewForNavigation({ author_nickname: 'ivan' }, '/documents/ivan/razbor');
    await screen.findByRole('heading', { level: 1, name: 'Разбор' });
    const data = await shareFrom(share);
    expect(data.title).toBe('Разбор — разбор');
    expect(data.url).toBe(`${window.location.origin}/documents/ivan/razbor`);
  });

  it('не предлагает поделиться неопубликованным', async () => {
    renderViewWith({ published_at: null });
    await screen.findByRole('heading', { level: 1, name: 'Разбор' });
    expect(screen.queryByRole('button', { name: 'Поделиться' })).toBeNull();
  });
});
