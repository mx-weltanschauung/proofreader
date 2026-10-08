import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import { Dashboard } from './Dashboard';
import { shelfApi } from '../services/api';
// useAuth — это и есть zustand-стор (create<AuthState>()), поэтому у него
// есть setState: роль в тестах подменяется прямо на нём, без vi.mock.
import { useAuth } from '../hooks/useAuth';
import { LEGACY_LAST_READ_KEY, RECENT_KEY, readRecent } from '../hooks/useReadingProgress';
import type { Edition, ShelfEdition, ShelfWork, VolumeSummary } from '../types';

vi.mock('../services/api', () => ({
  shelfApi: { get: vi.fn() },
}));

// Сброс перед каждым тестом файла: тест «редактору даёт создать собрание»
// подменяет роль, и без сброса она утекла бы в соседние тесты. Счётчик
// вызовов сбрасывается там же — тест «берёт всё одним запросом» считает
// обращения к API и без этого видел бы ещё и чужие.
beforeEach(() => {
  useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
  vi.clearAllMocks();
});

const EDITION: Edition = {
  id: 1,
  title: 'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.',
  slug: 'mae-2',
  description: '',
  created_at: '',
  updated_at: '',
};

// Второе собрание в базе. По алфавиту «Г» идёт раньше «К», поэтому именно оно
// приходит первым — и на нём ловится предположение о единственном собрании.
const PLEKHANOV: Edition = {
  id: 2,
  title: 'Г. В. Плеханов. Сочинения в 24 томах, 2-е изд.',
  slug: 'plekhanov-2',
  description: '',
  created_at: '',
  updated_at: '',
};

function volume(id: number, num: number | undefined, over: Partial<VolumeSummary> = {}) {
  return {
    id,
    title: `К. Маркс и Ф. Энгельс. Сочинения. Том ${num}`,
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '',
    volume_number: num,
    pages_total: 600,
    pages_by_status: {},
    chapters_total: 5,
    ...over,
  } as VolumeSummary;
}

function shelfOf(edition: Edition, volumes: VolumeSummary[]): ShelfEdition {
  return { edition, volumes };
}

/**
 * Ответ /api/shelf целиком: полки в том порядке, в каком их отдал сервер, и
 * работы вне собраний. Один мок на всю страницу — столько же, сколько запросов
 * она делает.
 */
function mockShelf(editions: ShelfEdition[], loose: ShelfWork[] = []) {
  vi.mocked(shelfApi.get).mockResolvedValue({
    data: { editions, loose_works: loose },
  } as never);
}

/** Одно собрание из n томов и без работ вне собраний. */
function mockVolumes(n: number) {
  mockShelf([
    shelfOf(
      EDITION,
      Array.from({ length: n }, (_, i) => volume(i + 1, i + 1)),
    ),
  ]);
}

function renderDashboard() {
  return render(
    <MemoryRouter>
      <Dashboard />
    </MemoryRouter>,
  );
}

