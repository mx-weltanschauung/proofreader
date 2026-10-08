import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, act, cleanup, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import type { AudioQueueItem, Chapter, User, Work } from '../types';
import { audioApi, chaptersApi, worksApi, cacheApi } from '../services/api';
import { findChapterById } from '../utils/chapterTree';
import { shareFrom, stubShare, unstubShare } from '../test/shareStub';
import { RECENT_KEY, readRecent, storageKey } from '../hooks/useReadingProgress';
// useAuth — это и есть zustand-стор (create<AuthState>()), поэтому у него
// есть setState: роль в тестах подменяется прямо на нём, без vi.mock (тот же
// приём, что и в Dashboard.test.tsx).
import { useAuth } from '../hooks/useAuth';

// KaTeX autorender walks the real DOM and is irrelevant here; the mock also
// gives Task 2 a call counter.
const { renderMathMock } = vi.hoisted(() => ({ renderMathMock: vi.fn() }));
vi.mock('katex/dist/contrib/auto-render', () => ({ default: renderMathMock }));

// IntersectionObserver в тестовом окружении — заглушка (src/test/setup.ts),
// поэтому настоящий useVisiblePage всегда возвращает null, и задать видимую
// страницу иначе нельзя. Аргументы прокидываются в мок, а не отбрасываются:
// первый из них — единственное, чем ChapterView управляет наблюдением, и
// проверить его больше негде.
const { visiblePageMock } = vi.hoisted(() => ({
  visiblePageMock: vi.fn((_enabled: boolean, _sectionsKey: unknown) => null as number | null),
}));
vi.mock('../hooks/useVisiblePage', () => ({
  useVisiblePage: (enabled: boolean, sectionsKey: unknown) => visiblePageMock(enabled, sectionsKey),
}));

import { ChapterView } from './ChapterView';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';

// Axios responses carry status/headers/config; the component reads only .data.
function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

// Затвор для ответа, который надо подержать незавершённым: окно между сменой
// :chapterId и приходом новых страниц иначе не поймать — оно длится один тик.
function deferred() {
  let resolve!: () => void;
  const promise = new Promise<void>((r) => {
    resolve = r;
  });
  return { promise, resolve };
}

// page_offset: 0 — колонцифра совпадает с номером страницы, как у обычного
// тома; printedFolio теперь требует это поле, раз ChapterView его считает.
const WORK = { id: 3, title: 'Том 3', page_offset: 0 } as Work;

const CHAPTER = {
  id: 127,
  work_id: 3,
  title: 'Глава',
  type: 'chapter',
  start_page: 245,
  end_page: 246,
} as Chapter;

const PAGES = [
  { page_number: 245, html: '<p>Так писал он.</p>', blank: false },
  // A blank page in the scan: no markdown, so the renderer returns a document
  // with an empty body and the section collapses to nothing.
  { page_number: 246, html: '', blank: true },
  { page_number: 247, html: '<p>Наутро всё переменилось.</p>', blank: false },
];

function renderChapter() {
  return render(
    <MemoryRouter initialEntries={['/works/3/chapters/127']}>
      <ReadingPreferencesProvider>
        <Routes>
          <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
        </Routes>
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

describe('ChapterView page markers', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  // Маркер — якорь на начало полосы внутри читаемой главы, а не переход на
  // отдельный экран полосы: он и есть та ссылка, которой читатель делится,
  // и берётся она из контекстного меню браузера («копировать адрес ссылки»).
  // В атрибуте стоит голый фрагмент: до полного адреса главы его разворачивает
  // сам браузер — и в строке состояния, и в скопированном адресе.
  it('renders one marker per page, anchoring to that page', async () => {
    renderChapter();
    const marker = await screen.findByRole('link', { name: 'Страница 245' });
    expect(marker).toHaveAttribute('href', '#chapter-page-245');
    expect(marker).toHaveTextContent('245');
    expect(screen.getByRole('link', { name: 'Страница 247' })).toHaveAttribute(
      'href',
      '#chapter-page-247',
    );
  });

  // A blank page renders nothing, so its section is a self-collapsing box:
  // zero height, margins collapsed away, top edge coinciding with the next
  // section's. A marker on it would sit exactly on top of the next page's
  // number. Dropping it keeps the layout untouched at the cost of a gap in
  // the numbering.
  // Проводка, а не отрисовка: видимую полосу знает читалка, а номер в
  // панели — он же переход к другой — получает её колонцифру. Доли цифрой у
  // главы больше нет: её показывает полоса прогресса.
  it('называет в панели видимую страницу', async () => {
    visiblePageMock.mockReturnValue(247);
    renderChapter();
    expect(
      await screen.findByRole('button', { name: 'Перейти к странице (сейчас 247)' }),
    ).toHaveTextContent('с. 247');
    expect(screen.queryByText('100 %')).toBeNull();
    visiblePageMock.mockReturnValue(null);
  });

  it('renders no marker for a page with no content', async () => {
    renderChapter();
    await screen.findByRole('link', { name: 'Страница 245' });
    expect(screen.queryByRole('link', { name: 'Страница 246' })).toBeNull();
  });

  // Visibility itself is CSS-only (jsdom applies no stylesheets), so the
  // behavioural contract under test is the class on the container. That the
  // markers are actually hidden when the class is absent is checked by hand
  // in the browser.
  it('toggles show-page-info on the content container', async () => {
    const { container } = renderChapter();
    await screen.findByRole('link', { name: 'Страница 245' });
    const content = container.querySelector('.chapter-pages-content');
    expect(content).not.toBeNull();
    // Номера показаны с самого начала: настройка чтения, умолчание — «да».
    expect(content).toHaveClass('show-page-info');

    const button = screen.getByRole('button', { name: 'Номера страниц' });
    await userEvent.click(button);
    expect(content).not.toHaveClass('show-page-info');

    await userEvent.click(button);
    expect(content).toHaveClass('show-page-info');
  });

  // Toggling the markers is a CSS class flip, not a content change. Leaving
  // showPageInfo in these deps re-runs KaTeX over the whole chapter and
  // rewires the footnote handlers on every click.
  it('does not re-run KaTeX when the markers are toggled', async () => {
    renderChapter();
    await screen.findByRole('link', { name: 'Страница 245' });
    const callsBefore = renderMathMock.mock.calls.length;
    // Guards against this test passing vacuously if the maths effect stopped
    // running altogether.
    expect(callsBefore).toBeGreaterThan(0);

    await userEvent.click(screen.getByRole('button', { name: 'Номера страниц' }));

    expect(renderMathMock.mock.calls.length).toBe(callsBefore);
  });
});

// --- Запись «где читатель остановился» ----------------------------------
// Единственная связка между чтением главы и главной страницей — запись в
// localStorage, и её содержимое не проверял ни один тест. Аргументы
// workTitle и chapterTitle стоят в вызове хука рядом и однотипны: поменяв
// их местами, можно было получить зелёный прогон и полку, на которой в
// предложении «продолжить» стоит «Том 3» вместо названия главы.

describe('ChapterView: запись о последнем прочитанном', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  // Порог в минуту стережёт хук (useReadingProgress.test.ts); здесь —
  // проводка: что именно страница кладёт в строку. Глава уже в списке,
  // поэтому строка поднимается сразу, без минуты ожидания.
  it('кладёт в строку открытый том, главу и видимую страницу', async () => {
    localStorage.setItem(
      RECENT_KEY,
      JSON.stringify([
        {
          workId: 9,
          chapterId: 1,
          workTitle: 'Том 9',
          chapterTitle: 'Другая',
          pageNumber: 1,
          ts: 2,
        },
        {
          workId: 3,
          chapterId: 127,
          workTitle: 'Том 3',
          chapterTitle: 'Глава',
          pageNumber: 1,
          ts: 1,
        },
      ]),
    );
    // По умолчанию мок useVisiblePage отдаёт null (IntersectionObserver в
    // jsdom — заглушка), а нам нужен номер, чтобы проверить и его.
    visiblePageMock.mockReturnValue(246);
    try {
      renderChapter();
      await screen.findByRole('link', { name: 'Страница 245' });
      await waitFor(() => expect(readRecent()[0]?.pageNumber).toBe(246));

      expect(readRecent()[0]).toMatchObject({
        workId: 3,
        chapterId: 127,
        workTitle: 'Том 3',
        chapterTitle: 'Глава',
        pageNumber: 246,
      });
    } finally {
      visiblePageMock.mockReturnValue(null);
    }
  });

  it('новую главу сразу в список не заводит', async () => {
    renderChapter();
    await screen.findByRole('link', { name: 'Страница 245' });
    expect(readRecent()).toEqual([]);
  });
});

