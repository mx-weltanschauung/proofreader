import { describe, expect, it, vi, beforeEach } from 'vitest';
import { act, render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Route, Routes, useLocation } from 'react-router-dom';

import { WorkRead } from './WorkRead';
import { readingApi, worksApi, chaptersApi } from '../services/api';
import type { ReadingWindow } from '../types';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';
import { DEFAULT_PREFS, PREFS_KEY } from '../contexts/readingPrefs';
import { RECENT_KEY, readRecent } from '../hooks/useReadingProgress';

vi.mock('../services/api', () => ({
  readingApi: { window: vi.fn() },
  worksApi: { get: vi.fn() },
  chaptersApi: { list: vi.fn() },
  // useSearchTerms зовёт его при непустом ?q= в адресе (Task 11): без мока
  // тест с q в адресе падает на «No "searchApi" export is defined».
  searchApi: { terms: vi.fn().mockResolvedValue({ data: { terms: [] } }) },
}));

// chaptersApi мокается уже здесь, хотя экран зовёт его только с Task 6:
// иначе тот шаг переписывал бы этот блок.

vi.mock('katex/dist/contrib/auto-render', () => ({ default: vi.fn() }));

// jsdom не знает IntersectionObserver. Подставной запоминает наблюдаемый узел
// и даёт тесту сообщить о его появлении в кадре — тот же приём, что в
// ConceptFragmentStream.test.tsx.
class FakeObserver {
  static instances: FakeObserver[] = [];
  callback: IntersectionObserverCallback;
  observed: Element[] = [];

  constructor(callback: IntersectionObserverCallback) {
    this.callback = callback;
    FakeObserver.instances.push(this);
  }
  observe(el: Element) {
    this.observed.push(el);
  }
  unobserve() {}
  disconnect() {}
  takeRecords() {
    return [];
  }
  // С Task 6 на экране два наблюдателя: сентинель догрузки (ему нужен только
  // isIntersecting) и useVisiblePage за секциями страниц (ему нужен ещё и
  // target — по entry.target.id он находит номер страницы). Без target
  // callback useVisiblePage падает на element.id. Сообщаем о каждом
  // наблюдаемом узле отдельной записью — сентинелю (один узел) это не мешает.
  enter() {
    this.enterOnly(() => true);
  }
  // Оба наблюдателя экрана — подставные одного класса, и различить их можно
  // только по тому, за чем они следят. Тесту про положение читателя это
  // нужно врозь: сообщить о видимой секции, НЕ дёрнув догрузку окна.
  enterOnly(match: (el: Element) => boolean) {
    const entries = this.observed
      .filter(match)
      .map((el) => ({ isIntersecting: true, target: el }) as IntersectionObserverEntry);
    if (entries.length === 0) return;
    this.callback(entries, this as unknown as IntersectionObserver);
  }
}

const isSection = (el: Element) => el.classList.contains('chapter-page-section');
const isSentinel = (el: Element) => el.classList.contains('work-read-sentinel');

/** Сообщить наблюдателю о видимых секциях страниц, не трогая сентинель. */
function seeSections() {
  FakeObserver.instances.forEach((o) => o.enterOnly(isSection));
}

/** Довести сентинель до кадра: следующее окно уезжает в запрос. */
function reachSentinel() {
  FakeObserver.instances.forEach((o) => o.enterOnly(isSentinel));
}

// Настоящая прокрутка сообщает об уходе прежней секции из полосы чтения и о
// появлении новой ОДНИМ колбэком. enterOnly() так не умеет — он только
// добавляет пересечения, а useVisiblePage хранит все замеченные пересечения
// в Map и берёт минимум номера среди них; без явного «ушла» прежняя секция
// осталась бы в этой карте, и минимум не сдвинулся бы со старой страницы.
function moveVisibleTo(enterId: string, leaveIds: string[]) {
  FakeObserver.instances.forEach((o) => {
    const entries = o.observed
      .filter((el) => el.id === enterId || leaveIds.includes(el.id))
      .map(
        (el) => ({ isIntersecting: el.id === enterId, target: el }) as IntersectionObserverEntry,
      );
    if (entries.length > 0) o.callback(entries, o as unknown as IntersectionObserver);
  });
}