describe('Dashboard', () => {
  beforeEach(() => {
    localStorage.clear();
    mockShelf([shelfOf(EDITION, [volume(1, 1), volume(3, 3), volume(9, 9)])]);
  });

  it('называет собрание, а не «Works Dashboard»', async () => {
    renderDashboard();
    expect(
      await screen.findByRole('heading', { name: 'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.' }),
    ).toBeInTheDocument();
  });

  // Раньше с главной нельзя было попасть на витрину разборов — только через
  // шапку или набранный вручную адрес. Указатель и подборки сюда не дублируются
  // — их и так показывает шапка на каждой странице.
  it('ведёт на витрину разборов', async () => {
    renderDashboard();
    await screen.findByRole('heading', { name: EDITION.title });
    expect(screen.getByRole('link', { name: 'Разборы' })).toHaveAttribute('href', '/documents');
  });

  // Страница берёт всё одним ответом: прежде она делала шесть запросов в две
  // волны — список собраний, каталог работ, а затем по запросу на собрание, и
  // вторая волна не могла начаться, пока не ответила первая.
  it('берёт всё одним запросом', async () => {
    renderDashboard();
    await screen.findByRole('heading', { name: EDITION.title });
    expect(vi.mocked(shelfApi.get)).toHaveBeenCalledTimes(1);
  });

  // Раньше /editions/:id был достижим только из шапки (роли editor+) или с
  // карточки работы — с главной гость туда попасть не мог.
  it('ведёт с заголовка полки на страницу собрания', async () => {
    renderDashboard();
    const heading = await screen.findByRole('heading', { name: EDITION.title });
    // EDITION без url_slug: адрес откатывается на editions.slug (задача 3),
    // как и на сервере (effectiveEditionSlug) — не на голый номер.
    expect(heading.querySelector('a')).toHaveAttribute(
      'href',
      `/editions/${EDITION.id}-${EDITION.slug}`,
    );
  });

  // Полка раскладывает тома по номеру тома, а не по порядку ответа. Порядок
  // ответа задан в SQL (`ORDER BY volume_number NULLS LAST, volume_part NULLS
  // FIRST`) и на живых данных совпадает с этим — но полке нужна именно
  // числовая ось: на ней стоят и пустые места отсутствующих томов, а место
  // пропуска из порядка ответа не выводится, его в ответе нет.
  it('раскладывает тома по номеру, как они стоят в собрании', async () => {
    mockShelf([shelfOf(EDITION, [volume(9, 9), volume(1, 1), volume(3, 3)])]);
    renderDashboard();
    // Именование по VolumeSpine.aria-label («Том N…») отсекает ссылку
    // заголовка полки на /editions/:id — она тоже <a>, но не том.
    await waitFor(() => expect(screen.getAllByRole('link', { name: /^Том / })).toHaveLength(3));
    const hrefs = screen.getAllByRole('link', { name: /^Том / }).map((a) => a.getAttribute('href'));
    expect(hrefs).toEqual(['/works/1', '/works/3', '/works/9']);
  });

  // Всем десяти работам стоит status: draft. Плашка одинакова везде и не
  // сообщает ничего — на полке её нет.
  it('не показывает плашку статуса работы', async () => {
    renderDashboard();
    await waitFor(() => expect(screen.getAllByRole('link', { name: /^Том / })).toHaveLength(3));
    expect(screen.queryByText(/draft/i)).not.toBeInTheDocument();
  });

  it('без записи о чтении не предлагает продолжить', async () => {
    renderDashboard();
    await waitFor(() => expect(screen.getAllByRole('link', { name: /^Том / })).toHaveLength(3));
    expect(screen.queryByRole('link', { name: /Продолжить/ })).not.toBeInTheDocument();
  });

  // Запись, оставленная до списка: у читателя, пришедшего после выкатки,
  // «Продолжить» не должно пропасть.
  it('предлагает продолжить с места, сохранённого до списка', async () => {
    localStorage.setItem(
      LEGACY_LAST_READ_KEY,
      JSON.stringify({
        workId: 8,
        chapterId: 55,
        workTitle: 'К. Маркс и Ф. Энгельс. Сочинения. Том 8',
        chapterTitle: 'Восемнадцатое брюмера Луи Бонапарта',
        pageNumber: 214,
        ts: 2,
      }),
    );
    renderDashboard();
    const link = await screen.findByRole('link', { name: /Продолжить/ });
    expect(link).toHaveAttribute('href', '/works/8/chapters/55');
    expect(screen.getByText(/Восемнадцатое брюмера/)).toBeInTheDocument();
    expect(screen.getByText(/214/)).toBeInTheDocument();
  });

  describe('несколько начатых глав', () => {
    const RECENT = [
      {
        workId: 8,
        chapterId: 55,
        workTitle: 'Том 8',
        chapterTitle: 'Восемнадцатое брюмера',
        pageNumber: 214,
        ts: 3,
      },
      {
        workId: 3,
        chapterId: 10,
        workTitle: 'Том 3',
        chapterTitle: 'Немецкая идеология',
        pageNumber: null,
        ts: 2,
      },
    ];

    beforeEach(() => localStorage.setItem(RECENT_KEY, JSON.stringify(RECENT)));

    it('показывает все, свежую первой', async () => {
      renderDashboard();
      const block = await screen.findByRole('region', { name: 'Продолжить чтение' });
      const links = within(block).getAllByRole('link');
      expect(links.map((a) => a.getAttribute('href'))).toEqual([
        '/works/8/chapters/55',
        '/works/3/chapters/10',
      ]);
      // Имя ссылки несёт главу: две одинаковые «Продолжить» подряд читалка
      // экрана не различила бы.
      expect(links[1]).toHaveAccessibleName('Продолжить: Немецкая идеология');
      // Номера страницы нет — хвоста «стр.» тоже.
      expect(within(block).getByText('Том 3')).toBeInTheDocument();
    });

    it('крестик снимает строку и с экрана, и из хранилища', async () => {
      const user = userEvent.setup();
      renderDashboard();
      await user.click(
        await screen.findByRole('button', { name: 'Убрать «Восемнадцатое брюмера» из списка' }),
      );
      const block = screen.getByRole('region', { name: 'Продолжить чтение' });
      expect(
        within(block)
          .getAllByRole('link')
          .map((a) => a.getAttribute('href')),
      ).toEqual(['/works/3/chapters/10']);
      expect(readRecent().map((e) => e.chapterId)).toEqual([10]);
    });

    it('сняв последнюю строку, возвращает фразу о читальне', async () => {
      localStorage.setItem(RECENT_KEY, JSON.stringify([RECENT[0]]));
      const user = userEvent.setup();
      renderDashboard();
      await user.click(
        await screen.findByRole('button', { name: 'Убрать «Восемнадцатое брюмера» из списка' }),
      );
      expect(screen.queryByRole('region', { name: 'Продолжить чтение' })).not.toBeInTheDocument();
      expect(screen.getByText(/^3 тома, /, { selector: '.shelf-intro' })).toBeInTheDocument();
    });
  });

  it('пустое собрание приглашает загрузить первый том', async () => {
    mockShelf([shelfOf(EDITION, [])]);
    renderDashboard();
    // Тот же текст есть и в сводке EditionSummary — уточняем до приглашения
    // в самом пустом состоянии.
    expect(
      await screen.findByText(/Пока ни одного тома/, { selector: '.empty-state p' }),
    ).toBeInTheDocument();
  });
});