// --- Навигация по главам -----------------------------------------------
// Фикстура повторяет форму тома 4: работы верхнего уровня, у одной из них —
// подглавы. Именно на ней проявлялась исходная ошибка, когда «следующей»
// главой оказывался первый ребёнок открытой.

const WORK4 = { id: 4, title: 'Том 4', page_offset: 0 } as Work;

function navChapter(
  id: number,
  title: string,
  start: number,
  end: number,
  children?: Chapter[],
): Chapter {
  return {
    id,
    work_id: 4,
    title,
    type: 'chapter',
    order_number: id,
    start_page: start,
    end_page: end,
    children,
  } as Chapter;
}

const TREE4: Chapter[] = [
  navChapter(229, 'Ф. Энгельс. Протекционизм', 61, 64),
  navChapter(230, 'К. Маркс. Нищета философии', 65, 185, [
    navChapter(281, 'ПРЕДИСЛОВИЕ', 69, 70),
    navChapter(282, 'Глава первая. НАУЧНОЕ ОТКРЫТИЕ', 71, 127),
    navChapter(283, 'Глава вторая. МЕТАФИЗИКА', 128, 185),
  ]),
  navChapter(231, 'Ф. Энгельс. Закат Гизо', 186, 193),
];

const PAGES4 = [
  { page_number: 69, html: '<p>Предисловие.</p>', blank: false },
  { page_number: 71, html: '<p>Открытие.</p>', blank: false },
  { page_number: 128, html: '<p>Метафизика.</p>', blank: false },
];

function renderNav(chapterId: number) {
  return render(
    <MemoryRouter initialEntries={[`/works/4/chapters/${chapterId}`]}>
      <ReadingPreferencesProvider>
        <Routes>
          <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
        </Routes>
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

// Показывает текущий адрес: так проверяется переход, сделанный не ссылкой,
// а вызовом navigate.
function LocationProbe() {
  return <div data-testid="location">{useLocation().pathname}</div>;
}

function mockVolume4() {
  vi.clearAllMocks();
  vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK4));
  vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(TREE4));
  vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
    ok({ pages: PAGES4, footnotes_html: '' }),
  );
  vi.spyOn(chaptersApi, 'get').mockImplementation((_workId: number, id: number) =>
    ok(findChapterById(TREE4, id) as Chapter),
  );
}