function windowOf(from: number, count: number, nextFrom: number | null): ReadingWindow {
  return {
    pages: Array.from({ length: count }, (_, i) => ({
      page_number: from + i,
      html: `<p>Текст страницы ${from + i}</p>`,
      notes_html: '',
      blank: false,
    })),
    next_from: nextFrom,
    total_pages: 784,
  };
}

beforeEach(() => {
  FakeObserver.instances = [];
  window.IntersectionObserver = FakeObserver as unknown as typeof IntersectionObserver;

  vi.mocked(worksApi.get).mockResolvedValue({
    data: { id: 47, title: 'Том 23', page_offset: 0, numbering_style: 'arabic' },
  } as never);
  vi.mocked(chaptersApi.list).mockResolvedValue({ data: [] } as never);
  vi.mocked(readingApi.window).mockReset();
  localStorage.clear();
});

// Зонд адреса: переход по Escape иначе не увидеть — MemoryRouter не трогает
// window.location. Тот же приём уже применён в ChapterView.test.tsx.
function LocationProbe() {
  const location = useLocation();
  return <span data-testid="location">{location.pathname}</span>;
}

// ReadingStreamBar монтирует ReadingSettings прямо на экране чтения (в
// отличие от ChapterView, где настройки живут в скрытой на этом экране шапке
// сайта) — без провайдера контекст типографики недоступен, и рендер падает.
function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <ReadingPreferencesProvider>
        <LocationProbe />
        <Routes>
          <Route path="/works/:workId/read/:pageNumber" element={<WorkRead />} />
        </Routes>
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

describe('WorkRead', () => {
  it('начинает поток со страницы из адреса', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');

    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    expect(readingApi.window).toHaveBeenCalledWith(47, 43);
  });

  it('сентинель в кадре тянет следующее окно', async () => {
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);
    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(48, 5, null) } as never);
    FakeObserver.instances.forEach((o) => o.enter());

    await waitFor(() => expect(screen.getByText('Текст страницы 48')).toBeInTheDocument());
    // Прежнее окно осталось на месте.
    expect(screen.getByText('Текст страницы 43')).toBeInTheDocument();
  });

  it('конец работы: отбивка и больше никаких запросов', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(780, 5, null) } as never);
    renderAt('/works/47/read/780');
    await waitFor(() => expect(screen.getByText('Текст страницы 780')).toBeInTheDocument());

    expect(screen.getByText(/Конец работы/i)).toBeInTheDocument();

    FakeObserver.instances.forEach((o) => o.enter());
    expect(readingApi.window).toHaveBeenCalledTimes(1);
  });

  it('сбой догрузки оставляет текст и даёт повторить', async () => {
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);
    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    vi.mocked(readingApi.window).mockRejectedValueOnce(new Error('сеть'));
    // Наблюдатель догрузки заводится только после прихода окна и
    // пересоздаётся при смене loadMore; прежний, отключённый, остаётся в
    // instances со старым замыканием. На медленной машине (раннер CI) вход,
    // сообщённый всем подряд сразу после текста, попадал в старый —
    // догрузка не шла, и кнопки «Повторить» не было. Ждём наблюдателя,
    // который следит за сентинелем, и сообщаем вход последнему из них.
    const isSentinel = (el: Element) => el.classList.contains('work-read-sentinel');
    const sentinelObserver = await waitFor(() => {
      const o = [...FakeObserver.instances].reverse().find((x) => x.observed.some(isSentinel));
      expect(o).toBeDefined();
      return o!;
    });
    sentinelObserver.enterOnly(isSentinel);

    const retry = await screen.findByRole('button', { name: /Повторить/i });
    // Накопленное на месте.
    expect(screen.getByText('Текст страницы 43')).toBeInTheDocument();

    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(48, 5, null) } as never);
    await userEvent.click(retry);

    await waitFor(() => expect(screen.getByText('Текст страницы 48')).toBeInTheDocument());
  });

  it('пока первое окно летит, показывает загрузку', () => {
    vi.mocked(readingApi.window).mockReturnValue(new Promise(() => {}) as never);
    renderAt('/works/47/read/43');

    expect(screen.getByText(/Загрузка/i)).toBeInTheDocument();
  });
});