// --- Несколько собраний --------------------------------------------------
// Страница брала editions[0], а список приходит отсортированным по названию:
// «Г. В. Плеханов» с одним томом заслонял всё собрание Маркса и Энгельса.
// Теперь порядок задаёт сама страница — по числу томов, — и алфавит ответа на
// него не влияет вовсе.

describe('Dashboard: собраний несколько', () => {
  beforeEach(() => {
    localStorage.clear();
    mockShelf([
      shelfOf(PLEKHANOV, [volume(41, 5)]),
      shelfOf(EDITION, [volume(1, 1), volume(3, 3)]),
    ]);
  });

  it('ставит полку под каждое собрание, а не только под первое', async () => {
    renderDashboard();
    await screen.findByRole('heading', { name: PLEKHANOV.title });
    expect(screen.getByRole('heading', { name: EDITION.title })).toBeInTheDocument();
  });

  it('показывает тома всех собраний', async () => {
    renderDashboard();
    await waitFor(() => expect(screen.getAllByRole('link', { name: /^Том / })).toHaveLength(3));
    const hrefs = screen.getAllByRole('link', { name: /^Том / }).map((a) => a.getAttribute('href'));
    // Сперва собрание, которого в читальне больше (два тома), потом
    // одинокое — хотя в ответе они пришли в обратном порядке, по алфавиту.
    expect(hrefs).toEqual(['/works/1', '/works/3', '/works/41']);
  });

  // Алфавит заголовка — не порядок: сорок пять ленинских томов стояли выше
  // четырёх плехановских не почему-либо, а потому что «В» раньше «Г».
  it('ставит собрание с бо́льшим числом томов первым', async () => {
    renderDashboard();
    const headings = await screen.findAllByRole('heading', { level: 2 });
    expect(headings.map((h) => h.textContent)).toEqual([EDITION.title, PLEKHANOV.title]);
  });

  // Счётчик — часть сводки EditionSummary под каждым заголовком и считает
  // тома своего собрания.
  it('считает тома по собраниям, а не всем скопом', async () => {
    renderDashboard();
    expect(await screen.findByText(/^1 том,/)).toBeInTheDocument();
    expect(screen.getByText(/^2 тома,/)).toBeInTheDocument();
  });

  // Собрание без томов не пропускается молча: администратор должен увидеть,
  // что оно заведено, иначе полка выглядит так, будто его нет.
  it('называет собрание без томов, а не прячет его', async () => {
    mockShelf([shelfOf(PLEKHANOV, []), shelfOf(EDITION, [volume(1, 1)])]);
    renderDashboard();
    expect(await screen.findByRole('heading', { name: PLEKHANOV.title })).toBeInTheDocument();
    // Тот же текст есть и в сводке EditionSummary — уточняем до приглашения
    // в самом пустом состоянии.
    expect(
      screen.getByText(/Пока ни одного тома/, { selector: '.empty-state p' }),
    ).toBeInTheDocument();
  });
});