describe('ChapterView chapter navigation', () => {
  beforeEach(mockVolume4);

  // Блоки навигации рендерятся дважды — над текстом и под ним, — поэтому
  // findAllByRole, а не findByRole: последний упал бы на двух совпадениях.
  it('ведёт правой стрелкой на сестру, а не на первого ребёнка', async () => {
    renderNav(230);
    const next = await screen.findAllByRole('link', { name: /Закат Гизо/ });
    expect(next[0]).toHaveAttribute('href', '/works/4/chapters/231');
    expect(screen.queryByRole('link', { name: /ПРЕДИСЛОВИЕ/ })).toBeNull();
  });

  it('ведёт левой стрелкой на предыдущую сестру', async () => {
    renderNav(230);
    const prev = await screen.findAllByRole('link', { name: /Протекционизм/ });
    expect(prev[0]).toHaveAttribute('href', '/works/4/chapters/229');
  });

  it('внутри главы переходит между подглавами', async () => {
    renderNav(281);
    const next = await screen.findAllByRole('link', { name: /НАУЧНОЕ ОТКРЫТИЕ/ });
    expect(next[0]).toHaveAttribute('href', '/works/4/chapters/282');
  });

  it('не показывает левую стрелку у первой подглавы', async () => {
    renderNav(281);
    await screen.findAllByRole('link', { name: /НАУЧНОЕ ОТКРЫТИЕ/ });
    const backwards = screen.queryAllByRole('link', { name: /^← / });
    expect(backwards).toHaveLength(0);
  });

  it('не показывает правую стрелку у последней подглавы', async () => {
    renderNav(283);
    await screen.findAllByRole('link', { name: /НАУЧНОЕ ОТКРЫТИЕ/ });
    const forwards = screen.queryAllByRole('link', { name: / →$/ });
    expect(forwards).toHaveLength(0);
  });

  it('показывает в шапке цепочку предков и ссылку на том', async () => {
    renderNav(281);
    const work = await screen.findByRole('link', { name: 'Том 4' });
    expect(work).toHaveAttribute('href', '/works/4');
    expect(screen.getByRole('link', { name: 'К. Маркс. Нищета философии' })).toHaveAttribute(
      'href',
      '/works/4/chapters/230',
    );
  });

  it('у главы верхнего уровня показывает только ссылку на том', async () => {
    renderNav(230);
    const work = await screen.findByRole('link', { name: 'Том 4' });
    expect(work).toHaveAttribute('href', '/works/4');
    // Сама открытая глава стоит заголовком, дублировать её в цепочке нечем.
    expect(screen.queryByRole('link', { name: 'К. Маркс. Нищета философии' })).toBeNull();
  });

  it('прокручивает к подглаве и закрывает шторку', async () => {
    renderNav(230);
    await screen.findByRole('button', { name: 'Оглавление главы' });
    await userEvent.click(screen.getByRole('button', { name: 'Оглавление главы' }));

    // jsdom не умеет scrollIntoView — подменяем именно на целевой секции.
    const target = document.getElementById('chapter-page-71');
    expect(target).not.toBeNull();
    const scrolled = vi.fn();
    target!.scrollIntoView = scrolled;

    await userEvent.click(screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ }));

    expect(scrolled).toHaveBeenCalledTimes(1);
    expect(screen.queryByRole('dialog')).not.toBeInTheDocument();
  });

  it('не показывает кнопку оглавления у главы без подглав', async () => {
    renderNav(229);
    await screen.findAllByRole('link', { name: /Нищета философии/ });
    expect(screen.queryByRole('button', { name: 'Оглавление главы' })).toBeNull();
  });

  it('не показывает оглавление всего тома', async () => {
    renderNav(230);
    await screen.findByRole('button', { name: 'Оглавление главы' });
    expect(screen.queryByRole('button', { name: 'Expand All' })).toBeNull();
  });

  it('переходит на адрес подглавы, если якоря на странице нет', async () => {
    // Подглава с диапазоном вне загруженных страниц: секции chapter-page-900
    // на странице нет, прокручивать некуда.
    const broken: Chapter[] = [
      navChapter(230, 'К. Маркс. Нищета философии', 65, 185, [
        navChapter(284, 'Приложение', 900, 901),
      ]),
    ];
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(broken));
    vi.spyOn(chaptersApi, 'get').mockImplementation((_workId: number, id: number) =>
      ok(findChapterById(broken, id) as Chapter),
    );

    render(
      <MemoryRouter initialEntries={['/works/4/chapters/230']}>
        <ReadingPreferencesProvider>
          <LocationProbe />
          <Routes>
            <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    await userEvent.click(await screen.findByRole('button', { name: 'Оглавление главы' }));
    await userEvent.click(screen.getByRole('link', { name: /^Приложение/ }));

    expect(screen.getByTestId('location')).toHaveTextContent('/works/4/chapters/284');
  });

  // Фокус должен уехать на прокрученную секцию: панель размонтируется вместе
  // с кнопкой-переключателем, на которой стоял фокус, иначе клавиатурный
  // пользователь теряет позицию, а скринридер не объявляет переход.
  it('переносит фокус на секцию подглавы после прыжка', async () => {
    renderNav(230);
    await userEvent.click(await screen.findByRole('button', { name: 'Оглавление главы' }));

    // jsdom не умеет scrollIntoView — подменяем именно на целевой секции,
    // как и в соседнем тесте прокрутки.
    const target = document.getElementById('chapter-page-71');
    expect(target).not.toBeNull();
    target!.scrollIntoView = vi.fn();

    await userEvent.click(screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ }));

    expect(target).toHaveFocus();
  });

  // Строка оглавления — настоящая ссылка на начало подглавы: читатель берёт
  // её из контекстного меню, чтобы сослаться на то, что читает сейчас.
  it('несёт в строке оглавления якорь на начало подглавы', async () => {
    renderNav(230);
    await userEvent.click(await screen.findByRole('button', { name: 'Оглавление главы' }));

    expect(screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ })).toHaveAttribute(
      'href',
      '#chapter-page-71',
    );
  });

  it('отмечает прыжок к подглаве в адресе, не заводя записи в истории', async () => {
    window.history.replaceState(null, '', '/works/4/chapters/230');
    renderNav(230);
    await userEvent.click(await screen.findByRole('button', { name: 'Оглавление главы' }));
    document.getElementById('chapter-page-71')!.scrollIntoView = vi.fn();
    const lengthBefore = window.history.length;

    await userEvent.click(screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ }));

    expect(window.location.hash).toBe('#chapter-page-71');
    expect(window.history.length).toBe(lengthBefore);
  });

  it('помечает в шторке подглаву, чей текст на экране', async () => {
    // useVisiblePage замокан на уровне модуля (см. шапку файла) — сообщаем
    // ему видимую страницу напрямую, а не через IntersectionObserver.
    visiblePageMock.mockReturnValue(71);
    renderNav(230);
    await screen.findByRole('button', { name: 'Оглавление главы' });

    await userEvent.click(screen.getByRole('button', { name: 'Оглавление главы' }));
    expect(screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ })).toHaveAttribute(
      'aria-current',
      'true',
    );
  });

  it('не помечает ничего, когда видимая страница вне подглав', async () => {
    // Страницы 65-68 главы 230 идут до «ПРЕДИСЛОВИЯ» (69-70) и не попадают ни
    // в одну подглаву. visiblePage теперь приходит из мока напрямую, поэтому
    // реальная страница 65 в ответе listPages не нужна — раньше её добавляли
    // как DOM-цель для удалённого installObserverMock.
    visiblePageMock.mockReturnValue(65);

    renderNav(230);
    await screen.findByRole('button', { name: 'Оглавление главы' });

    await userEvent.click(screen.getByRole('button', { name: 'Оглавление главы' }));
    const panel = screen.getByRole('dialog', { name: 'Оглавление главы' });
    expect(panel.querySelector('[aria-current]')).toBeNull();
  });

  // levels[levels.length - 1] должен брать САМЫЙ глубокий уровень, а не
  // просто первый: на плоском TREE4 (дети без своих детей) эта разница не
  // проявляется, обе реализации дают один результат. Здесь у подглавы есть
  // собственная вложенная подглава — том 3 доходит так до одиннадцати уровней.
  it('подсвечивает самую глубокую подглаву, а не её родителя', async () => {
    const nested: Chapter[] = [
      navChapter(230, 'К. Маркс. Нищета философии', 65, 185, [
        navChapter(282, 'Глава первая. НАУЧНОЕ ОТКРЫТИЕ', 71, 127, [
          navChapter(293, '§ 1. Противоположность', 71, 90),
        ]),
      ]),
    ];
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(nested));
    vi.spyOn(chaptersApi, 'get').mockImplementation((_workId: number, id: number) =>
      ok(findChapterById(nested, id) as Chapter),
    );
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({
        pages: [{ page_number: 71, html: '<p>Открытие.</p>', blank: false }],
        footnotes_html: '',
      }),
    );

    visiblePageMock.mockReturnValue(71);
    renderNav(230);
    await screen.findByRole('button', { name: 'Оглавление главы' });

    await userEvent.click(screen.getByRole('button', { name: 'Оглавление главы' }));

    expect(screen.getByRole('link', { name: /^§ 1\. Противоположность/ })).toHaveAttribute(
      'aria-current',
      'true',
    );
    expect(
      screen.getByRole('link', { name: /^Глава первая\. НАУЧНОЕ ОТКРЫТИЕ/ }),
    ).not.toHaveAttribute('aria-current');
  });
});

// --- Свёртка длинной цепочки предков ------------------------------------
// Форма тома 3: одиннадцать уровней вложенности. Здесь — шесть, этого
// достаточно, чтобы получить пять предков и перейти порог свёртки
// ChapterBreadcrumb (>4 уровней).

const DEEP_LEAF = navChapter(506, 'Уровень 6', 620, 621);
const DEEP_TREE: Chapter[] = [
  navChapter(501, 'Уровень 1', 570, 621, [
    navChapter(502, 'Уровень 2', 580, 621, [
      navChapter(503, 'Уровень 3', 590, 621, [
        navChapter(504, 'Уровень 4', 600, 621, [
          navChapter(505, 'Уровень 5', 610, 621, [DEEP_LEAF]),
        ]),
      ]),
    ]),
  ]),
];

const PAGES_DEEP = [{ page_number: 620, html: '<p>Текст.</p>', blank: false }];

function mockDeepVolume() {
  vi.clearAllMocks();
  // clearAllMocks не трогает implementation — сбрасываем явно, иначе значение,
  // оставленное последним тестом подсветки подглав, утекло бы сюда.
  visiblePageMock.mockReturnValue(null);
  vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK4));
  vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(DEEP_TREE));
  vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
    ok({ pages: PAGES_DEEP, footnotes_html: '' }),
  );
  vi.spyOn(chaptersApi, 'get').mockImplementation((_workId: number, id: number) =>
    ok(findChapterById(DEEP_TREE, id) as Chapter),
  );
}

describe('ChapterView свёртка цепочки предков', () => {
  beforeEach(mockDeepVolume);

  it('не схлопывает раскрытую цепочку при перерисовке родителя', async () => {
    renderNav(506);
    await screen.findByRole('heading', { name: 'Уровень 6' });

    const expandButton = await screen.findByRole('button', { name: /Показать пропущенные уровни/ });
    await userEvent.click(expandButton);
    // Раскрылось — кнопки-многоточия больше нет, виден и спрятанный уровень.
    expect(screen.queryByRole('button', { name: /Показать пропущенные уровни/ })).toBeNull();
    expect(screen.getByRole('link', { name: 'Уровень 3' })).toBeInTheDocument();

    // Перерисовка родителя без изменения предков (скролл в реальности,
    // здесь — любой сеттер состояния) не должна вернуть свёртку.
    await userEvent.click(screen.getByRole('button', { name: 'Номера страниц' }));

    expect(screen.queryByRole('button', { name: /Показать пропущенные уровни/ })).toBeNull();
    expect(screen.getByRole('link', { name: 'Уровень 3' })).toBeInTheDocument();
  });
});

import { ScrollDock } from '../components/ScrollDock';
import { ScrollDockProvider } from '../contexts/ScrollDockProvider';

