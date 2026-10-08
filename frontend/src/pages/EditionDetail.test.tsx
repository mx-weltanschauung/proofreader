import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { editionsApi, shelfApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import type { Edition, EditionHighlight, VolumeSummary } from '../types';
import { EditionDetail } from './EditionDetail';
import { shareFrom, stubShare, unstubShare } from '../test/shareStub';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

const EDITION: Edition = {
  id: 1,
  title: 'Сочинения, 2-е изд.',
  slug: 'mae-2',
  description: '',
  created_at: '',
  updated_at: '',
};

// Порядок приходит с сервера: volume_number NULLS LAST, volume_part, title.
// Маршрут отдаёт VolumeSummary — работу с агрегатами; координаты и название
// в фикстуре различаются по тому, остальные обязательные поля — общие
// заглушки (EditionSummary/VolumeTable читают их напрямую, без `?.`).
function volume(over: Partial<VolumeSummary>): VolumeSummary {
  return {
    id: 1,
    title: 'Том',
    author: '',
    language: '',
    country: '',
    file_path: '',
    status: 'draft',
    page_offset: 0,
    owner_id: 1,
    created_at: '',
    updated_at: '',
    pages_total: 0,
    pages_by_status: {},
    chapters_total: 0,
    ...over,
  } as VolumeSummary;
}

const WORKS = [
  volume({ id: 4, title: 'Том 4', volume_number: 4 }),
  volume({ id: 25, title: 'Том 25, часть I', volume_number: 25, volume_part: 'I' }),
  volume({ id: 90, title: 'Предметный указатель А—М' }),
];

function renderDetail() {
  return render(
    <MemoryRouter initialEntries={['/editions/1']}>
      <Routes>
        <Route path="/editions/:id" element={<EditionDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('EditionDetail', () => {
  beforeEach(() => {
    localStorage.clear();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    // По умолчанию избранного нет; тесты строки избранного подменяют это сами.
    vi.spyOn(editionsApi, 'highlights').mockImplementation(() => ok([]));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('переживает null вместо списка томов', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(null));

    renderDetail();

    expect(await screen.findByText(/В собрании пока нет работ/)).toBeInTheDocument();
  });

  it('делится собранием по адресному слагу', async () => {
    const share = stubShare();
    try {
      vi.spyOn(editionsApi, 'get').mockImplementation(() =>
        ok({ ...EDITION, url_slug: 'marx-engels' }),
      );
      vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));
      renderDetail();
      await screen.findByRole('heading', { name: EDITION.title });
      const data = await shareFrom(share);
      expect(data.title).toBe('Сочинения, 2-е изд.');
      expect(data.url).toBe(`${window.location.origin}/editions/1-marx-engels`);
    } finally {
      unstubShare();
    }
  });

  it('показывает сводку и полку корешков', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));

    const { container } = renderDetail();

    expect(await screen.findByRole('heading', { name: EDITION.title })).toBeInTheDocument();
    expect(container.querySelectorAll('.volume-slab')).toHaveLength(WORKS.length);
    // Гостю правка избранного не предлагается.
    expect(screen.queryByRole('link', { name: 'Избранное' })).toBeNull();
    // Обрезки на этом экране нет: собрание показывается целиком. Ищем именно
    // плитку «ещё N →», а не слово «ещё»: голым словом проверка ловила и
    // полосу пропуска («Тома 12—15 ещё не сняты»), которая к обрезке
    // отношения не имеет и на неполном собрании законна.
    expect(screen.queryByText(/ещё\s+\d+/)).toBeNull();
  });

  // Избранное собрания показывается над полкой — тем же компонентом, что
  // правит экран «Избранное». На главной его нет: там только полки.
  it('показывает избранные работы над полкой', async () => {
    const capital: EditionHighlight = {
      chapter_id: 2066,
      chapter_slug: 'kapital',
      chapter_title: 'КАПИТАЛ',
      work_id: 47,
      work_slug: 'mae-t23',
      volume_number: 23,
      volume_part: null,
      label: 'Капитал, т. I',
    };
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));
    vi.spyOn(editionsApi, 'highlights').mockImplementation(() => ok([capital]));

    const { container } = renderDetail();

    const link = await screen.findByRole('link', { name: 'Капитал, т. I, т. 23' });
    expect(link).toHaveAttribute('href', '/works/47-mae-t23/chapters/2066-kapital');
    // Строка стоит раньше полки, а не под ней.
    const list = screen.getByRole('list', { name: 'Избранные работы' });
    const shelf = container.querySelector('.shelf')!;
    expect(list.compareDocumentPosition(shelf) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('без избранного строки нет', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));

    renderDetail();

    expect(await screen.findByRole('heading', { name: EDITION.title })).toBeInTheDocument();
    expect(screen.queryByRole('list', { name: 'Избранные работы' })).toBeNull();
  });

  // Избранное — украшение страницы, а не её содержание: бэкенд старше этой
  // работы отвечает на запрос 404, и собрание обязано показаться всё равно.
  it('провал запроса избранного не роняет страницу', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));
    vi.spyOn(editionsApi, 'highlights').mockImplementation(() => Promise.reject(new Error('404')));

    const { container } = renderDetail();

    expect(await screen.findByRole('heading', { name: EDITION.title })).toBeInTheDocument();
    expect(container.querySelectorAll('.volume-slab')).toHaveLength(WORKS.length);
    expect(screen.queryByText(/Не удалось/)).toBeNull();
  });

  // Вторая из двух заведённых руками точек входа в поиск (первая — карточка
  // тома). Проверяется само сужение: с чужим id читатель уехал бы в выдачу
  // другого собрания, и не упало бы нигде.
  it('затравка поиска сужена до этого собрания', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));
    // Панель поиска (за затравкой) рисует ScopePicker, а он тянет /shelf —
    // без заглушки открытие панели в тесте било бы реальным запросом.
    vi.spyOn(shelfApi, 'get').mockImplementation(() => ok({ editions: [], loose_works: [] }));

    function Probe() {
      const location = useLocation();
      return <output data-testid="loc">{location.pathname + location.search}</output>;
    }
    render(
      <MemoryRouter initialEntries={['/editions/1']}>
        <Routes>
          <Route path="/editions/:id" element={<EditionDetail />} />
        </Routes>
        <Probe />
      </MemoryRouter>,
    );

    await screen.findByRole('heading', { name: EDITION.title });
    await userEvent.click(screen.getByRole('button', { name: /искать в собрании/i }));
    // На EditionDetail второго searchbox-элемента нет (в отличие от
    // WorkDetail с фильтром содержания VolumeOutline), но диалог всё равно —
    // самый узкий контейнер для запроса: правка формулировки внутри панели
    // не тронет этот тест.
    const dialog = screen.getByRole('dialog');
    await userEvent.type(within(dialog).getByRole('searchbox'), 'партия{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent(
      '/search?q=%D0%BF%D0%B0%D1%80%D1%82%D0%B8%D1%8F&editions=1',
    );
  });

  it('переключается на список и запоминает выбор', async () => {
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));

    renderDetail();

    await userEvent.click(await screen.findByRole('button', { name: 'Список' }));
    expect(screen.getByRole('table')).toBeInTheDocument();
    expect(screen.getByRole('button', { name: 'Список' })).toHaveAttribute('aria-pressed', 'true');
    expect(localStorage.getItem('volume-view')).toBe('list');
  });

  it('редактору даёт добавить том и править собрание', async () => {
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(WORKS));

    renderDetail();

    // EDITION без url_slug: адрес откатывается на editions.slug (задача 3).
    expect(await screen.findByRole('link', { name: 'Изменить' })).toHaveAttribute(
      'href',
      `/editions/${EDITION.id}-${EDITION.slug}/edit`,
    );
    expect(screen.getByRole('link', { name: 'Избранное' })).toHaveAttribute(
      'href',
      `/editions/${EDITION.id}-${EDITION.slug}/highlights`,
    );
    expect(screen.getByRole('button', { name: 'Удалить' })).toHaveClass('btn-danger');
  });

  it('редактору у пустого собрания видна «Добавить том»', async () => {
    // Панель действий раньше пряталась целиком внутри volumes.length > 0 —
    // у пустого собрания добавить в него первый том с этого экрана было
    // нельзя. Переключатель вида (Корешки/Список) при этом смысла не имеет
    // и не показывается — показывать нечего.
    useAuth.setState({
      user: { id: 1, email: 'a@b.c', role: 'editor' },
      token: 't',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok([]));

    renderDetail();

    expect(await screen.findByRole('link', { name: 'Добавить том' })).toHaveAttribute(
      'href',
      '/works/new',
    );
    expect(screen.getByText(/В собрании пока нет работ/)).toBeInTheDocument();
    expect(screen.queryByRole('group', { name: 'Вид списка томов' })).toBeNull();
  });

  it('показывает предваряющую работу над томами и не считает её томом', async () => {
    const works = [
      volume({
        id: 101,
        title: 'Предисловие ко второму изданию',
        role: 'edition_front_matter',
        precedes_volume: 1,
        pages_total: 5,
      }),
      volume({ id: 1, title: 'Том 1', volume_number: 1, pages_total: 700 }),
    ];
    vi.spyOn(editionsApi, 'get').mockImplementation(() => ok(EDITION));
    vi.spyOn(editionsApi, 'volumes').mockImplementation(() => ok(works));

    const { container } = renderDetail();

    expect(
      await screen.findByRole('link', { name: /Предисловие ко второму изданию/ }),
    ).toBeInTheDocument();
    // Книга одна: предваряющая работа в штабель не ложится.
    expect(container.querySelectorAll('.volume-slab')).toHaveLength(1);
    // И в сводку собрания она не входит — иначе у Маркса стало бы 52 тома.
    expect(screen.getByText(/1 том(?!а)/)).toBeInTheDocument();
  });

  // Регрессия: эффект выходил на id === null, не сбросив isLoading, —
  // карточка вечно висела на «Загрузка собрания…» вместо уже существующего
  // экрана «Собрание не найдено».
  it('на битом id в адресе показывает отказ, а не вечную загрузку, и ничего не грузит', async () => {
    const get = vi.spyOn(editionsApi, 'get');
    const volumes = vi.spyOn(editionsApi, 'volumes');

    render(
      <MemoryRouter initialEntries={['/editions/xyz']}>
        <Routes>
          <Route path="/editions/:id" element={<EditionDetail />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Собрание не найдено')).toBeInTheDocument();
    expect(screen.queryByText('Загрузка собрания…')).not.toBeInTheDocument();
    expect(get).not.toHaveBeenCalled();
    expect(volumes).not.toHaveBeenCalled();
  });
});
