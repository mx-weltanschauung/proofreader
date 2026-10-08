import { afterEach, describe, it, expect, beforeEach, vi } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, useNavigate } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { useAuth } from '../hooks/useAuth';
import { suggestionsApi, feedbackApi } from '../services/api';
import { Header } from './Header';
import { resetSiteForTests } from '../services/site';
// Header рендерит ReadingSettings, которому нужен провайдер настроек чтения —
// вне брифа, но без него любой рендер Header падает ещё до наших проверок.
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';

// Без этого мока administratorские тесты дёргали бы feedbackApi.unreadCount()
// и suggestionsApi.queue по-настоящему: ответ ушёл бы в .catch() Header'а и
// тест прошёл бы вслепую, не проверяя ничего про сами счётчики.
vi.mock('../services/api', () => ({
  feedbackApi: {
    unreadCount: vi.fn(),
  },
  suggestionsApi: {
    queue: vi.fn(),
  },
}));

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function renderHeader(path = '/') {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ReadingPreferencesProvider>
        <Header />
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

// Кнопка-триггер вне Header: имитирует переход по ссылке где-то на странице
// (сортировка понятия, фильтр очереди, глубина содержания тома) — то есть
// смену query-строки, которая с формой поиска в шапке никак не связана.
// Принимает адрес пропом, а не читает его сама, — так один и тот же
// компонент годится и для перехода «в сторону» (другой путь), и для
// повторной отправки на /search (тот же путь, новый q=).
function NavigateButton({ to }: { to: string }) {
  const navigate = useNavigate();
  return (
    <button type="button" onClick={() => navigate(to)}>
      перейти
    </button>
  );
}

function renderHeaderWithNav(initialEntry: string, to: string) {
  return render(
    <MemoryRouter initialEntries={[initialEntry]}>
      <ReadingPreferencesProvider>
        <Header />
        <NavigateButton to={to} />
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

// Подпись под именем — свойство экземпляра: у чужого корпуса «собрания
// сочинений» врали бы, поэтому без SITE_TAGLINE подписи нет вовсе.
describe('подпись под именем', () => {
  function mockSite(tagline: string) {
    vi.spyOn(globalThis, 'fetch').mockImplementation(async (input) =>
      String(input).endsWith('/api/site')
        ? new Response(JSON.stringify({ site_name: 'Тестовая', site_tagline: tagline }))
        : new Response('', { status: 404 }),
    );
  }
  beforeEach(() => resetSiteForTests());
  afterEach(() => resetSiteForTests());

  it('стоит, когда экземпляр её задал', async () => {
    mockSite('журналы');
    const { container } = renderHeader();
    await screen.findByText('Тестовая');
    expect(container.querySelector('.logo-tagline')?.textContent).toBe('журналы');
    vi.restoreAllMocks();
  });

  it('отсутствует без SITE_TAGLINE', async () => {
    mockSite('');
    const { container } = renderHeader();
    await screen.findByText('Тестовая');
    expect(container.querySelector('.logo-tagline')).toBeNull();
    vi.restoreAllMocks();
  });
});

describe('Header', () => {
  beforeEach(() => {
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    // Значок очереди спрашивает suggestionsApi.queue у любого редактора или
    // администратора — без заглушки тесты били бы реальным axios-запросом.
    vi.mocked(suggestionsApi.queue).mockReset();
    vi.mocked(suggestionsApi.queue).mockReturnValue(ok({ items: [], total: 0 }) as never);
    vi.mocked(feedbackApi.unreadCount).mockReset();
    vi.mocked(feedbackApi.unreadCount).mockReturnValue(ok({ count: 0 }));
    localStorage.clear();
  });

  it('называется «Читальня» и ведёт на главную', () => {
    renderHeader();
    expect(screen.getByRole('link', { name: /Читальня/ })).toHaveAttribute('href', '/');
  });

  // Шапка звала раздел «Подборки», подвал — «Подборки»: один и тот же
  // /collections назывался двумя словами на одном экране.
  it('называет раздел подборками — тем же словом, что и подвал', () => {
    renderHeader();
    expect(screen.getByRole('link', { name: 'Подборки' })).toHaveAttribute('href', '/collections');
  });

  // Витрина разборов — не служебный раздел: её видит гость, не только
  // сотрудник (тот же экран `documentsApi.list` открывает и без токена).
  it('показывает «Разборы» гостю, рядом с «Подборки»', () => {
    renderHeader();
    expect(screen.getByRole('link', { name: 'Разборы' })).toHaveAttribute('href', '/documents');
  });

  it('гостю показывает «Записаться» и не показывает служебных разделов', () => {
    renderHeader();
    expect(screen.getByRole('link', { name: 'Записаться' })).toHaveAttribute('href', '/join');
    expect(screen.queryByRole('link', { name: 'Пользователи' })).toBeNull();
    expect(screen.queryByRole('link', { name: /Предложения/ })).toBeNull();
  });

  // Шапка видна на каждой странице: гость, начавший что-то на полосе и
  // решивший записаться, обязан вернуться туда же, а не на главную.
  it('«Записаться» несёт адрес возврата — шапка видна отовсюду', () => {
    render(
      <MemoryRouter initialEntries={['/works/6/pages/493/suggest']}>
        <ReadingPreferencesProvider>
          <Header />
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );
    expect(screen.getByRole('link', { name: 'Записаться' })).toHaveAttribute(
      'href',
      `/join?next=${encodeURIComponent('/works/6/pages/493/suggest')}`,
    );
  });

  it('«Записаться» с главной не тащит бесполезный next=/', () => {
    renderHeader();
    expect(screen.getByRole('link', { name: 'Записаться' })).toHaveAttribute('href', '/join');
  });

  it('читателю показывает ник ссылкой на /mine и кнопку «Выйти»', () => {
    useAuth.setState({
      user: { id: 5, email: '', nickname: 'Читатель', role: 'reader' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.getByRole('link', { name: 'Читатель' })).toHaveAttribute('href', '/mine');
    expect(screen.getByRole('button', { name: 'Выйти' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Записаться' })).toBeNull();
  });

  // Раздел /editions теперь редирект на главную, куда ведёт и логотип:
  // пункт «Собрания» дублировал бы его самому себе.
  it('не показывает пункт «Собрания» даже редактору', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@proofreader.local', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.queryByRole('link', { name: 'Собрания' })).toBeNull();
    await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
  });

  it('администратору показывает «Пользователи», роль по-русски и «Выйти»', async () => {
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.getByRole('link', { name: 'Пользователи' })).toHaveAttribute(
      'href',
      '/admin/users',
    );
    expect(screen.getByText('администратор')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Выйти' })).toHaveClass('btn');
    await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
  });

  it('редактору показывает ссылку на очередь предложений', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@proofreader.local', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.getByRole('link', { name: /Предложения/ })).toHaveAttribute(
      'href',
      '/suggestions/queue',
    );
    await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
  });

  it('показывает счётчик непросмотренных предложений из total', async () => {
    vi.mocked(suggestionsApi.queue).mockReturnValue(ok({ items: [], total: 5 }) as never);
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(await screen.findByText('5')).toBeInTheDocument();
  });

  it('не рисует счётчик, пока очередь пуста', async () => {
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    await waitFor(() => {
      expect(suggestionsApi.queue).toHaveBeenCalledWith('новое', 0, 0);
    });
    expect(screen.queryByText('0')).not.toBeInTheDocument();
  });

  it('администратору с неразобранными письмами показывает счётчик', async () => {
    vi.mocked(feedbackApi.unreadCount).mockReturnValue(ok({ count: 3 }));
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();

    const link = await screen.findByRole('link', { name: /Обращения/ });
    expect(link).toHaveAttribute('href', '/admin/feedback');
    await waitFor(() => expect(screen.getByText('3')).toBeInTheDocument());
  });

  it('при нуле неразобранных писем значок не рисуется, а ссылка остаётся', async () => {
    vi.mocked(feedbackApi.unreadCount).mockReturnValue(ok({ count: 0 }));
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();

    const link = await screen.findByRole('link', { name: 'Обращения' });
    expect(link).toHaveAttribute('href', '/admin/feedback');
    await waitFor(() => expect(feedbackApi.unreadCount).toHaveBeenCalled());
    expect(screen.queryByText('0')).toBeNull();
  });

  it('администратору показывает ссылку «Кэш»', async () => {
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.getByRole('link', { name: 'Кэш' })).toHaveAttribute('href', '/admin/cache');
    expect(screen.getByRole('link', { name: 'Статистика' })).toHaveAttribute(
      'href',
      '/admin/stats',
    );
    await waitFor(() => expect(feedbackApi.unreadCount).toHaveBeenCalled());
  });

  it('редактору не показывает ссылку «Кэш»', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@proofreader.local', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.queryByRole('link', { name: 'Кэш' })).toBeNull();
    await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
  });

  it('администратору показывает ссылку «Читатели»', async () => {
    useAuth.setState({
      user: { id: 1, email: 'admin@proofreader.local', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.getByRole('link', { name: 'Читатели' })).toHaveAttribute(
      'href',
      '/admin/readers',
    );
    await waitFor(() => expect(feedbackApi.unreadCount).toHaveBeenCalled());
  });

  // Список читателей и их подборок — личные данные, и вход сюда меряется
  // ролью, а не фактом входа: редактор его не видит ни ссылкой, ни маршрутом.
  it('редактору не показывает ссылку «Читатели»', async () => {
    useAuth.setState({
      user: { id: 2, email: 'editor@proofreader.local', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    renderHeader();
    expect(screen.queryByRole('link', { name: 'Читатели' })).toBeNull();
    await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
  });

  // Задача 12 заменила поле поиска в шапке затравкой-кнопкой: у неё больше
  // нет собственного черновика, который можно потерять перемонтированием
  // (searchFormKey и объяснявший его комментарий удалены вместе с полем —
  // стирать в затравке нечего). Проверки ниже перенесены на то, что
  // действительно осталось верным для кнопки: она не перемонтируется на
  // чужую смену query-параметров, а на /search её видимый текст следует за
  // текущим запросом.
  describe('служебная полоса', () => {
    const admin = { id: 1, email: 'admin@proofreader.local', role: 'administrator' as const };
    const editor = { id: 2, email: 'editor@proofreader.local', role: 'editor' as const };

    function signIn(user: typeof admin | typeof editor) {
      useAuth.setState({ user, token: 't', isAuthenticated: true, isLoading: false });
    }

    it('у читателя её нет, и основной ряд тот же, что у сотрудника', () => {
      renderHeader();
      expect(screen.queryByRole('navigation', { name: 'Служебное' })).toBeNull();
      const main = screen.getByRole('navigation', { name: 'Разделы' });
      expect(
        within(main)
          .getAllByRole('link')
          .map((a) => a.textContent),
      ).toEqual(['Указатель', 'Подборки', 'Разборы']);
    });

    it('администратору выносит все служебные ссылки из основного ряда в полосу', async () => {
      signIn(admin);
      renderHeader();
      const main = screen.getByRole('navigation', { name: 'Разделы' });
      expect(within(main).getAllByRole('link')).toHaveLength(3);
      const staff = screen.getByRole('navigation', { name: 'Служебное' });
      expect(
        within(staff)
          .getAllByRole('link')
          .map((a) => a.getAttribute('href')),
      ).toEqual([
        '/suggestions/queue',
        '/documents/review',
        '/admin/audio',
        '/admin/users',
        '/admin/readers',
        '/admin/feedback',
        '/admin/cache',
        '/admin/stats',
      ]);
      await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
    });

    it('редактору даёт только очередь, без управления', async () => {
      signIn(editor);
      renderHeader();
      const staff = screen.getByRole('navigation', { name: 'Служебное' });
      expect(
        within(staff)
          .getAllByRole('link')
          .map((a) => a.getAttribute('href')),
      ).toEqual(['/suggestions/queue', '/documents/review', '/admin/audio']);
      expect(within(staff).queryByText('Управление')).toBeNull();
      await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
    });
  });

  describe('текущий раздел', () => {
    it('отмечает раздел и на вложенном адресе', () => {
      renderHeader('/concepts/prisvoenie');
      expect(screen.getByRole('link', { name: 'Указатель' })).toHaveAttribute(
        'aria-current',
        'page',
      );
      expect(screen.getByRole('link', { name: 'Подборки' })).not.toHaveAttribute('aria-current');
    });

    // /documents/review лежит внутри /documents: префиксное сравнение
    // отметило бы обе ссылки разом.
    it('на очереди разборов отмечает её одну, а не «Разборы» заодно', async () => {
      useAuth.setState({
        user: { id: 2, email: 'editor@proofreader.local', role: 'editor' },
        token: 't',
        isAuthenticated: true,
        isLoading: false,
      });
      renderHeader('/documents/review');
      expect(screen.getByRole('link', { name: 'Разборы на проверку' })).toHaveAttribute(
        'aria-current',
        'page',
      );
      expect(screen.getByRole('link', { name: 'Разборы' })).not.toHaveAttribute('aria-current');
      await waitFor(() => expect(suggestionsApi.queue).toHaveBeenCalled());
    });

    it('не отмечает раздел, чей адрес лишь начинается теми же буквами', () => {
      renderHeader('/documentsX');
      expect(screen.getByRole('link', { name: 'Разборы' })).not.toHaveAttribute('aria-current');
    });
  });

  describe('затравка поиска в шапке', () => {
    it('не перемонтируется при смене query-параметров вне /search', async () => {
      renderHeaderWithNav('/concepts?sort=asc', '/concepts?sort=desc');
      const button = screen.getByRole('button', { name: /поиск/i });

      await userEvent.click(screen.getByRole('button', { name: 'перейти' }));

      expect(screen.getByRole('button', { name: /поиск/i })).toBe(button);
    });

    it('на /search показывает текущий запрос и обновляется при новом', async () => {
      renderHeaderWithNav('/search?q=старый', '/search?q=новый');
      const button = screen.getByRole('button', { name: /поиск/i });
      expect(button).toHaveTextContent('старый');

      await userEvent.click(screen.getByRole('button', { name: 'перейти' }));

      expect(button).toHaveTextContent('новый');
    });
  });
});