// Подглавы приходят из дерева тома: ChapterView берёт детей открытой главы
// через findChapterById по ответу chaptersApi.list. SUB_ONE — длиной ровно в
// одну страницу: в загруженных томах такова каждая седьмая листовая подглава
// (в томе 6 — больше половины), и постраничное сравнение «видимая страница
// дальше начала» для неё невыполнимо в принципе. Условие показа геометрическое,
// поэтому фикстура держит и этот случай.
const SUB_ONE = {
  id: 199,
  work_id: 3,
  title: 'Глава первая. НАУЧНОЕ ОТКРЫТИЕ',
  type: 'chapter',
  start_page: 245,
  end_page: 245,
} as Chapter;

const SUB_TWO = {
  id: 200,
  work_id: 3,
  title: 'Глава вторая. МЕТАФИЗИКА',
  type: 'chapter',
  start_page: 247,
  end_page: 248,
} as Chapter;

const PAGES_SUB = [...PAGES, { page_number: 248, html: '<p>Конец главы.</p>', blank: false }];

const TREE = [{ ...CHAPTER, end_page: 248, children: [SUB_ONE, SUB_TWO] } as Chapter];

// Соседняя глава верхнего уровня — со своей подглавой, чтобы тест перехода
// без размонтирования проверял не просто исчезновение кнопки, а то, что она
// показывает подглаву именно новой главы, а не старой. Диапазон Б-1
// ПЕРЕСЕКАЕТСЯ с главой А намеренно, и начинается он со страницы 247, которая
// на экране уже есть: пока грузится новая глава, на экране остаются страницы
// А, а visiblePage отстаёт и показывает всё ту же 248. На непересекающихся
// диапазонах проверка прошла бы и вовсе без защиты — подглаву новой главы
// было бы не на чем показать.
const SUB_B = {
  id: 201,
  work_id: 3,
  title: 'Подглава Б-1',
  type: 'chapter',
  start_page: 247,
  end_page: 250,
} as Chapter;

const CHAPTER_B = {
  id: 130,
  work_id: 3,
  title: 'Глава Б',
  type: 'chapter',
  start_page: 247,
  end_page: 250,
  children: [SUB_B],
} as Chapter;

const TREE_WITH_SIBLING = [TREE[0], CHAPTER_B];

const PAGES_B = [247, 248, 250].map((n) => ({
  page_number: n,
  html: `<p>Страница ${n}.</p>`,
  blank: false,
}));

describe('кнопка «в начало подглавы»', () => {
  // Кнопку рисует ScrollDock из Layout, а ChapterView только регистрирует
  // действие — поэтому здесь собирается та же пара, что в приложении.
  function renderWithDock() {
    return render(
      <ScrollDockProvider>
        <MemoryRouter initialEntries={['/works/3/chapters/127']}>
          <ReadingPreferencesProvider>
            <Routes>
              <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
            </Routes>
          </ReadingPreferencesProvider>
        </MemoryRouter>
        <ScrollDock />
      </ScrollDockProvider>,
    );
  }

  function scrollPastThreshold() {
    act(() => {
      Object.defineProperty(window, 'scrollY', { value: 1300, writable: true, configurable: true });
      window.dispatchEvent(new Event('scroll'));
    });
  }

  // Условие показа геометрическое: верх секции сравнивается с нижним краем
  // липкой шапки главы. В jsdom нет вёрстки — все прямоугольники нулевые,
  // поэтому кромка приходится на y=0, а положение секции задаётся вручную.
  // Отрицательный top — начало подглавы ушло за кромку, положительный — оно
  // ещё на экране.
  function placeSection(pageNumber: number, top: number) {
    const section = document.getElementById(`chapter-page-${pageNumber}`);
    expect(section, `нет секции страницы ${pageNumber}`).not.toBeNull();
    section!.getBoundingClientRect = () =>
      ({ top, bottom: top + 400, height: 400, y: top }) as DOMRect;
  }

  beforeEach(() => {
    vi.clearAllMocks();
    window.scrollTo = vi.fn();
    Object.defineProperty(window, 'innerHeight', { value: 800, configurable: true });
    Object.defineProperty(window, 'scrollY', { value: 0, writable: true, configurable: true });
    // jsdom реализует requestAnimationFrame через реальный ~16-мс таймер, а не
    // синхронно и не микротаском — без этой замены обновление видимости
    // ScrollDock не успевает произойти внутри act() до проверок ниже
    // (см. тот же приём в ScrollDock.test.tsx).
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    }) as typeof window.requestAnimationFrame;
    window.cancelAnimationFrame = vi.fn();
    visiblePageMock.mockReturnValue(null);
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES_SUB, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(TREE));
  });

  // Глава без подглав: кнопка дублировала бы «Наверх», и её быть не должно.
  it('не появляется у главы без подглав', async () => {
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
    visiblePageMock.mockReturnValue(248);
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    scrollPastThreshold();
    expect(screen.getByRole('button', { name: 'Наверх' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /В начало подглавы/ })).not.toBeInTheDocument();
  });

  // Читатель на первой странице подглавы — прыгать некуда, а кнопка сдвинула
  // бы текст на пару строк и сбила бы чтение.
  it('не появляется, пока начало подглавы на экране', async () => {
    visiblePageMock.mockReturnValue(247);
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    placeSection(247, 300);
    scrollPastThreshold();
    expect(screen.queryByRole('link', { name: /В начало подглавы/ })).not.toBeInTheDocument();
  });

  it('появляется с названием подглавы, когда её начало ушло выше', async () => {
    visiblePageMock.mockReturnValue(248);
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    placeSection(247, -100);
    scrollPastThreshold();
    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Глава вторая. МЕТАФИЗИКА' }),
    ).toBeInTheDocument();
  });

  // Ради этого случая условие и переписано с постраничного на геометрическое.
  // Подглава в одну страницу — 14% листовых подглав в загруженных томах: её
  // начало и её конец это одна и та же страница, поэтому «видимая страница
  // дальше начала» не наступает никогда, а начало при этом преспокойно уходит
  // за кромку экрана.
  it('появляется у подглавы длиной в одну страницу', async () => {
    visiblePageMock.mockReturnValue(SUB_ONE.start_page);
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    placeSection(SUB_ONE.start_page, -600);
    scrollPastThreshold();
    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Глава первая. НАУЧНОЕ ОТКРЫТИЕ' }),
    ).toBeInTheDocument();
  });

  // Док показывает подглаву, которую читатель читает сейчас, — то самое
  // место, откуда естественно взять на неё ссылку. Поэтому он не кнопка, а
  // настоящая ссылка с якорем: адрес берётся из контекстного меню браузера.
  it('несёт адрес читаемой подглавы, чтобы на неё можно было сослаться', async () => {
    visiblePageMock.mockReturnValue(248);
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    placeSection(247, -100);
    scrollPastThreshold();

    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Глава вторая. МЕТАФИЗИКА' }),
    ).toHaveAttribute('href', '#chapter-page-247');
  });

  it('по нажатию уводит к началу подглавы, а не главы', async () => {
    visiblePageMock.mockReturnValue(248);
    renderWithDock();
    const anchor = await screen.findByRole('link', { name: 'Страница 245' });
    const section = anchor.closest('.chapter-page-section')!;
    const target = document.getElementById('chapter-page-247')!;
    const scrollIntoView = vi.fn();
    target.scrollIntoView = scrollIntoView;
    expect(section).toBeTruthy();

    placeSection(247, -100);
    scrollPastThreshold();
    await userEvent.click(
      screen.getByRole('link', { name: 'В начало подглавы: Глава вторая. МЕТАФИЗИКА' }),
    );
    expect(scrollIntoView).toHaveBeenCalled();
  });

  // Ради этой смены весь блок и задуман: подпись на кнопке — не статичный
  // текст, а текущее место читателя. Проверяем и обратное: старая подпись не
  // задерживается на экране.
  it('меняет подпись при переходе из одной подглавы в другую', async () => {
    visiblePageMock.mockReturnValue(245); // в SUB_ONE
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    placeSection(245, -600);
    placeSection(247, -100);
    scrollPastThreshold();
    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Глава первая. НАУЧНОЕ ОТКРЫТИЕ' }),
    ).toBeInTheDocument();

    // visiblePage приходит из замоканного хука и не реактивен сам по себе —
    // толчок к перерисовке даёт клик по несвязанной кнопке (тот же приём,
    // что и в тесте на несворачиваемость цепочки предков).
    visiblePageMock.mockReturnValue(248); // в SUB_TWO, её начало уже за кромкой
    await userEvent.click(screen.getByRole('button', { name: 'Номера страниц' }));

    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Глава вторая. МЕТАФИЗИКА' }),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /НАУЧНОЕ ОТКРЫТИЕ/ })).not.toBeInTheDocument();
  });

  // ChapterView не размонтируется между главами — Route остаётся тем же,
  // меняется только :chapterId. Худший исход этой фичи: кнопка тащит за собой
  // название главы, которую читатель уже покинул, — или, того хуже, показывает
  // подглаву главы, в которую он только входит, пока на экране ещё старый
  // текст. Второе и воспроизводится здесь: ответы по главе Б держатся на
  // затворе, поэтому окно «дети уже от Б, страницы ещё от А» живёт ровно
  // столько, сколько нужно проверке. visiblePage при этом не двигается —
  // в жизни он тоже отстаёт: наблюдатель заговорит только по новым секциям.
  it('молчит, пока грузится новая глава', async () => {
    const gate = deferred();
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(TREE_WITH_SIBLING));
    vi.spyOn(chaptersApi, 'get').mockImplementation(async (_workId: number, id: number) => {
      if (id === CHAPTER_B.id) await gate.promise;
      return ok(id === CHAPTER_B.id ? CHAPTER_B : CHAPTER);
    });
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(
      async (_workId: number, chapterId: number) => {
        if (chapterId === CHAPTER_B.id) await gate.promise;
        return ok({
          pages: chapterId === CHAPTER_B.id ? PAGES_B : PAGES_SUB,
          footnotes_html: '',
        });
      },
    );
    visiblePageMock.mockReturnValue(248); // в SUB_TWO, её начало уже за кромкой

    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    placeSection(247, -100);
    scrollPastThreshold();
    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Глава вторая. МЕТАФИЗИКА' }),
    ).toBeInTheDocument();

    // Переход по ссылке «следующая глава» — тот же компонент, другой :chapterId.
    const nextLinks = await screen.findAllByRole('link', { name: /Глава Б/ });
    await userEvent.click(nextLinks[0]);

    // Окно загрузки: на экране ещё страницы главы А, а подглавы уже от Б, и
    // Б-1 накрывает страницу 248 — без защиты подпись сменилась бы на неё.
    expect(screen.getByRole('link', { name: 'Страница 245' })).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: /В начало подглавы/ })).not.toBeInTheDocument();

    await act(async () => {
      gate.resolve();
      await gate.promise;
    });
    await screen.findByRole('heading', { name: 'Глава Б' });
    placeSection(SUB_B.start_page, -600);
    scrollPastThreshold();
    expect(
      screen.getByRole('link', { name: 'В начало подглавы: Подглава Б-1' }),
    ).toBeInTheDocument();
    expect(screen.queryByRole('button', { name: /МЕТАФИЗИКА/ })).not.toBeInTheDocument();
  });

  // useVisiblePage замокан на уровне модуля, поэтому его первый аргумент не
  // проверяется больше нигде: заменив его на константу false, можно было
  // получить зелёный прогон и мёртвую фичу в продакшене. Наблюдение включено
  // безусловно (Задача 5) — видимая страница нужна не только шторке подглав,
  // но и записи «где читатель остановился», а без неё писать нечего.
  // Проверяем и главу с подглавами, и без — включение не должно зависеть от
  // их наличия.
  it('включает наблюдение за видимой страницей независимо от наличия подглав', async () => {
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    expect(visiblePageMock.mock.calls.some(([enabled]) => enabled === true)).toBe(true);
    expect(visiblePageMock.mock.calls.every(([enabled]) => enabled === true)).toBe(true);

    cleanup();
    visiblePageMock.mockClear();
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
    renderWithDock();
    await screen.findByRole('link', { name: 'Страница 245' });
    expect(visiblePageMock).toHaveBeenCalled();
    expect(visiblePageMock.mock.calls.every(([enabled]) => enabled === true)).toBe(true);
  });
});

