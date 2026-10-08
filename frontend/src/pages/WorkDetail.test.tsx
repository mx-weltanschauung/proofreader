import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation, useNavigate } from 'react-router-dom';
import type { Chapter, PageMapEntry, Work } from '../types';

const work: Work = {
  id: 41,
  title: 'К. Маркс и Ф. Энгельс. Сочинения. Том 16',
  author: '',
  language: '',
  country: '',
  file_path: 'works/41/original/t16.pdf',
  status: 'draft',
  owner_id: 1,
  page_offset: 0,
  created_at: '',
  updated_at: '',
};

const chapters: Chapter[] = [
  {
    id: 1664,
    work_id: 41,
    title: 'К. Маркс. Учредительный манифест',
    type: 'chapter',
    order_number: 1,
    start_page: 1,
    end_page: 2,
  } as Chapter,
];

const pageMap: PageMapEntry[] = [
  { page_number: 1, status: 'не_вычитана' },
  { page_number: 2, status: 'вычитано_машиной' },
  { page_number: 3, status: 'не_вычитана' },
];

const { user: authUser } = vi.hoisted(() => ({ user: { value: null as unknown } }));

vi.mock('../hooks/useAuth', () => ({
  useAuth: () => ({ user: authUser.value }),
  canEdit: (u: unknown) => u !== null,
}));

vi.mock('../services/api', () => ({
  worksApi: {
    get: vi.fn(() => Promise.resolve({ data: work })),
    pageMap: vi.fn(() => Promise.resolve({ data: pageMap })),
    delete: vi.fn(),
    uploadFile: vi.fn(),
    createPages: vi.fn(),
  },
  chaptersApi: {
    list: vi.fn(() => Promise.resolve({ data: chapters })),
    delete: vi.fn(),
    move: vi.fn(),
  },
  editionsApi: { get: vi.fn(() => Promise.resolve({ data: { id: 1, title: 'Собрание' } })) },
  pagesApi: {},
  // Панель поиска (за затравкой) рисует ScopePicker, а он тянет /shelf —
  // без заглушки открытие панели в тесте било бы реальным запросом.
  shelfApi: { get: vi.fn(() => Promise.resolve({ data: { editions: [], loose_works: [] } })) },
  API_BASE_URL: '',
  audioApi: {
    forWork: vi.fn(() => Promise.resolve({ data: { tracks: [], recordings: [] } })),
    enqueue: vi.fn(),
    requeueStale: vi.fn(),
  },
}));

import { WorkDetail } from './WorkDetail';
import { RECENT_KEY } from '../hooks/useReadingProgress';
import { audioApi, worksApi, chaptersApi } from '../services/api';