describe('WorkRead: положение читателя', () => {
  it('прячет хром сайта, пока экран открыт', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);
    const { unmount } = renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    expect(document.body.classList.contains('reading-immersive')).toBe(true);

    unmount();
    expect(document.body.classList.contains('reading-immersive')).toBe(false);
  });

  // Спека: Escape возвращает в БЕГУЩУЮ главу, а не в ту, через которую
  // читатель вошёл, — за сотню страниц потока это давно разные главы.
  it('Escape уводит в бегущую главу', async () => {
    vi.mocked(chaptersApi.list).mockResolvedValue({
      data: [
        {
          id: 2066,
          title: 'Книга первая',
          start_page: 43,
          end_page: 784,
          order_number: 1,
          children: [],
        },
      ],
    } as never);
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    // Шевелим только наблюдателя за секциями: сентинель тут ни при чём, а его
    // loadMore увёл бы тест в догрузку следующего окна.
    act(() => seeSections());

    await userEvent.keyboard('{Escape}');

    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent('/works/47/chapters/2066'),
    );
  });

  // Приезд окна не должен стирать то, что читалка знает о положении
  // читателя. Пока прежний код ждал первого колбэка пересозданного
  // наблюдателя, полоса прогресса дёргалась в ноль, бегущий заголовок
  // пропадал, а ссылка выхода откатывалась с главы на том — и Escape,
  // нажатый в этот кадр, уводил не туда.
  it('догрузка окна не теряет положение читателя', async () => {
    vi.mocked(chaptersApi.list).mockResolvedValue({
      data: [
        {
          id: 2066,
          title: 'Книга первая',
          start_page: 43,
          end_page: 784,
          order_number: 1,
          children: [],
        },
      ],
    } as never);
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    act(() => seeSections());

    await waitFor(() => expect(screen.getByText('Книга первая')).toBeInTheDocument());
    const progressBefore = screen.getByRole('progressbar').getAttribute('aria-valuenow');
    expect(progressBefore).not.toBe('0');
    expect(screen.getByRole('link', { name: /Выйти из чтения/ })).toHaveAttribute(
      'href',
      '/works/47/chapters/2066',
    );

    // Сентинель тянет второе окно. Наблюдатели отчитываются СИНХРОННО, до
    // ответа сети, так что после приезда окна нового колбэка не будет:
    // всё, что показано ниже, — состояние без единого нового замера.
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(48, 5, null) } as never);
    act(() => reachSentinel());
    await waitFor(() => expect(screen.getByText('Текст страницы 48')).toBeInTheDocument());

    expect(screen.getByText('Книга первая')).toBeInTheDocument();
    expect(screen.getByRole('progressbar')).toHaveAttribute('aria-valuenow', progressBefore!);
    expect(screen.getByRole('link', { name: /Выйти из чтения/ })).toHaveAttribute(
      'href',
      '/works/47/chapters/2066',
    );
  });

  // Прошлый тест ловит откат выхода только после приезда окна. Но окно летит
  // не мгновенно, и весь этот промежуток читатель может нажать Escape: экран
  // обязан знать, где он, и пока ответ в пути.
  //
  // Наблюдатели разводятся во времени намеренно: сперва отчитывается
  // наблюдатель видимой страницы (положение известно), затем сентинель
  // (догрузка пошла), и только потом жмётся Escape — при неразрешённом
  // промисе окна.
  it('Escape посреди догрузки уводит в бегущую главу, а не в том', async () => {
    vi.mocked(chaptersApi.list).mockResolvedValue({
      data: [
        {
          id: 2066,
          title: 'Книга первая',
          start_page: 43,
          end_page: 784,
          order_number: 1,
          children: [],
        },
      ],
    } as never);
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    act(() => seeSections());
    await waitFor(() => expect(screen.getByText('Книга первая')).toBeInTheDocument());

    // Второе окно повисает в полёте: разрешать промис тест будет сам.
    let deliverWindow: (value: unknown) => void = () => {};
    vi.mocked(readingApi.window).mockReturnValueOnce(
      new Promise((resolve) => {
        deliverWindow = resolve;
      }) as never,
    );
    act(() => reachSentinel());

    // Догрузка идёт прямо сейчас — иначе проверять нечего.
    await waitFor(() => expect(screen.getByText('Загрузка…')).toBeInTheDocument());

    await userEvent.keyboard('{Escape}');

    await waitFor(() =>
      expect(screen.getByTestId('location')).toHaveTextContent('/works/47/chapters/2066'),
    );

    // Окно приезжает уже после ухода: незавершённый запрос не должен ничего
    // ломать напоследок.
    await act(async () => {
      deliverWindow({ data: windowOf(48, 5, null) });
    });
    expect(screen.getByTestId('location')).toHaveTextContent('/works/47/chapters/2066');
  });

  // Точка входа в правку: том — основной способ читать, и без входа отсюда
  // между «заметил опечатку» и «могу исправить» стоял бы лишний переход с
  // потерей места чтения.
  it('ведёт «Предложить исправление» на видимую сейчас страницу', async () => {
    // Своя фикстура, не windowOf: у неё текст без точки на конце, и
    // stitchPages (см. utils/stitchPages.ts) читает отсутствие точки как
    // незаконченную фразу — весь пятистраничный оконный охват склеивается в
    // один абзац 43-й полосы, а 44–47 садятся на шов внутрь неё же. Секция
    // 43 тогда не «внутренняя» (в ней вложены видимые швы), и минимум среди
    // вложенных сдвигается на 44 — здесь же нужна страница, которую читатель
    // реально видит первой, поэтому текст оканчивается точкой, склейки нет,
    // и все пять секций остаются полосами-соседями, как в живом тексте.
    const sentencePage = (n: number) => ({
      page_number: n,
      html: `<p>Текст страницы ${n}.</p>`,
      notes_html: '',
      blank: false,
    });
    vi.mocked(readingApi.window).mockResolvedValue({
      data: {
        pages: [43, 44, 45, 46, 47].map(sentencePage),
        next_from: 48,
        total_pages: 784,
      },
    } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43.')).toBeInTheDocument());

    // Видимая страница ещё не определена (наблюдатель ничего не сообщил) —
    // рисовать ссылку не на что.
    expect(screen.queryByRole('link', { name: /предложить исправление/i })).not.toBeInTheDocument();

    act(() => seeSections());

    const link = await screen.findByRole('link', { name: /предложить исправление/i });
    expect(link).toHaveAttribute('href', '/works/47/pages/43/suggest');
  });

  // Находка координатора: адрес следовал за читателем, но полностью терял
  // строку запроса — в т.ч. ?q= с Task 11, ради которого «скопировать
  // ссылку»/перезагрузка и задуманы этим эффектом. window.history.replaceState
  // — родной DOM API, MemoryRouter его не подменяет (см. LocationProbe выше),
  // поэтому итог виден прямо в window.location, как и в реальном браузере.
  it('сохраняет строку запроса при переписывании адреса под видимую страницу', async () => {
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    act(() =>
      FakeObserver.instances.forEach((o) => o.enterOnly((el) => el.id === 'chapter-page-43')),
    );
    await waitFor(() => expect(window.location.pathname).toBe('/works/47/read/43'));
    expect(window.location.search).toBe('?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F');

    // Видимая страница меняется — эмулируем прокрутку именно на 48-ю секцию,
    // а не seeSections(): тот отмечает пересекающими сразу все накопленные
    // секции обоих окон, и минимум среди них остался бы 43.
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(48, 5, null) } as never);
    act(() => reachSentinel());
    await waitFor(() => expect(screen.getByText('Текст страницы 48')).toBeInTheDocument());

    act(() => moveVisibleTo('chapter-page-48', ['chapter-page-43']));
    await waitFor(() => expect(window.location.pathname).toBe('/works/47/read/48'));
    // Строка запроса пережила смену страницы — ради этого чинили находку.
    expect(window.location.search).toBe('?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F');
  });

  it('без строки запроса переписывает голый путь, без лишнего "?"', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    act(() =>
      FakeObserver.instances.forEach((o) => o.enterOnly((el) => el.id === 'chapter-page-43')),
    );
    await waitFor(() => expect(window.location.pathname).toBe('/works/47/read/43'));
    expect(window.location.search).toBe('');
    expect(window.location.href).not.toContain('?');
  });

  // Порог в минуту стережёт хук (useReadingProgress.test.ts); здесь —
  // проводка: в строку уходит бегущая глава, а не id полосы или тома. Глава
  // уже в списке, поэтому строка поднимается сразу.
  it('пишет в список настоящую бегущую главу', async () => {
    mockRunningChapter();
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
          workId: 47,
          chapterId: 2066,
          workTitle: 'Том',
          chapterTitle: 'Книга первая',
          pageNumber: 1,
          ts: 1,
        },
      ]),
    );

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    // useVisiblePage сам по себе ничего не видит — IntersectionObserver в
    // jsdom заглушка, сообщить о видимой секции должен тест.
    act(() => FakeObserver.instances.forEach((o) => o.enter()));

    await waitFor(() => {
      const [top] = readRecent();
      expect(top?.workId).toBe(47);
      expect(top?.chapterId).toBe(2066);
      expect(top?.pageNumber).not.toBe(1);
    });
  });

  it('новую бегущую главу сразу в список не заводит', async () => {
    mockRunningChapter();
    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    act(() => FakeObserver.instances.forEach((o) => o.enter()));
    await waitFor(() => expect(window.location.pathname).toMatch(/\/read\/4\d$/));
    expect(readRecent()).toEqual([]);
  });
});