describe('панель главы: кнопки одного вида', () => {
  // mockVolume4() тут не подходит: он держит дерево тома 4 (TREE4, главы
  // 229-283), а renderChapter() открывает /works/3/chapters/127 — тот же
  // набор моков, что и в 'ChapterView page markers'.
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  it('оба тумблера — .btn с объявленным состоянием', async () => {
    renderChapter();

    const pages = await screen.findByRole('button', { name: 'Номера страниц' });
    const immersive = screen.getByRole('button', { name: 'Режим чтения' });

    for (const button of [pages, immersive]) {
      expect(button).toHaveClass('btn');
      expect(button).toHaveClass('btn-secondary');
    }
    // Номера показаны по умолчанию, режим чтения — нет; объявлено обоими.
    expect(pages).toHaveAttribute('aria-pressed', 'true');
    expect(immersive).toHaveAttribute('aria-pressed', 'false');

    await userEvent.click(pages);
    expect(pages).toHaveAttribute('aria-pressed', 'false');
  });
});

// --- Где открывается соседняя глава --------------------------------------
// ChapterView не размонтируется между главами: Route тот же, меняется только
// :chapterId. Прокрутку окна при этом не сбрасывает никто — ни браузер (это
// pushState, а не загрузка документа), ни маршрутизатор. Читатель, дочитавший
// главу до конца и нажавший «следующая», получал новую главу открытой на том
// же смещении: если она короче прежней, браузер прижимал смещение к её низу —
// то есть глава открывалась своим концом.

