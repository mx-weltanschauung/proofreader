import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { searchApi, worksApi, shelfApi } from '../services/api';
import type {
  SearchResponse,
  SearchPagesResponse,
  SearchPage,
  SearchVolume,
  Shelf,
} from '../types';
import { Search } from './Search';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

/** Управляемый ответ: разрешается вручную из теста, а не сразу при вызове мока. */
function deferred<T>(): { promise: Promise<AxiosResponse<T>>; resolve: (data: T) => void } {
  let resolve!: (data: T) => void;
  const promise = new Promise<AxiosResponse<T>>((res) => {
    resolve = (data: T) => res({ data } as unknown as AxiosResponse<T>);
  });
  return { promise, resolve };
}

function volume(work_id: number, extra: Partial<SearchVolume> = {}): SearchVolume {
  return {
    work_id,
    title: `Том ${work_id}`,
    author: 'Автор',
    volume_label: `т. ${work_id}`,
    edition_id: 1,
    edition_title: 'Собрание',
    role: 'volume',
    parent_work_id: null,
    text_hits: 3,
    apparatus_hits: 1,
    numbering_style: 'arabic',
    pages: [],
    ...extra,
  };
}

/** Совпавшая полоса для строки тома: та же форма, что и в выдаче по тому. */
function hit(page_number: number, extra: Partial<SearchPage> = {}): SearchPage {
  return {
    page_number,
    printed_number: page_number,
    chapter_title: 'О Гегеле',
    chapter_id: 5,
    is_apparatus: false,
    snippet: 'читали \u0001Гегеля\u0002 <все>',
    ...extra,
  };
}

const first: SearchResponse = {
  query: 'Гегеля',
  terms: ['гегел'],
  chapters: [
    {
      id: 5,
      title: 'О Гегеле',
      work_id: 2,
      work_title: 'Том 2',
      volume_label: 'т. 2',
      edition_title: 'Собрание',
      is_apparatus: false,
    },
  ],
  concepts: [{ slug: 'gegel', title: 'Гегель' }],
  volumes: [
    volume(2),
    volume(21, { role: 'front_matter', parent_work_id: 2, title: 'Передние листы' }),
  ],
  // total_hits — сумма text_hits по томам (3 + 3), как его считает сервер:
  // аппарат в него не входит, он назван в строке тома отдельно.
  total_hits: 6,
};

const pages: SearchPagesResponse = {
  query: 'Гегеля',
  terms: ['гегел'],
  total: 1,
  pages: [
    {
      page_number: 14,
      printed_number: 14,
      chapter_title: 'О Гегеле',
      chapter_id: 5,
      is_apparatus: false,
      snippet: 'читали \u0001Гегеля\u0002 <все>',
    },
  ],
  chapters: [
    {
      id: 5,
      title: 'О Гегеле',
      hits: 1,
    },
  ],
  chapters_total: 1,
};

/**
 * Полка для строки области (ScopeBar): заголовки собраний/томов, которых
 * ScopeBar сама не знает и берёт из loadShelfOnce (см. utils/shelfCache.ts).
 * Полями VolumeSummary/Work, которых ScopeBar не читает, фикстура не
 * заполнена — приведение типа тем же приёмом, что и у worksApi.get ниже.
 */
const shelfFixture = {
  editions: [
    {
      edition: {
        id: 1,
        title: 'Собрание',
        slug: 's',
        description: '',
        created_at: '',
        updated_at: '',
      },
      volumes: [{ id: 2, title: 'Том 2' }],
    },
    {
      edition: {
        id: 2,
        title: 'Собрание 2',
        slug: 's2',
        description: '',
        created_at: '',
        updated_at: '',
      },
      volumes: [
        { id: 13, title: 'Том 13' },
        { id: 14, title: 'Том 14' },
      ],
    },
  ],
  loose_works: [],
} as unknown as Shelf;

function renderAt(path: string) {
  return render(
    <MemoryRouter initialEntries={[path]}>
      <Search />
    </MemoryRouter>,
  );
}