// --- Работы вне собраний -------------------------------------------------
// В форме создания работы поля собрания нет, поэтому созданной через
// интерфейс работе достаётся edition_id = NULL. На полке ей места нет, а
// других списков работ в интерфейсе не осталось. Отбирает их теперь сервер:
// прежде страница просила двести работ и фильтровала на клиенте.

describe('Dashboard: работы вне собраний', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('выводит работу, не приписанную ни к одному собранию', async () => {
    mockShelf([shelfOf(EDITION, [volume(1, 1)])], [{ id: 77, title: 'Одинокая брошюра' }]);
    renderDashboard();
    const link = await screen.findByRole('link', { name: 'Одинокая брошюра' });
    expect(link).toHaveAttribute('href', '/works/77');
    expect(screen.getByRole('heading', { name: 'Вне собраний' })).toBeInTheDocument();
  });

  it('не заводит раздел, когда все работы разложены по собраниям', async () => {
    mockShelf([shelfOf(EDITION, [volume(1, 1)])]);
    renderDashboard();
    await screen.findByRole('heading', { name: EDITION.title });
    expect(screen.queryByRole('heading', { name: 'Вне собраний' })).not.toBeInTheDocument();
  });
});

// --- Пусто и сломано -----------------------------------------------------

describe('Dashboard: нечего показать', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('говорит, что собраний нет, вместо голого заголовка', async () => {
    mockShelf([]);
    renderDashboard();
    expect(await screen.findByText(/Пока ни одного собрания/)).toBeInTheDocument();
  });

  // /editions удаляется следующей задачей, а вместе с ним — прежний вход в
  // создание собрания. На пустой установке (собраний ещё ни одного) кнопка
  // обязана остаться доступной, иначе создать первое собрание станет негде.
  it('редактору доступно создать собрание, даже если собраний ещё нет', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    mockShelf([]);
    renderDashboard();
    expect(await screen.findByRole('link', { name: 'Создать собрание' })).toHaveAttribute(
      'href',
      '/editions/new',
    );
  });

  // Строка действий («Добавить том») не зависит от числа собраний и уже
  // ведёт на /works/new — второй ссылки туда же в самом пустом состоянии
  // не нужно, только поясняющий текст.
  it('в пустом состоянии не дублирует ссылку «Добавить том»', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    mockShelf([]);
    renderDashboard();
    await screen.findByText(/Пока ни одного собрания/);
    expect(
      screen.getAllByRole('link', { name: /том/i }).map((a) => a.getAttribute('href')),
    ).toEqual(['/works/new']);
  });

  // Приглашение загрузить том поверх сообщения об ошибке врёт: полка пуста не
  // потому, что в ней ничего нет, а потому, что её не удалось прочитать. Та же
  // логика распространяется и на строку действий редактора.
  it('на ошибке не зовёт загружать, а показывает саму ошибку', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.mocked(shelfApi.get).mockRejectedValue(new Error('сеть отвалилась'));
    renderDashboard();
    expect(await screen.findByText(/Не удалось загрузить собрание/)).toBeInTheDocument();
    expect(screen.queryByText(/Пока ни одного собрания/)).not.toBeInTheDocument();
    expect(screen.queryByText(/Пока ни одного тома/)).not.toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Создать собрание' })).not.toBeInTheDocument();
  });
});