describe('где открывается соседняя глава', () => {
  function renderAt(entry: string) {
    return render(
      <MemoryRouter initialEntries={[entry]}>
        <ReadingPreferencesProvider>
          <Routes>
            <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );
  }

  // Ответы по главе Б держатся на затворе: окно «адрес уже от Б, текст ещё от
  // А» иначе живёт один тик, а именно в нём и происходит всё интересное.
  function gateChapterB() {
    const gate = deferred();
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(TREE_WITH_SIBLING));
    vi.spyOn(chaptersApi, 'get').mockImplementation(async (_workId: number, id: number) => {
      if (id === CHAPTER_B.id) await gate.promise;
      return ok(id === CHAPTER_B.id ? CHAPTER_B : CHAPTER);
    });
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(
      async (_workId: number, chapterId: number) => {
        if (chapterId === CHAPTER_B.id) await gate.promise;
        return ok({
          pages: chapterId === CHAPTER_B.id ? PAGES_B : PAGES_SUB,
          footnotes_html: '',
        });
      },
    );
    return gate;
  }

  async function openChapterB() {
    const nextLinks = await screen.findAllByRole('link', { name: /Глава Б/ });
    await userEvent.click(nextLinks[0]);
  }

  async function letChapterBArrive(gate: ReturnType<typeof deferred>) {
    await act(async () => {
      gate.resolve();
      await gate.promise;
    });
    await screen.findByRole('heading', { name: 'Глава Б' });
  }

  beforeEach(() => {
    vi.clearAllMocks();
    localStorage.clear();
    window.scrollTo = vi.fn();
    // Читатель дочитал главу А до конца — смещение окна велико. jsdom вёрстки
    // не считает, поэтому положение задаётся руками.
    Object.defineProperty(document.documentElement, 'scrollTop', {
      value: 5000,
      writable: true,
      configurable: true,
    });
    // jsdom исполняет requestAnimationFrame настоящим ~16-мс таймером: без
    // замены запись позиции не успевает случиться внутри act().
    window.requestAnimationFrame = ((cb: FrameRequestCallback) => {
      cb(0);
      return 0;
    }) as typeof window.requestAnimationFrame;
    window.cancelAnimationFrame = vi.fn();
    visiblePageMock.mockReturnValue(null);
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES_SUB, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok(TREE_WITH_SIBLING));
  });

  it('открывает главу, которую читатель ещё не открывал, сверху', async () => {
    const gate = gateChapterB();
    renderAt('/works/3/chapters/127');
    await screen.findByRole('link', { name: 'Страница 245' });
    vi.mocked(window.scrollTo).mockClear();

    await openChapterB();
    await letChapterBArrive(gate);

    expect(window.scrollTo).toHaveBeenCalledWith(0, 0);
  });

  // Позиция пишется по ключу главы. Пока на экране текст прежней главы, её
  // смещение под ключом новой — это ложь, которая переживёт перезагрузку:
  // в следующий раз глава Б «восстановится» на конце главы А.
  it('не записывает смещение прежней главы под ключ новой', async () => {
    // Затвор нужен ради самого окна загрузки, а не ради его снятия: проверка
    // идёт внутри окна, глава Б до конца так и не приезжает.
    gateChapterB();
    renderAt('/works/3/chapters/127');
    await screen.findByRole('link', { name: 'Страница 245' });

    await openChapterB();

    expect(localStorage.getItem(storageKey(3, CHAPTER_B.id))).toBeNull();
  });

  // Обратная половина: место, где читатель бросил главу Б в прошлый раз,
  // восстанавливается — но тогда, когда текст Б уже на экране. Прокрутка по
  // ещё не сменившемуся тексту прежней главы упирается в её высоту и была бы
  // прижата браузером к её низу.
  it('восстанавливает сохранённую позицию, когда текст новой главы уже на экране', async () => {
    localStorage.setItem(storageKey(3, CHAPTER_B.id), '1234');
    const gate = gateChapterB();
    renderAt('/works/3/chapters/127');
    await screen.findByRole('link', { name: 'Страница 245' });
    vi.mocked(window.scrollTo).mockClear();

    await openChapterB();
    expect(window.scrollTo).not.toHaveBeenCalled();

    await letChapterBArrive(gate);
    expect(window.scrollTo).toHaveBeenCalledWith(0, 1234);
  });

  // Сторож для входящих ссылок: у адреса с якорем прокруткой распоряжается
  // useHashAnchor, и сброс наверх увёл бы читателя с той самой полосы, ради
  // которой ссылку и прислали.
  it('не сбрасывает наверх адрес с якорем полосы', async () => {
    renderAt('/works/3/chapters/127#chapter-page-247');
    await screen.findByRole('link', { name: 'Страница 245' });

    expect(window.scrollTo).not.toHaveBeenCalled();
  });
});

describe('ChapterView: вход в потоковое чтение', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  it('ведёт на первую страницу главы', async () => {
    renderChapter();
    const link = await screen.findByRole('link', { name: 'Читать потоком' });
    expect(link).toHaveAttribute('href', '/works/3/read/245');
  });
});

// --- Сброс кэша главы ----------------------------------------------------
// Кэш живёт час и путей инвалидации не имеет: кнопка — вторая половина этого
// размена, и её смысл проверяется здесь же, а не только фактом появления.
// useAuth сбрасывается в beforeEach каждого теста этого блока, поэтому роль
// не утекает ни в соседний тест здесь, ни в предыдущие describe (они вообще
// не трогают useAuth, и по умолчанию user: null).

describe('сброс кэша главы', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  it('не показывает кнопку читателю', async () => {
    renderChapter();

    await waitFor(() => expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument());
    expect(screen.queryByRole('button', { name: /Сбросить кэш/i })).not.toBeInTheDocument();
  });

  it('редактору показывает кнопку, а после нажатия перезапрашивает главу', async () => {
    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(cacheApi, 'purgeChapter').mockImplementation(() => ok({ removed: 1 }));

    renderChapter();
    await waitFor(() => expect(screen.getByRole('heading', { level: 1 })).toBeInTheDocument());

    const callsBefore = vi.mocked(chaptersApi.listPages).mock.calls.length;
    await userEvent.click(screen.getByRole('button', { name: /Сбросить кэш главы/i }));

    // WORK.id и CHAPTER.id — id из фикстуры этого файла (том 3, глава 127).
    await waitFor(() => expect(cacheApi.purgeChapter).toHaveBeenCalledWith(WORK.id, CHAPTER.id));
    // Без перезапроса кнопка выглядела бы сломанной: редактор видел бы тот же
    // старый текст из состояния компонента.
    await waitFor(() =>
      expect(vi.mocked(chaptersApi.listPages).mock.calls.length).toBeGreaterThan(callsBefore),
    );
  });
});

// --- Скачивание главы: адрес API строго числовой ------------------------
// Бэкенд не понимает слаг в /api/... (решение спеки, терпимость там не
// вводится) — а сырой сегмент маршрута (workParam) его несёт на каноническом
// адресе читальни. Раньше DownloadMenu собирался именно из workParam, и
// кнопка «скачать» отвечала 400 на любом адресе со слагом.
const WORK_SLUGGED = { id: 49, title: 'Том 49', page_offset: 0, slug: 'lenin-t06' } as Work;

const CHAPTER_SLUGGED = {
  id: 10125,
  work_id: 49,
  title: 'Глава',
  type: 'chapter',
  start_page: 1,
  end_page: 2,
} as Chapter;

describe('ChapterView: адрес скачивания главы', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER_SLUGGED));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK_SLUGGED));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({
        pages: [{ page_number: 1, html: '<p>Текст.</p>', blank: false }],
        footnotes_html: '',
      }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  it('строит адрес скачивания из числового id, а не из сегмента маршрута со слагом', async () => {
    render(
      <MemoryRouter initialEntries={['/works/49-lenin-t06/chapters/10125']}>
        <ReadingPreferencesProvider>
          <Routes>
            <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    await screen.findByRole('link', { name: 'Страница 1' });
    await userEvent.click(screen.getByRole('button', { name: 'Скачать' }));

    expect(screen.getByRole('menuitem', { name: /EPUB/ })).toHaveAttribute(
      'href',
      '/api/works/49/chapters/10125/download?format=epub',
    );
  });
});

describe('ChapterView: Поделиться', () => {
  afterEach(unstubShare);

  it('отдаёт главу целиком — канонический адрес без подсветки поиска и якоря полосы', async () => {
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() =>
      ok({ ...CHAPTER_SLUGGED, title: 'Что делать?', slug: 'chto-delat' }),
    );
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK_SLUGGED, title: 'В. И. Ленин. Полное собрание сочинений. Том 6' }),
    );
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({
        pages: [{ page_number: 1, html: '<p>Текст.</p>', blank: false }],
        footnotes_html: '',
      }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
    const share = stubShare();
    render(
      <MemoryRouter initialEntries={['/works/49/chapters/10125?q=партия#chapter-page-1']}>
        <ReadingPreferencesProvider>
          <Routes>
            <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );
    await screen.findByRole('link', { name: 'Страница 1' });
    const data = await shareFrom(share);
    expect(data.title).toBe('Что делать? — В. И. Ленин. Полное собрание сочинений. Том 6');
    expect(data.url).toBe(`${window.location.origin}/works/49-lenin-t06/chapters/10125-chto-delat`);
  });
});