function mockRunningChapter() {
  vi.mocked(chaptersApi.list).mockResolvedValue({
    data: [
      {
        id: 2066,
        title: 'Книга первая',
        start_page: 43,
        end_page: 784,
        order_number: 1,
        children: [],
      },
    ],
  } as never);
  vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);
}

// Печатный номер нужен, чтобы сослаться на источник по бумажному изданию.
// В сплошном чтении он мешает, поэтому живёт под тумблером — как и в чтении
// главы.
describe('WorkRead: номера страниц', () => {
  // replaceState живёт в jsdom и переживает тест — следующий начинал бы с
  // адреса, оставленного предыдущим.
  beforeEach(() => window.history.replaceState(null, '', '/'));

  it('по умолчанию номера показаны', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    const { container } = renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    expect(container.querySelector('.reading-chunk')).toHaveClass('show-page-info');
  });

  // Настройка общая с чтением главы и живёт в localStorage: выключивший
  // номера читатель не должен встретить их снова, перейдя в поток.
  it('чтит выключенные номера, сохранённые на другом экране читальни', async () => {
    localStorage.setItem(PREFS_KEY, JSON.stringify({ ...DEFAULT_PREFS, pageNumbers: false }));
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    const { container } = renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    expect(container.querySelector('.reading-chunk')).not.toHaveClass('show-page-info');
  });

  it('тумблер прячет номера во всех загруженных окнах', async () => {
    vi.mocked(readingApi.window)
      .mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never)
      .mockResolvedValueOnce({ data: windowOf(48, 5, null) } as never);

    const { container } = renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());
    await act(async () => {
      reachSentinel();
    });
    await waitFor(() => expect(screen.getByText('Текст страницы 48')).toBeInTheDocument());
    expect(container.querySelectorAll('.reading-chunk')).toHaveLength(2);

    await userEvent.click(screen.getByRole('button', { name: 'Номера страниц' }));

    // Второе окно уже смонтировано со своим состоянием — тумблер обязан
    // достать и до него, иначе номера останутся на части потока.
    container.querySelectorAll('.reading-chunk').forEach((chunk) => {
      expect(chunk).not.toHaveClass('show-page-info');
    });
  });

  it('включённые номера доходят до дерева доступности', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    expect(screen.getByRole('link', { name: /Страница 43/ })).toHaveAttribute(
      'href',
      '/works/47/read/43',
    );
  });

  // Хэш в потоке для пересылки не годится: окна догружаются от полосы из
  // маршрута, и у получателя ссылки нужной секции в документе просто нет.
  // Поэтому якорь полосы здесь — сам адрес потока.
  it('клик по маркеру правит адрес, не пересобирая поток', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 45')).toBeInTheDocument());
    const callsBefore = vi.mocked(readingApi.window).mock.calls.length;

    await userEvent.click(screen.getByRole('link', { name: /Страница 45/ }));

    expect(window.location.pathname).toBe('/works/47/read/45');
    // Поток остался тем же: окна не перезапрашивались, текст не пропал.
    expect(vi.mocked(readingApi.window).mock.calls.length).toBe(callsBefore);
    expect(screen.getByText('Текст страницы 43')).toBeInTheDocument();
  });

  it('повторное нажатие возвращает номера обратно', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);

    const { container } = renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    const toggle = screen.getByRole('button', { name: 'Номера страниц' });
    await userEvent.click(toggle);
    await userEvent.click(toggle);

    expect(container.querySelector('.reading-chunk')).toHaveClass('show-page-info');
  });
});