// --- Карточка собрания ----------------------------------------------------

describe('Dashboard: карточка собрания', () => {
  // Полка больше не обрезается на двенадцати корешках. Обрезка появилась,
  // когда корешок был шириной 64px и сорок пять томов не помещались; теперь
  // толщина равна объёму тома, ряд переносится, и собрание видно целиком —
  // а «ещё 33 →» и «ВСЁ СОБРАНИЕ →» вместе с обрезкой ушли. Заголовок
  // собрания и есть ссылка на него.
  it('показывает собрание целиком, без обрезки и плитки «ещё»', async () => {
    mockVolumes(14);
    renderDashboard();

    expect(
      await screen.findByText(/14 томов/, { selector: '.edition-summary-counts' }),
    ).toBeInTheDocument();
    await waitFor(() => expect(screen.getAllByRole('link', { name: /^Том / })).toHaveLength(14));
    expect(screen.queryByText(/ещё/)).toBeNull();
    expect(screen.queryByRole('link', { name: /Всё собрание/ })).toBeNull();
    // EDITION без url_slug: адрес откатывается на editions.slug (задача 3).
    expect(screen.getByRole('heading', { name: EDITION.title }).querySelector('a')).toHaveAttribute(
      'href',
      `/editions/${EDITION.id}-${EDITION.slug}`,
    );
  });

  // Полное заглавие («К. Маркс и Ф. Энгельс. Сочинения. Том N») сюда не
  // «Последний — т. N» — служебная метка, читателю она ничего не говорит.
  it('не печатает последний загруженный том', async () => {
    mockShelf([
      shelfOf(EDITION, [
        volume(1, 1, { updated_at: '2026-01-01' }),
        volume(2, 2, { updated_at: '2026-06-01' }),
      ]),
    ]);
    renderDashboard();
    expect(
      await screen.findByText(/^2 тома,/, { selector: '.edition-summary-counts' }),
    ).toBeInTheDocument();
    expect(screen.queryByText(/Последний/)).toBeNull();
  });

  it('редактору даёт создать собрание — с /editions эта кнопка ушла', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'administrator' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    mockVolumes(3);
    renderDashboard();

    expect(await screen.findByRole('link', { name: 'Создать собрание' })).toHaveAttribute(
      'href',
      '/editions/new',
    );
  });
});

// --- Предваряющие работы (предисловие к группе томов) --------------------
// EditionDetail тот же список фильтрует, а главная раньше — нет: счётчик
// печатал лишний том, а обрезанная SHELF_LIMIT полка вытесняла настоящие
// тома предисловиями.

describe('Dashboard: предваряющая работа не путается с томом', () => {
  beforeEach(() => {
    localStorage.clear();
  });

  it('не считает предисловие томом и не ставит его на полку корешков', async () => {
    mockShelf([
      shelfOf(EDITION, [
        volume(50, undefined, {
          title: 'Предисловие ко второму изданию',
          role: 'edition_front_matter',
          precedes_volume: 1,
          volume_number: undefined,
        }),
        volume(1, 1),
        volume(3, 3),
      ]),
    ]);

    renderDashboard();

    // Счётчик — сводка по томам, предисловие в него не входит.
    expect(
      await screen.findByText(/^2 тома,/, { selector: '.edition-summary-counts' }),
    ).toBeInTheDocument();
    // На полке — только два корешка тома, подписи предисловия там нет.
    await waitFor(() => expect(screen.getAllByRole('link', { name: /^Том / })).toHaveLength(2));
    expect(screen.queryByRole('link', { name: /Предисловие/ })).not.toBeInTheDocument();
  });
});