function setup() {
  return render(
    <MemoryRouter initialEntries={['/works/41']}>
      <Routes>
        <Route path="/works/:id" element={<WorkDetail />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('WorkDetail', () => {
  beforeEach(() => {
    authUser.value = null;
    vi.clearAllMocks();
  });

  // Затравка «искать в томе» — одна из трёх точек входа в поиск, и обе
  // скоуповые заводятся руками. Проверяется не наличие кнопки, а именно
  // сужение: с чужим id читатель уехал бы в выдачу другого тома, и не
  // упало бы нигде.
  it('затравка поиска сужена до этого тома', async () => {
    const user = userEvent.setup();
    function Probe() {
      const location = useLocation();
      return <output data-testid="loc">{location.pathname + location.search}</output>;
    }
    render(
      <MemoryRouter initialEntries={['/works/41']}>
        <Routes>
          <Route path="/works/:id" element={<WorkDetail />} />
        </Routes>
        <Probe />
      </MemoryRouter>,
    );

    await screen.findByRole('heading', { level: 1 });
    await user.click(screen.getByRole('button', { name: /искать в томе/i }));
    // Не голый getByRole('searchbox'): у тома есть ещё «Поиск по работам»
    // (фильтр содержания, VolumeOutline) — второе несвязанное поле с той же
    // ролью. Сужаем до диалога панели, а не подбираем имя по тексту метки:
    // так тест переживёт правку формулировки внутри панели.
    const dialog = screen.getByRole('dialog');
    await user.type(within(dialog).getByRole('searchbox'), 'партия{Enter}');
    expect(screen.getByTestId('loc')).toHaveTextContent(
      '/search?q=%D0%BF%D0%B0%D1%80%D1%82%D0%B8%D1%8F&works=41',
    );
  });

  it('не грузит полный список страниц', async () => {
    setup();
    await screen.findByRole('heading', { level: 1 });
    expect(worksApi.pageMap).toHaveBeenCalledWith(41);
  });

  it('показывает крышку, обрез и содержание', async () => {
    setup();
    expect(await screen.findByRole('img', { name: 'Обрез тома' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Учредительный манифест' })).toBeInTheDocument();
    expect(screen.getByText('Вне оглавления')).toBeInTheDocument();
  });

  // Открыв том, читатель ищет, что в нём написано, — то есть оглавление.
  // Обрез тома со статусами полос отвечает на другой вопрос и стоит ниже.
  it('оглавление идёт раньше обреза тома', async () => {
    const { container } = setup();
    await screen.findByRole('img', { name: 'Обрез тома' });
    const toc = container.querySelector('.vol-toc')!;
    const scale = container.querySelector('.vol-scale')!;
    expect(toc.compareDocumentPosition(scale) & Node.DOCUMENT_POSITION_FOLLOWING).toBeTruthy();
  });

  it('гость не видит панели управления', async () => {
    setup();
    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByText('Управление томом')).toBeNull();
  });

  it('редактор получает панель управления', async () => {
    authUser.value = { id: 1, email: 'e@x', role: 'editor' };
    setup();
    expect(await screen.findByText('Управление томом')).toBeInTheDocument();
  });

  it('подсветка статуса из легенды гасит клетки в карточках', async () => {
    const user = userEvent.setup();
    setup();
    await screen.findByRole('img', { name: 'Обрез тома' });
    // Постраничные клетки главы в содержании свёрнуты по умолчанию —
    // разворачиваем перед тем, как искать клетку по aria-label.
    await user.click(screen.getByRole('button', { name: 'Постранично: Учредительный манифест' }));
    await user.click(screen.getByRole('button', { name: 'вычитано машиной, 1' }));
    expect(screen.getByRole('link', { name: 'стр. 1, не вычитана' })).toHaveClass('is-dimmed');
  });

  it('пока карта страниц летит, не показывает «том ещё не расписан»', async () => {
    type PageMapResponse = Awaited<ReturnType<typeof worksApi.pageMap>>;
    let resolvePageMap: (value: PageMapResponse) => void = () => {};
    vi.mocked(worksApi.pageMap).mockReturnValueOnce(
      new Promise<PageMapResponse>((resolve) => {
        resolvePageMap = resolve;
      }),
    );
    setup();

    // Работа и главы уже пришли (h1 отрисован), а карта страниц — ещё нет.
    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByText('Том ещё не расписан по страницам.')).toBeNull();
    expect(
      screen.queryByText(
        'В томе ещё нет страниц. Загрузите файл и создайте их в панели управления.',
      ),
    ).toBeNull();

    resolvePageMap({ data: pageMap } as unknown as PageMapResponse);
    expect(await screen.findByRole('img', { name: 'Обрез тома' })).toBeInTheDocument();
    expect(screen.queryByText('Том ещё не расписан по страницам.')).toBeNull();
  });

  it('карта не загрузилась — «Читать с начала» остаётся на крышке', async () => {
    vi.mocked(worksApi.pageMap).mockRejectedValueOnce(new Error('нет связи'));
    setup();
    expect(await screen.findByText('Карту страниц не удалось загрузить')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Читать с начала' })).toHaveAttribute(
      'href',
      '/works/41/chapters/1664',
    );
  });

  it('карта не загрузилась — содержание живёт, обреза нет', async () => {
    vi.mocked(worksApi.pageMap).mockRejectedValueOnce(new Error('нет связи'));
    setup();
    expect(await screen.findByText('Карту страниц не удалось загрузить')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: 'Учредительный манифест' })).toBeInTheDocument();
    expect(screen.queryByRole('img', { name: 'Обрез тома' })).toBeNull();
  });

  it('подсветка статуса сбрасывается при переходе на другую работу', async () => {
    const user = userEvent.setup();

    function NavigateAway() {
      const navigate = useNavigate();
      return (
        <button type="button" onClick={() => navigate('/works/42')}>
          к другой работе
        </button>
      );
    }

    render(
      <MemoryRouter initialEntries={['/works/41']}>
        <NavigateAway />
        <Routes>
          <Route path="/works/:id" element={<WorkDetail />} />
        </Routes>
      </MemoryRouter>,
    );

    await screen.findByRole('img', { name: 'Обрез тома' });
    // Постраничные клетки главы в содержании свёрнуты по умолчанию —
    // разворачиваем перед тем, как искать клетку по aria-label.
    await user.click(screen.getByRole('button', { name: 'Постранично: Учредительный манифест' }));
    await user.click(screen.getByRole('button', { name: 'вычитано машиной, 1' }));
    expect(screen.getByRole('link', { name: 'стр. 1, не вычитана' })).toHaveClass('is-dimmed');

    // Тот же маршрут /works/:id без key на Route: React не размонтирует
    // компонент при смене id, значит подсветку обязан сбросить сам WorkDetail.
    await user.click(screen.getByRole('button', { name: 'к другой работе' }));
    await screen.findByRole('img', { name: 'Обрез тома' });
    expect(screen.getByRole('link', { name: 'стр. 1, не вычитана' })).not.toHaveClass('is-dimmed');
  });

  // Регрессия: загрузчик выходил на id === null, не сбросив isLoading, — том
  // вечно висел на «Загружаем том…» вместо уже существующего экрана «Том не
  // найден».
  it('на битом id в адресе показывает отказ, а не вечную загрузку, и ничего не грузит', async () => {
    render(
      <MemoryRouter initialEntries={['/works/xyz']}>
        <Routes>
          <Route path="/works/:id" element={<WorkDetail />} />
        </Routes>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Том не найден')).toBeInTheDocument();
    expect(screen.queryByText('Загружаем том…')).not.toBeInTheDocument();
    expect(worksApi.get).not.toHaveBeenCalled();
    expect(worksApi.pageMap).not.toHaveBeenCalled();
    expect(chaptersApi.list).not.toHaveBeenCalled();
  });

  // Ради этого список и заведён: прежняя единственная запись затиралась
  // чтением другого тома, и «Продолжить» в шапке этого пропадало.
  it('«Продолжить» ведёт на свежее место этого тома, даже если потом читали другой', async () => {
    localStorage.setItem(
      RECENT_KEY,
      JSON.stringify([
        {
          workId: 7,
          chapterId: 900,
          workTitle: 'Том 7',
          chapterTitle: 'Чужая',
          pageNumber: 5,
          ts: 3,
        },
        {
          workId: 41,
          chapterId: 1664,
          workTitle: work.title,
          chapterTitle: 'Учредительный манифест',
          pageNumber: 2,
          ts: 2,
        },
        {
          workId: 41,
          chapterId: 1665,
          workTitle: work.title,
          chapterTitle: 'Давняя',
          pageNumber: 9,
          ts: 1,
        },
      ]),
    );
    try {
      setup();
      const resume = await screen.findByRole('link', { name: /^Продолжить/ });
      expect(resume).toHaveTextContent('Продолжить: Учредительный манифест, стр. 2');
      expect(resume.getAttribute('href')).toMatch(/\/chapters\/1664$/);
    } finally {
      localStorage.clear();
    }
  });
});

describe('звук тома', () => {
  beforeEach(() => {
    authUser.value = null;
    vi.clearAllMocks();
    // clearAllMocks не снимает mockReturnValue соседнего теста: без этой
    // строки «звука нет» шёл бы с упавшим запросом и ничего не проверял.
    vi.mocked(audioApi.forWork).mockReturnValue(
      Promise.resolve({ data: { tracks: [], recordings: [] } }) as never,
    );
  });

  // Review Focus 5.
  it('упал запрос звука — том без раздела «Аудио» и без ошибки', async () => {
    vi.mocked(audioApi.forWork).mockReturnValue(Promise.reject(new Error('500')));
    setup();
    await screen.findByRole('heading', { level: 1 });
    expect(screen.queryByRole('region', { name: 'Аудио' })).toBeNull();
    expect(screen.queryByText(/не удалось/i)).toBeNull();
  });

  it('звука нет, читатель — раздела нет', async () => {
    setup();
    await screen.findByRole('heading', { level: 1 });
    await waitFor(() => expect(audioApi.forWork).toHaveBeenCalled());
    expect(screen.queryByRole('region', { name: 'Аудио' })).toBeNull();
  });

  it('звук есть — раздел «Аудио» и плейлист в меню выгрузки', async () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    vi.mocked(audioApi.forWork).mockReturnValue(
      Promise.resolve({
        data: {
          tracks: [
            {
              id: 1,
              work_id: 1,
              title: 'Глава',
              start_page: 1,
              end_page: 2,
              duration_ms: 1000,
              bytes: 1,
              recipe_sha256: 'r',
              pages_sha256: 'p',
              stale: false,
              created_at: '',
              url: '/api/audio/1.opus',
            },
          ],
          recordings: [],
        },
      } as never),
    );
    setup();
    expect(await screen.findByRole('region', { name: 'Аудио' })).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /Скачать/ }));
    expect(screen.getByRole('menuitem', { name: /Аудио/ })).toBeInTheDocument();
  });
});