describe('Search', () => {
  // loadShelfOnce кэширует ответ модульным синглтоном на весь файл (см.
  // shelfCache.ts) — второй и далее моки этого спая после первого успешного
  // резолва уже не читаются, но подставлять его в каждом тесте всё равно
  // необходимо: порядок тестов не гарантирован, и первый же тест, дошедший
  // до ScopeBar, обязан получить фикстуру, а не настоящий axios.
  beforeEach(() => {
    vi.spyOn(shelfApi, 'get').mockImplementation(() => ok(shelfFixture));
  });
  afterEach(() => vi.restoreAllMocks());

  it('состояние 1: каталог и тома, служебная работа под родителем', async () => {
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=Гегеля');

    expect(await screen.findByRole('link', { name: 'О Гегеле' })).toHaveAttribute(
      'href',
      '/works/2/chapters/5',
    );
    expect(screen.getByRole('link', { name: 'Гегель' })).toHaveAttribute('href', '/concepts/gegel');
    const rows = screen.getAllByRole('link', { name: /Том 2|Передние листы/ });
    expect(rows[0]).toHaveAttribute(
      'href',
      '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F&works=2',
    );
    expect(rows[1]).toHaveTextContent('Передние листы');
    // Обе строки фикстуры несут text_hits: 3 / apparatus_hits: 1 (второй
    // volume() переопределяет только role/parent_work_id/title) — getByText
    // без области видимости находит два совпадения и падает с
    // «multiple elements». Проверка сужена до строки Тома 2, смысл тот же:
    // счёт полос и счёт аппарата отрисованы верно.
    expect(rows[0].closest('li')).toHaveTextContent(/3 полосы/);
    expect(rows[0].closest('li')).toHaveTextContent(/1 в аппарате/);
    // Заголовок над строками томов обязан сходиться с самими строками: он
    // говорит «в тексте», строка тома печатает свой text_hits, аппарат
    // назван отдельно. Сумма считается из фикстуры, а не вписана числом —
    // иначе тест переписывает ту же арифметику, что и проверяемый код.
    const textSum = first.volumes.reduce((n, v) => n + v.text_hits, 0);
    expect(
      screen.getByRole('heading', { name: `В тексте — ${textSum} полос` }),
    ).toBeInTheDocument();
    // Область не выбрана вовсе — строке области нечего показывать, и она не
    // рисуется: без этой проверки ScopeBar мог бы всегда лезть на экран
    // (например, отвалившийся ранний return для пустой области), а «состояние
    // 1» ниже это не заметило бы — оно не смотрит на .scope-bar вообще.
    expect(screen.queryByText(/область поиска/i)).not.toBeInTheDocument();
  });

  it('состояние 2: фильтр собрания уезжает в API и показывается в строке области', async () => {
    const spy = vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=Гегеля&edition=1');

    // «Собрание» — то же слово, что и в edition_title строки тома ниже
    // (search-volume-meta): сужение до чипа обязательно, иначе getByText без
    // selector падает с «нашлось больше одного».
    expect(await screen.findByText('Собрание', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(spy).toHaveBeenCalledWith('Гегеля', { editions: [1], works: [], chapters: [] });
  });

  it('переход к тому не теряет фильтр собрания', async () => {
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=Гегеля&edition=1');

    const rows = await screen.findAllByRole('link', { name: /Том 2|Передние листы/ });
    // Читатель сузил поиск до собрания; строка тома обязана вести в том
    // ВНУТРИ этого собрания — иначе клик по тому потерял бы контекст
    // собрания, которое читатель уже выбрал.
    expect(rows[0]).toHaveAttribute(
      'href',
      '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F&editions=1&works=2',
    );
  });

  // Круг правок 1, находка 2: у прежней версии этого теста не было ссылки,
  // которая снимала бы только том, держа собрание, — «Сбросить» сбрасывал
  // область целиком. Крестик на чипе тома (ScopeChip) как раз и есть такая
  // ссылка, восстановленная в виде, который заодно умеет снимать один
  // лишний том из области в несколько (три теста ниже). Здесь же — прямая
  // проверка поправки спеки от 18.09.2026: том вместе с собранием показывает
  // полосы тома, а не тонет в обзоре (см. singleWorkOf в searchScope.ts).
  it('том вместе с собранием — сразу полосы, собрание остаётся в строке области', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    const search = vi.spyOn(searchApi, 'search');
    renderAt('/search?q=Гегеля&work=2&edition=1');

    expect(await screen.findByText('Собрание', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(screen.getByText('Том 2', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(searchApi.pages).toHaveBeenCalled();
    expect(search).not.toHaveBeenCalled();
  });

  // Круг правок 1, находка 2: восстановленный одношаговый возврат «ко всем
  // томам собрания» — крестик снимает ровно том, собрание остаётся. Область
  // живёт в адресе, поэтому проверка — это href самой ссылки-крестика, а не
  // состояние компонента.
  it('крестик тома снимает том, оставляя собрание в области', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=Гегеля&work=2&edition=1');

    const removeWork = await screen.findByRole('link', {
      name: 'Снять том «Том 2» из области поиска',
    });
    expect(removeWork).toHaveAttribute(
      'href',
      '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F&editions=1',
    );
  });

  it('крестик собрания снимает собрание, оставляя том в области', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=Гегеля&work=2&edition=1');

    const removeEdition = await screen.findByRole('link', {
      name: 'Снять собрание «Собрание» из области поиска',
    });
    expect(removeEdition).toHaveAttribute(
      'href',
      '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F&works=2',
    );
  });

  // Снятие последнего элемента области — это и есть «Сбросить»: searchPath
  // не пишет параметры для пустых списков, отдельной ветки для «последний»
  // не заводим.
  it('крестик последнего чипа ведёт на выдачу без области', async () => {
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=Гегеля&edition=1');

    const removeEdition = await screen.findByRole('link', {
      name: 'Снять собрание «Собрание» из области поиска',
    });
    expect(removeEdition).toHaveAttribute('href', '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F');
  });

  it('состояние 3: полосы тома с отрывком без утечки HTML', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    const search = vi.spyOn(searchApi, 'search');
    renderAt('/search?q=Гегеля&work=2');

    expect(await screen.findByText('Том 2', { selector: '.scope-chip' })).toBeInTheDocument();
    const mark = screen.getByText('Гегеля', { selector: 'mark' });
    expect(mark).toHaveClass('search-hit');
    expect(screen.getByText(/<все>/)).toBeInTheDocument();
    expect(document.querySelector('.search-page-snippet')?.innerHTML).not.toContain('<все>');
    // Полоса открывается сама по себе, а не в потоке чтения: читатель пришёл
    // за конкретной полосой, и окно потока увело бы его к соседям.
    expect(screen.getByRole('link', { name: /14/ })).toHaveAttribute(
      'href',
      '/works/2/pages/14?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
    // Ближайшая глава — ссылка, а не подпись.
    expect(screen.getByRole('link', { name: 'О Гегеле' })).toHaveAttribute(
      'href',
      '/works/2/chapters/5',
    );
    expect(search).not.toHaveBeenCalled();
    // Полный сброс области — одна кнопка вместо прежней ссылки «ко всем
    // томам»: снять только том, оставив прочее, здесь нечем — точечная
    // правка идёт через «Изменить» (ту же панель поиска).
    expect(screen.getByRole('link', { name: /сбросить/i })).toHaveAttribute(
      'href',
      '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
  });

  // Круг правок 1, находка 1 (регрессия). Все ссылки, которые задача 12
  // расставила по читальне (кнопка «Искать в томе», строка тома, «ко всем
  // томам»), пишут МНОЖЕСТВЕННОЕ works=/editions= через searchPath из
  // utils/searchScope.ts — а Search.tsx до этой правки читал только голое
  // params.get('work'). На свежей ссылке это давало null и обзор всего
  // корпуса вместо полос тома. Тест ниже рендерит именно новый формат
  // (works=, без старого work=) — старым форматом уже проверяет тест
  // «состояние 3» выше, дословно тем же телом.
  it('регрессия: адрес нового формата works= тоже открывает полосы тома, а не обзор', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    const search = vi.spyOn(searchApi, 'search');
    renderAt('/search?q=Гегеля&works=2');

    expect(await screen.findByText('Том 2', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /14/ })).toHaveAttribute(
      'href',
      '/works/2/pages/14?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
    // Обзор каталога сюда бы дёрнул searchApi.search — по нему и отличаем
    // «показаны полосы тома» от «читатель уехал в нескоуплённый обзор».
    expect(search).not.toHaveBeenCalled();
  });

  // Тот же класс регрессии для входа «Искать в собрании»: ссылка пишет
  // editions= (множественное), а старое чтение брало только edition=.
  it('регрессия: адрес нового формата editions= сужает обзор до собрания', async () => {
    const search = vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=Гегеля&editions=1');

    expect(await screen.findByText('Собрание', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(search).toHaveBeenCalledWith('Гегеля', { editions: [1], works: [], chapters: [] });
  });

  it('«ещё» просит следующий offset', async () => {
    // Первое окно — ровно PAGE_SIZE полос: offset второго равен их числу.
    const many: SearchPagesResponse = {
      ...pages,
      total: 60,
      pages: Array.from({ length: 50 }, (_, i) => ({
        ...pages.pages[0],
        page_number: i + 1,
        printed_number: i + 1,
      })),
    };
    const spy = vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(many));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=Гегеля&work=2');

    await userEvent.click(await screen.findByRole('button', { name: /ещё/i }));
    await waitFor(() =>
      expect(spy).toHaveBeenLastCalledWith({ q: 'Гегеля', work_id: 2, limit: 50, offset: 50 }),
    );
  });

  it('двойной клик «ещё» не шлёт второй запрос, пока первый в пути', async () => {
    const initial: SearchPagesResponse = {
      ...pages,
      total: 3,
      pages: [{ ...pages.pages[0], page_number: 1, printed_number: 1 }],
    };
    const second: SearchPagesResponse = {
      ...pages,
      total: 3,
      pages: [{ ...pages.pages[0], page_number: 2, printed_number: 2 }],
    };
    const { promise: secondPromise, resolve: resolveSecond } = deferred<SearchPagesResponse>();
    const spy = vi
      .spyOn(searchApi, 'pages')
      .mockImplementation((params) => (params.offset === 0 ? ok(initial) : secondPromise));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=Гегеля&work=2');

    const button = await screen.findByRole('button', { name: /ещё/i });
    await userEvent.click(button);
    await userEvent.click(button);

    // Второй клик не должен уйти отдельным запросом, пока первый ещё висит
    // в воздухе — иначе оба ответа допишут одно и то же окно офсетов, и
    // список полос задвоится (со сгенерированными React `key` тоже).
    const followUpCalls = spy.mock.calls.filter(([params]) => params.offset !== 0);
    expect(followUpCalls).toHaveLength(1);

    resolveSecond(second);

    await waitFor(() => {
      const numbers = screen.getAllByText(/^с\. \d+$/).map((el) => el.textContent);
      expect(numbers).toHaveLength(2);
      expect(new Set(numbers).size).toBe(2);
    });
  });

  it('ошибка запроса полос показывает баннер, а не «ничего не найдено»', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => Promise.reject(new Error('boom')));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=Гегеля&work=2');

    expect(await screen.findByText('Поиск не удался')).toBeInTheDocument();
    expect(screen.queryByText(/ничего не найдено/i)).not.toBeInTheDocument();
  });

  it('успешный повтор «ещё» после ошибки убирает баннер', async () => {
    const initial: SearchPagesResponse = {
      ...pages,
      total: 2,
      pages: [{ ...pages.pages[0], page_number: 1, printed_number: 1 }],
    };
    const success: SearchPagesResponse = {
      ...pages,
      total: 2,
      pages: [{ ...pages.pages[0], page_number: 2, printed_number: 2 }],
    };
    let attempt = 0;
    vi.spyOn(searchApi, 'pages').mockImplementation((params) => {
      if (params.offset === 0) return ok(initial);
      attempt += 1;
      return attempt === 1 ? Promise.reject(new Error('boom')) : ok(success);
    });
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 2, title: 'Том 2', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=Гегеля&work=2');

    const button = await screen.findByRole('button', { name: /ещё/i });
    await userEvent.click(button);
    expect(await screen.findByText('Поиск не удался')).toBeInTheDocument();

    await userEvent.click(button);
    await waitFor(() => expect(screen.queryByText('Поиск не удался')).not.toBeInTheDocument());
  });

  it('строка области печатает выбранное и открывает панель', async () => {
    vi.spyOn(searchApi, 'search').mockImplementation(() =>
      ok({ ...first, chapters: [], concepts: [], volumes: [], total_hits: 0 }),
    );
    renderAt('/search?q=партия&works=13,14');

    expect(await screen.findByText(/область поиска/i)).toBeInTheDocument();
    await userEvent.click(screen.getByRole('button', { name: /изменить/i }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();
  });

  it('один том в области — сразу полосы, без обзора', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    const search = vi.spyOn(searchApi, 'search');
    renderAt('/search?q=xx&works=13');

    expect(await screen.findByText('Том 13', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(searchApi.pages).toHaveBeenCalled();
    expect(search).not.toHaveBeenCalled();
  });

  it('выбранные главы уезжают в запрос полос', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=xx&works=13&chapters=101');

    await screen.findByText('Том 13', { selector: '.scope-chip' });
    // Находка 3 итоговой рецензии: заголовок главы 101 берёт чип из фасета
    // (chapters в ответе searchApi.pages), а фикстура `pages` фасета для неё
    // не знает вовсе (там только глава 5) — ровно тот редкий случай, когда
    // заголовок неизвестен (переход по ссылке на главу, выпавшую из топ-30
    // фасета), и чип возвращается к прежнему голому номеру, а не «висит» на
    // «…» без надежды когда-либо разрешиться.
    expect(screen.getByText('глава 101', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(searchApi.pages).toHaveBeenCalledWith(expect.objectContaining({ chapters: [101] }));
  });

  // Находка 3 итоговой рецензии: чип строки области раньше печатал голый
  // `глава ${id}` — внутренний ключ БД, ничего не говорящий читателю. Когда
  // выбранная глава есть в фасете (обычный путь — глава выбрана кликом по
  // самому фасету, а не вручную вписанной ссылкой), чип обязан показывать её
  // настоящий заголовок.
  it('чип главы в строке области показывает заголовок, а не голый id', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    // pages.chapters несёт { id: 5, title: 'О Гегеле' } — тот же id, что и
    // выбранная в адресе глава.
    renderAt('/search?q=xx&works=13&chapters=5');

    expect(await screen.findByText('О Гегеле', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(screen.queryByText('глава 5', { selector: '.scope-chip' })).not.toBeInTheDocument();
  });

  // Задача 14: чипы ScopeBar и кнопки ChapterFacet — два взгляда на один и
  // тот же scope.chapters из адреса. Они обязаны сходиться в обе стороны:
  // добавление главы через фасет заводит чип в строке области, а снятие
  // чипа снимает нажатие с кнопки фасета — оба пути меняют один и тот же
  // URL, а не собственное состояние компонента. Находка 3 итоговой рецензии:
  // чип теперь показывает заголовок из фасета («Первая»/«Вторая»), а не
  // голый номер — фикстура ниже несёт оба заголовка ровно для этого.
  it('чип области и кнопка фасета сходятся в обе стороны', async () => {
    const facetPages: SearchPagesResponse = {
      ...pages,
      chapters: [
        { id: 101, title: 'Первая', hits: 5 },
        { id: 102, title: 'Вторая', hits: 2 },
      ],
      chapters_total: 2,
    };
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(facetPages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=xx&works=13&chapters=101');

    await screen.findByText('Первая', { selector: '.scope-chip' });
    expect(screen.getByRole('button', { name: /Первая/ })).toHaveAttribute('aria-pressed', 'true');
    expect(screen.getByRole('button', { name: /Вторая/ })).toHaveAttribute('aria-pressed', 'false');

    // Добавляем вторую главу через фасет — оба чипа обязаны появиться в
    // строке области, и запрос полос уходит с обеими главами.
    await userEvent.click(screen.getByRole('button', { name: /Вторая/ }));
    await screen.findByText('Вторая', { selector: '.scope-chip' });
    expect(screen.getByText('Первая', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(searchApi.pages).toHaveBeenLastCalledWith(
      expect.objectContaining({ chapters: [101, 102] }),
    );
    expect(screen.getByRole('button', { name: /Вторая/ })).toHaveAttribute('aria-pressed', 'true');

    // Снимаем главу 102 повторным нажатием той же кнопки фасета — это и есть
    // ветка снятия внутри toggleChapter (chapters.filter), а не только путь
    // через крестик чипа ниже. Мутация, убирающая эту ветку (toggleChapter
    // всегда добавляет), не ломала тест до этого шага — он проверял только
    // добавление и снятие крестиком, которое снятия через фасет не касается.
    await userEvent.click(screen.getByRole('button', { name: /Вторая/ }));
    await waitFor(() =>
      expect(screen.queryByText('Вторая', { selector: '.scope-chip' })).not.toBeInTheDocument(),
    );
    expect(screen.getByText('Первая', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Вторая/ })).toHaveAttribute('aria-pressed', 'false');
    expect(searchApi.pages).toHaveBeenLastCalledWith(expect.objectContaining({ chapters: [101] }));

    // Возвращаем вторую главу и снимаем главу 101 крестиком чипа — кнопка
    // фасета обязана перестать быть нажатой, а чип 101 — исчезнуть; снятие
    // проверено теперь обоими путями в одном тесте.
    await userEvent.click(screen.getByRole('button', { name: /Вторая/ }));
    await screen.findByText('Вторая', { selector: '.scope-chip' });
    await userEvent.click(
      screen.getByRole('link', { name: 'Снять главу «Первая» из области поиска' }),
    );
    await waitFor(() =>
      expect(screen.queryByText('Первая', { selector: '.scope-chip' })).not.toBeInTheDocument(),
    );
    expect(screen.getByText('Вторая', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(screen.getByRole('button', { name: /Первая/ })).toHaveAttribute('aria-pressed', 'false');
    expect(searchApi.pages).toHaveBeenLastCalledWith(expect.objectContaining({ chapters: [102] }));
  });

  // Круг правок 1, находка 1: фасет менял только одну ось области (главы), а
  // адрес после клика собирался из голого workId — собрание из чипа строки
  // области молча пропадало. Оба теста ниже держат собрание живым по обе
  // стороны переключателя (добавление главы и снятие) — под мутацией
  // `{ editions: [], works: [workId], chapters: next }` вместо
  // `{ ...scope, chapters: next }` они обязаны покраснеть.
  it('клик по фасету не стирает собрание из области — добавление главы', async () => {
    const facetPages: SearchPagesResponse = {
      ...pages,
      chapters: [
        { id: 101, title: 'Первая', hits: 5 },
        { id: 102, title: 'Вторая', hits: 2 },
      ],
      chapters_total: 2,
    };
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(facetPages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    // edition 2 «Собрание 2» несёт том 13 в shelfFixture — сочетание editions
    // + works, которого singleWorkOf теперь не гасит (поправка от 18.09.2026).
    renderAt('/search?q=xx&editions=2&works=13&chapters=101');

    await screen.findByText('Собрание 2', { selector: '.scope-chip' });
    await userEvent.click(screen.getByRole('button', { name: /Вторая/ }));

    await screen.findByText('Вторая', { selector: '.scope-chip' });
    expect(screen.getByText('Собрание 2', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(searchApi.pages).toHaveBeenLastCalledWith(
      expect.objectContaining({ chapters: [101, 102] }),
    );
  });

  it('клик по фасету не стирает собрание из области — снятие главы', async () => {
    const facetPages: SearchPagesResponse = {
      ...pages,
      chapters: [
        { id: 101, title: 'Первая', hits: 5 },
        { id: 102, title: 'Вторая', hits: 2 },
      ],
      chapters_total: 2,
    };
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(facetPages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=xx&editions=2&works=13&chapters=101,102');

    await screen.findByText('Собрание 2', { selector: '.scope-chip' });
    expect(screen.getByRole('button', { name: /Первая/ })).toHaveAttribute('aria-pressed', 'true');

    await userEvent.click(screen.getByRole('button', { name: /Первая/ }));

    await waitFor(() =>
      expect(screen.queryByText('Первая', { selector: '.scope-chip' })).not.toBeInTheDocument(),
    );
    expect(screen.getByText('Собрание 2', { selector: '.scope-chip' })).toBeInTheDocument();
    expect(searchApi.pages).toHaveBeenLastCalledWith(expect.objectContaining({ chapters: [102] }));
  });

  it('старая ссылка ?work= работает как прежде', async () => {
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(pages));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ id: 13, title: 'Том 13', page_offset: 0, numbering_style: 'arabic' } as never),
    );
    renderAt('/search?q=xx&work=13');

    await screen.findByText('Том 13', { selector: '.scope-chip' });
    expect(searchApi.pages).toHaveBeenCalledWith(expect.objectContaining({ work_id: 13 }));
  });

  it('колонцифра считается как везде в читальне: римская у передних листов', async () => {
    // Передние листы: numbering_style = roman, page_offset = -1 (обложка вне
    // счёта). Сервер отдаёт сырую арифметику page_number + page_offset —
    // 8 → 7 и 1 → 0; печатную форму даёт printedFolio, как на карточке
    // полосы, в обрезе тома и в режиме чтения.
    const roman: SearchPagesResponse = {
      ...pages,
      total: 2,
      pages: [
        { ...pages.pages[0], page_number: 8, printed_number: 7 },
        { ...pages.pages[0], page_number: 1, printed_number: 0 },
      ],
    };
    vi.spyOn(searchApi, 'pages').mockImplementation(() => ok(roman));
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({
        id: 21,
        title: 'Передние листы',
        page_offset: -1,
        numbering_style: 'roman',
      } as never),
    );
    renderAt('/search?q=Гегеля&work=21');

    expect(await screen.findByRole('link', { name: 'с. VII' })).toHaveAttribute(
      'href',
      '/works/21/pages/8?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
    // Ниже единицы колонцифры нет вовсе — «б/н», как на карточке полосы.
    expect(screen.getByRole('link', { name: 'б/н' })).toHaveAttribute(
      'href',
      '/works/21/pages/1?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
    expect(screen.queryByText(/с\. 7$/)).not.toBeInTheDocument();
  });

  it('строка тома показывает свои первые полосы с отрывками, без второго запроса', async () => {
    const withPages: SearchResponse = {
      ...first,
      volumes: [volume(2, { text_hits: 5, apparatus_hits: 0, pages: [hit(14), hit(20)] })],
      total_hits: 5,
    };
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(withPages));
    const pagesSpy = vi.spyOn(searchApi, 'pages');
    renderAt('/search?q=Гегеля');

    // Отрывок нарисован текстовыми узлами: HTML полосы не просачивается.
    // Полос две, значит и совпадений помечено два.
    const marks = await screen.findAllByText('Гегеля', { selector: 'mark' });
    expect(marks).toHaveLength(2);
    expect(marks[0]).toHaveClass('search-hit');
    expect(document.querySelector('.search-page-snippet')?.innerHTML).not.toContain('<все>');
    // Полоса открывается сама, тем же адресом, что и из выдачи по тому.
    expect(screen.getByRole('link', { name: 'с. 14' })).toHaveAttribute(
      'href',
      '/works/2/pages/14?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
    // Показаны две из пяти — остальные за ссылкой в режим одного тома.
    expect(screen.getByRole('link', { name: /все 5 полос/ })).toHaveAttribute(
      'href',
      '/search?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F&works=2',
    );
    // Первый экран остаётся одним запросом: полосы приехали вместе с томами.
    expect(pagesSpy).not.toHaveBeenCalled();
  });

  it('ссылки «все N полос» нет, когда показаны все совпавшие полосы', async () => {
    const all: SearchResponse = {
      ...first,
      volumes: [volume(2, { text_hits: 1, apparatus_hits: 1, pages: [hit(14), hit(15)] })],
      total_hits: 1,
    };
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(all));
    renderAt('/search?q=Гегеля');

    expect(await screen.findByRole('link', { name: 'с. 14' })).toBeInTheDocument();
    // Аппарат считается наравне с текстом: обе полосы тома на экране, звать
    // «ещё» некуда. Строка тома сама по себе ведёт туда же и остаётся.
    expect(screen.queryByRole('link', { name: /все \d+ полос/ })).not.toBeInTheDocument();
  });

  it('колонцифра в списке томов считается как везде: римская у передних листов', async () => {
    // numbering_style едет в самом томе, printed_number — сырая арифметика
    // сервера (page_offset уже применён): 8 → VII, 1 → «б/н».
    const roman: SearchResponse = {
      ...first,
      chapters: [],
      concepts: [],
      volumes: [
        volume(21, {
          role: 'front_matter',
          parent_work_id: 2,
          title: 'Передние листы',
          numbering_style: 'roman',
          text_hits: 2,
          apparatus_hits: 0,
          pages: [hit(8, { printed_number: 7 }), hit(1, { printed_number: 0 })],
        }),
      ],
      total_hits: 2,
    };
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(roman));
    renderAt('/search?q=Гегеля');

    expect(await screen.findByRole('link', { name: 'с. VII' })).toHaveAttribute(
      'href',
      '/works/21/pages/8?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F',
    );
    expect(screen.getByRole('link', { name: 'б/н' })).toBeInTheDocument();
    expect(screen.queryByText(/с\. 7$/)).not.toBeInTheDocument();
  });

  // Форма такой запрос не пропустит, но по прямому адресу её никто не
  // спрашивает: ссылку можно прислать, набрать руками или сохранить в
  // закладках. Проверка на странице — не дубль формы, а единственная,
  // которая тут работает.
  it('короткий запрос из адреса не уходит в API и объясняет отказ', () => {
    // Ответ подставлен намеренно, хотя его никто не ждёт: без него регрессия
    // ушла бы в настоящий axios и тест упал бы сетевой ошибкой, а не той
    // проверкой, ради которой написан.
    const spy = vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=и');
    expect(screen.getByText(/не меньше двух/i)).toBeInTheDocument();
    expect(spy).not.toHaveBeenCalled();
  });

  it('под формой стоит ссылка на справку о поиске', async () => {
    vi.spyOn(searchApi, 'search').mockImplementation(() => ok(first));
    renderAt('/search?q=Гегеля');
    await screen.findByRole('link', { name: 'О Гегеле' });
    expect(screen.getByRole('link', { name: /как искать/i })).toHaveAttribute(
      'href',
      '/help#search',
    );
  });

  it('пустая выдача говорит об этом словами', async () => {
    vi.spyOn(searchApi, 'search').mockImplementation(() =>
      ok({ ...first, chapters: [], concepts: [], volumes: [], total_hits: 0 }),
    );
    renderAt('/search?q=абракадабра');
    expect(await screen.findByText(/ничего не найдено/i)).toBeInTheDocument();
  });
});