// WorkRead — единственный экран чтения, где кнопка цитаты до сих пор не была
// проверена ни одним тестом. Проверка целиком на месте: CiteButton уже
// разобран в изоляции (CiteButton.test.tsx), но никто не подтверждал, что
// сам экран передаёт ему contentRef, указывающий на настоящее дерево
// ReadingChunk, и что секции полос внутри несут data-page. Пропади любое из
// двух — кнопка осталась бы на месте («Ссылка» при видимой полосе), но при
// выделении текста молча копировала бы голую ссылку на видимую страницу
// вместо цитаты с текстом, а тест не заметил бы: набор оставался бы зелёным.
describe('WorkRead: цитата в потоке', () => {
  it('кнопка цитаты видит выделение внутри article, полосы несут data-page', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);
    const { container } = renderAt('/works/47/read/43');
    await waitFor(() => expect(screen.getByText('Текст страницы 43')).toBeInTheDocument());

    // Без видимой полосы кнопки нет вовсе (visiblePage === null и выделения
    // нет) — сначала нужно её обнаружить, как и остальные тесты положения
    // читателя.
    act(() => seeSections());

    // Мутация Item 2: сними data-page в ReadingChunk.tsx — и эта проверка
    // упадёт здесь же. Нужна именно НЕсклеенная секция первой полосы окна
    // (id="chapter-page-43"): склеенные абзацы других страниц несут свой
    // data-page независимо, через шов stitchPages.ts, и не заметили бы
    // потери атрибута на самой секции.
    const firstSection = container.querySelector('#chapter-page-43');
    expect(firstSection).not.toBeNull();
    expect(firstSection).toHaveAttribute('data-page', '43');

    expect(await screen.findByRole('button', { name: 'Ссылка' })).toBeInTheDocument();

    // Мутация Item 2: сними ref={contentRef} с <article> в WorkRead.tsx —
    // contentRef.current останется null, hasSelectionInside всегда вернёт
    // false, и кнопка ниже никогда не покажет «Цитировать» несмотря на
    // настоящее выделение внутри article.
    const article = container.querySelector('article.work-read-content');
    expect(article).not.toBeNull();
    const paragraph = screen.getByText('Текст страницы 43');
    expect(article!.contains(paragraph)).toBe(true);

    const textNode = paragraph.firstChild!;
    const range = document.createRange();
    range.setStart(textNode, 0);
    range.setEnd(textNode, (textNode.textContent ?? '').length);
    const selection = window.getSelection();
    selection?.removeAllRanges();
    selection?.addRange(range);
    act(() => {
      document.dispatchEvent(new Event('selectionchange'));
    });

    expect(await screen.findByRole('button', { name: 'Цитировать' })).toBeInTheDocument();
  });
});