describe('ChapterView: битый сегмент адреса', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
  });

  // Регрессия: эффект выходил на workId === null || chapterId === null, не
  // сбросив isLoading, — экран вечно висел на «Загрузка главы…» вместо уже
  // существующего экрана отказа (условие там уже проверяет workId === null /
  // chapterId === null).
  it('на битом id работы показывает отказ, а не вечную загрузку, и ничего не грузит', async () => {
    render(
      <MemoryRouter initialEntries={['/works/xyz/chapters/127']}>
        <ReadingPreferencesProvider>
          <Routes>
            <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Глава не найдена')).toBeInTheDocument();
    expect(screen.queryByText('Загрузка главы…')).not.toBeInTheDocument();
    expect(worksApi.get).not.toHaveBeenCalled();
    expect(chaptersApi.get).not.toHaveBeenCalled();
    expect(chaptersApi.listPages).not.toHaveBeenCalled();
    expect(chaptersApi.list).not.toHaveBeenCalled();
  });

  it('на битом id главы показывает отказ, а не вечную загрузку, и ничего не грузит', async () => {
    render(
      <MemoryRouter initialEntries={['/works/3/chapters/xyz']}>
        <ReadingPreferencesProvider>
          <Routes>
            <Route path="/works/:workId/chapters/:chapterId" element={<ChapterView />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    expect(await screen.findByText('Глава не найдена')).toBeInTheDocument();
    expect(screen.queryByText('Загрузка главы…')).not.toBeInTheDocument();
    expect(worksApi.get).not.toHaveBeenCalled();
    expect(chaptersApi.get).not.toHaveBeenCalled();
    expect(chaptersApi.listPages).not.toHaveBeenCalled();
    expect(chaptersApi.list).not.toHaveBeenCalled();
  });
});

describe('звук', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    useAuth.setState({ user: null, token: null, isAuthenticated: false, isLoading: false });
    vi.spyOn(chaptersApi, 'get').mockImplementation(() => ok(CHAPTER));
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'listPages').mockImplementation(() =>
      ok({ pages: PAGES, footnotes_html: '' }),
    );
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([CHAPTER] as Chapter[]));
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
  });

  it('при звуке — «Слушать» раскрывает панель, меню выгрузки даёт плейлист', async () => {
    vi.spyOn(audioApi, 'forWork').mockImplementation(() =>
      ok({
        tracks: [
          {
            id: 1,
            work_id: WORK.id,
            title: CHAPTER.title,
            start_page: CHAPTER.start_page,
            end_page: CHAPTER.end_page,
            duration_ms: 60_000,
            bytes: 1,
            recipe_sha256: 'r',
            pages_sha256: 'p',
            stale: false,
            created_at: '',
            url: '/api/audio/1.opus',
          },
        ],
        recordings: [],
      }),
    );
    renderChapter();
    const listen = await screen.findByRole('button', { name: 'Слушать' });
    expect(listen).toHaveAttribute('aria-expanded', 'false');
    await userEvent.click(listen);
    expect(listen).toHaveAttribute('aria-expanded', 'true');
    expect(screen.getByRole('link', { name: `Скачать «${CHAPTER.title}»` })).toHaveAttribute(
      'href',
      '/api/audio/1.opus?download=1',
    );
    await userEvent.click(screen.getByRole('button', { name: /^Скачать/ }));
    expect(screen.getByRole('menuitem', { name: /Аудио/ })).toBeInTheDocument();
  });

  // Кнопка очереди — у сотрудника; после действия глава перечитывает свой звук.
  it('редактор ставит главу в озвучку — звук тома перечитывается', async () => {
    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    const forWork = vi
      .spyOn(audioApi, 'forWork')
      .mockImplementation(() => ok({ tracks: [], recordings: [] }));
    vi.spyOn(audioApi, 'queue').mockImplementation(() => ok({ items: [], stale: [] }));
    const enqueue = vi
      .spyOn(audioApi, 'enqueue')
      .mockImplementation(() => ok({} as AudioQueueItem));
    renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Поставить в озвучку' }));
    expect(enqueue).toHaveBeenCalledWith(WORK.id, CHAPTER.id);
    await waitFor(() => expect(forWork).toHaveBeenCalledTimes(2));
  });

  it('форма прикрепления — у сотрудника, у читателя её нет', async () => {
    vi.spyOn(audioApi, 'forWork').mockImplementation(() =>
      ok({
        tracks: [],
        recordings: [
          {
            id: 9,
            work_id: WORK.id,
            chapter_id: CHAPTER.id,
            chapter_title: CHAPTER.title,
            position: 1,
            reader: '',
            content_type: 'audio/mpeg',
            bytes: 1,
            duration_ms: 1000,
            created_at: '',
            url: '/api/audio/rec/9',
          },
        ],
      }),
    );
    const view = renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Слушать' }));
    expect(screen.queryByLabelText('Прикрепить запись')).toBeNull();
    view.unmount();

    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(audioApi, 'queue').mockImplementation(() => ok({ items: [], stale: [] }));
    renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Слушать' }));
    expect(screen.getByLabelText('Прикрепить запись')).toBeInTheDocument();
  });

  // Рецензия, п. 3: ChapterView не размонтируется при смене главы — панель,
  // раскрытая у озвученной главы, не должна переезжать пустой рамкой на соседнюю.
  it('переход на соседнюю главу закрывает панель', async () => {
    const NEXT = {
      ...CHAPTER,
      id: 128,
      title: 'Соседняя',
      start_page: 248,
      end_page: 250,
    } as Chapter;
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([CHAPTER, NEXT] as Chapter[]));
    vi.spyOn(chaptersApi, 'get').mockImplementation((_w, id) =>
      ok(id === NEXT.id ? NEXT : CHAPTER),
    );
    vi.spyOn(audioApi, 'forWork').mockImplementation(() =>
      ok({
        tracks: [
          {
            id: 1,
            work_id: WORK.id,
            title: CHAPTER.title,
            start_page: 245,
            end_page: 246,
            duration_ms: 60_000,
            bytes: 1,
            recipe_sha256: 'r',
            pages_sha256: 'p',
            stale: false,
            created_at: '',
            url: '/api/audio/1.opus',
          },
        ],
        recordings: [],
      }),
    );
    const { container } = renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Слушать' }));
    expect(container.querySelector('.audio-panel')).not.toBeNull();
    await userEvent.click((await screen.findAllByRole('link', { name: /Соседняя/ }))[0]);
    await screen.findByRole('heading', { level: 1, name: 'Соседняя' });
    expect(screen.queryByRole('button', { name: 'Слушать' })).toBeNull();
    expect(container.querySelector('.audio-panel')).toBeNull();
  });

  // Сотруднику «Слушать» видна всегда; раскрыв её у соседней главы, он не
  // должен увидеть поле «Читает» и строки заливки прежней главы.
  it('менеджер записей у соседней главы начинается с чистого листа', async () => {
    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    const NEXT = {
      ...CHAPTER,
      id: 128,
      title: 'Соседняя',
      start_page: 248,
      end_page: 250,
    } as Chapter;
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([CHAPTER, NEXT] as Chapter[]));
    vi.spyOn(chaptersApi, 'get').mockImplementation((_w, id) =>
      ok(id === NEXT.id ? NEXT : CHAPTER),
    );
    vi.spyOn(audioApi, 'forWork').mockImplementation(() => ok({ tracks: [], recordings: [] }));
    vi.spyOn(audioApi, 'queue').mockImplementation(() => ok({ items: [], stale: [] }));
    renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Слушать' }));
    await userEvent.type(screen.getByLabelText('Кто читает (для прикрепляемых файлов)'), 'Иванов');
    await userEvent.click((await screen.findAllByRole('link', { name: /Соседняя/ }))[0]);
    await screen.findByRole('heading', { level: 1, name: 'Соседняя' });
    expect(screen.queryByLabelText('Кто читает (для прикрепляемых файлов)')).toBeNull();
    await userEvent.click(screen.getByRole('button', { name: 'Слушать' }));
    expect(screen.getByLabelText('Кто читает (для прикрепляемых файлов)')).toHaveValue('');
  });

  // Тикет 07, п. 4: у сотрудника пустой список при упавшем запросе читается
  // как «записей нет» — и прикрепляется повторно то, что уже лежит. Кнопка
  // очереди по той же причине: без дорожек она звала бы озвучить заново.
  it('упал запрос звука у сотрудника — «не загрузился», без формы и очереди; повтор', async () => {
    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    const forWork = vi.spyOn(audioApi, 'forWork').mockRejectedValueOnce(new Error('500'));
    vi.spyOn(audioApi, 'queue').mockImplementation(() => ok({ items: [], stale: [] }));
    renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Слушать' }));
    expect(await screen.findByText(/Звук не загрузился/)).toBeInTheDocument();
    expect(screen.queryByLabelText('Прикрепить запись')).toBeNull();
    expect(screen.queryByRole('button', { name: 'Поставить в озвучку' })).toBeNull();
    // На месте кнопки — пометка, а не молчание (рецензия, мелочь 2).
    expect(screen.getByText('Звук тома не загрузился')).toBeInTheDocument();

    let answer!: () => void;
    forWork.mockImplementation(
      () =>
        new Promise((resolve) => {
          answer = () => resolve({ data: { tracks: [], recordings: [] } } as AxiosResponse);
        }),
    );
    await userEvent.click(screen.getByRole('button', { name: 'Загрузить снова' }));
    // Отклик на нажатие, пока ответа нет (рецензия, мелочь 3).
    expect(screen.getByRole('button', { name: 'Загружаю…' })).toBeDisabled();
    await act(async () => answer());
    expect(await screen.findByLabelText('Прикрепить запись')).toBeInTheDocument();
    expect(screen.queryByText(/Звук не загрузился/)).toBeNull();
    expect(await screen.findByRole('button', { name: 'Поставить в озвучку' })).toBeInTheDocument();
  });

  // Рецензия ветки тикета 07: упавшее перечитывание после пачки не должно
  // размонтировать менеджер — иначе строки «какой файл не прикрепился и
  // почему» пропадают ровно при обрыве связи, ради которого они и пишутся.
  it('упало перечитывание после пачки — строки статуса остаются, панель предупреждает', async () => {
    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    vi.spyOn(audioApi, 'forWork')
      .mockImplementationOnce(() => ok({ tracks: [], recordings: [] }))
      .mockImplementation(() => Promise.reject(new Error('сеть')));
    vi.spyOn(audioApi, 'queue').mockImplementation(() => ok({ items: [], stale: [] }));
    vi.spyOn(audioApi, 'recordingUploadURL').mockImplementation(() =>
      Promise.reject(new Error('обрыв')),
    );
    renderChapter();
    await userEvent.click(await screen.findByRole('button', { name: 'Слушать' }));
    await userEvent.upload(screen.getByLabelText('Прикрепить запись'), [
      new File(['abc'], 'a.mp3'),
    ]);
    expect(await screen.findByText(/Звук не перечитался/)).toBeInTheDocument();
    expect(screen.getByText(/^a\.mp3: /)).toBeInTheDocument();
    expect(screen.queryByText(/Звук не загрузился/)).toBeNull();
  });

  // Тикет 07, п. 6: признак аппарата ставится руками через PUT только на
  // корень, детям он не проставляется — а worker наследует его вниз
  // (build._узлы) и отклоняет заявку подглавы. Кнопка в главе обязана
  // смотреть на путь от корня, а не на флаг самой главы.
  it('подглава аппарата, помеченного у родителя, — кнопки очереди нет', async () => {
    useAuth.setState({
      user: { id: 1, email: 'e@example.org', role: 'editor' } as User,
      token: 'tok',
      isAuthenticated: true,
      isLoading: false,
    });
    const APPARATUS = {
      ...CHAPTER,
      id: 120,
      title: 'Примечания',
      start_page: 240,
      end_page: 250,
      is_apparatus: true,
      children: [{ ...CHAPTER, parent_id: 120, is_apparatus: false, children: [] }],
    } as Chapter;
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([APPARATUS] as Chapter[]));
    vi.spyOn(chaptersApi, 'get').mockImplementation(() =>
      ok({ ...CHAPTER, parent_id: 120, is_apparatus: false } as Chapter),
    );
    vi.spyOn(audioApi, 'forWork').mockImplementation(() => ok({ tracks: [], recordings: [] }));
    vi.spyOn(audioApi, 'queue').mockImplementation(() => ok({ items: [], stale: [] }));
    renderChapter();
    // Дерево пришло — предок виден в цепочке.
    await screen.findByRole('link', { name: 'Примечания' });
    await screen.findByRole('button', { name: 'Сбросить кэш главы' });
    expect(screen.queryByRole('button', { name: 'Поставить в озвучку' })).toBeNull();
  });

  // Review Focus 5.
  it('упал запрос звука — глава как раньше: без «Слушать» и без ошибки', async () => {
    vi.spyOn(audioApi, 'forWork').mockRejectedValue(new Error('500'));
    renderChapter();
    await screen.findByRole('heading', { name: CHAPTER.title });
    expect(screen.queryByRole('button', { name: 'Слушать' })).toBeNull();
    expect(screen.queryByText(/не удалось/i)).toBeNull();
  });
});

// Переход к странице из панели: внутри главы — прокрутка без перехода, вне
// её — в самую узкую накрывающую главу на нужное место, вне всех глав — на
// отдельную полосу, а несуществующая — отказ без перехода.
describe('ChapterView page jump', () => {
  beforeEach(mockVolume4);

  function Where() {
    const { pathname, hash } = useLocation();
    return <div data-testid="where">{`${pathname}${hash}`}</div>;
  }

  function renderJump(chapterId: number) {
    return render(
      <MemoryRouter initialEntries={[`/works/4/chapters/${chapterId}`]}>
        <ReadingPreferencesProvider>
          <Routes>
            <Route
              path="/works/:workId/chapters/:chapterId"
              element={
                <>
                  <ChapterView />
                  <Where />
                </>
              }
            />
            <Route path="/works/:workId/pages/:pageNumber" element={<Where />} />
          </Routes>
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );
  }

  async function jump(page: string) {
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /Перейти к странице/ }));
    await user.keyboard(`${page}{Enter}`);
  }

  it('полоса открытой главы — прокрутка к ней, без перехода', async () => {
    renderJump(230);
    await jump('71');
    expect(document.activeElement).toHaveAttribute('id', 'chapter-page-71');
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/works\/4\/chapters\/230$/);
  });

  it('полоса вне главы — самая узкая глава, накрывающая её, на нужном месте', async () => {
    renderJump(229);
    await jump('100');
    await waitFor(() =>
      expect(screen.getByTestId('where')).toHaveTextContent(
        '/works/4/chapters/282#chapter-page-100',
      ),
    );
  });

  it('полоса вне всех глав — отдельная полоса', async () => {
    vi.spyOn(worksApi, 'pageMap').mockImplementation(() =>
      ok([{ page_number: 10, status: 'не_вычитана' }]),
    );
    renderJump(229);
    await jump('10');
    await waitFor(() =>
      expect(screen.getByTestId('where')).toHaveTextContent(/^\/works\/4\/pages\/10$/),
    );
  });

  it('несуществующая страница — отказ, никуда не уводит', async () => {
    vi.spyOn(worksApi, 'pageMap').mockImplementation(() =>
      ok([{ page_number: 10, status: 'не_вычитана' }]),
    );
    renderJump(229);
    await jump('999');
    expect(await screen.findByRole('alert')).toHaveTextContent('В томе нет страницы 999');
    expect(screen.getByTestId('where')).toHaveTextContent(/^\/works\/4\/chapters\/229$/);
  });
});