// Переход к странице из панели потока: подгруженная полоса — прокрутка без
// нового запроса, остальная — перезапуск потока с неё, за концом тома — отказ.
describe('WorkRead: переход к странице', () => {
  async function jump(page: string) {
    const user = userEvent.setup();
    await user.click(await screen.findByRole('button', { name: /Перейти к странице/ }));
    await user.keyboard(`${page}{Enter}`);
  }

  it('подгруженная полоса — прокрутка, поток не перезапускается', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);
    renderAt('/works/47/read/43');
    await screen.findByText('Текст страницы 43');

    await jump('45');

    expect(document.activeElement).toHaveAttribute('id', 'chapter-page-45');
    expect(readingApi.window).toHaveBeenCalledTimes(1);
    expect(screen.getByTestId('location')).toHaveTextContent(/^\/works\/47\/read\/43$/);
  });

  it('полоса вне подгруженного — поток с неё', async () => {
    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(43, 5, 48) } as never);
    renderAt('/works/47/read/43');
    await screen.findByText('Текст страницы 43');

    vi.mocked(readingApi.window).mockResolvedValueOnce({ data: windowOf(300, 5, 305) } as never);
    await jump('300');

    await screen.findByText('Текст страницы 300');
    expect(readingApi.window).toHaveBeenLastCalledWith(47, 300);
    expect(screen.getByTestId('location')).toHaveTextContent(/^\/works\/47\/read\/300$/);
  });

  it('за концом тома — отказ, поток на месте', async () => {
    vi.mocked(readingApi.window).mockResolvedValue({ data: windowOf(43, 5, 48) } as never);
    renderAt('/works/47/read/43');
    await screen.findByText('Текст страницы 43');

    await jump('785');

    expect(await screen.findByRole('alert')).toHaveTextContent('В томе нет страницы 785');
    expect(readingApi.window).toHaveBeenCalledTimes(1);
  });
});
