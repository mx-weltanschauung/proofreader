import { describe, it, expect, vi, beforeEach, afterEach } from 'vitest';
import { render, screen, waitFor } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, useLocation } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import { conceptsApi } from '../services/api';
import type { Concept, ConceptArticle, ConceptEntry, ConceptReference } from '../types';
import { groupByRubric, referenceLabel } from '../utils/conceptReferences';
import { encodeRubricPath } from '../utils/rubricPathParam';
import { ConceptView } from './ConceptView';
import { shareFrom, stubShare, unstubShare } from '../test/shareStub';

function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

function ref(over: Partial<ConceptReference>): ConceptReference {
  return {
    id: 1,
    article_id: 1,
    volume_number: 4,
    page_start: 85,
    page_end: 85,
    rubric: 'определение',
    order_number: 1,
    is_uncertain: false,
    resolved: false,
    ...over,
  };
}

// Запись потока — единица нового API. reference_id связывает запись с
// адресом указателя (ConceptReference.id) — по нему панель узнаёт, что
// адрес уже загружен, и куда должна вести её ссылка на якорь.
function entry(over: Partial<ConceptEntry>): ConceptEntry {
  return {
    reference_id: 1,
    volume_number: 1,
    printed_start: 233,
    printed_end: 233,
    work_id: 1,
    work_title: 'Из ранних произведений',
    chapter_title: 'К критике гегелевской философии права',
    chapter_id: null,
    rubric: 'определение',
    rubric_path: ['определение'],
    is_uncertain: false,
    state: 'fragment',
    pages: [{ page_id: 501, page_number: 220, printed_page: 233, page_status: 'не_вычитана' }],
    cuts: [
      {
        id: 1,
        bounds: null,
        status: 'machine',
        head_quote: 'тело страницы 233',
        parts: [
          {
            page_id: 501,
            page_number: 220,
            printed_page: 233,
            page_status: 'не_вычитана',
            html: '<p>тело страницы 233</p>',
          },
        ],
      },
    ],
    stale_cuts: [],
    ...over,
  };
}

const ARTICLE: ConceptArticle = {
  id: 1,
  edition_id: 1,
  edition_title: 'Сочинения',
  work_id: 90,
  source_url: '',
  title: 'Абстракция, абстрактное и конкретное',
  kind: 'article',
  article_markdown: '— абстракция и действительность — **1**, 233—235; **12**, 714',
  source_page_start: 11,
  source_page_end: 12,
  references: [
    ref({
      id: 1,
      volume_number: 1,
      page_start: 233,
      page_end: 235,
      resolved: true,
      work_id: 1,
      page_number: 220,
    }),
    ref({ id: 2, volume_number: 12, page_start: 714, page_end: 714, resolved: false }),
  ],
  links: [],
};

const CONCEPT: Concept = {
  id: 1,
  title: 'Абстракция, абстрактное и конкретное',
  slug: 'abstrakciya',
  sort_key: 'абстракция',
  articles: [ARTICLE],
  created_at: '',
  updated_at: '',
};

// Зонд текущего URL: требование «дефолт не пишется в URL» проверяется по
// адресной строке, а не по аргументам запроса — те совпадают в обоих случаях
// (см. requestParams в useConceptFragments.ts), так что одних их недостаточно.
function LocationSearchProbe() {
  const location = useLocation();
  return <span data-testid="location-search">{location.search}</span>;
}

function renderConcept() {
  return render(
    <MemoryRouter initialEntries={['/concepts/abstrakciya']}>
      <Routes>
        <Route path="/concepts/:slug" element={<ConceptView />} />
      </Routes>
    </MemoryRouter>,
  );
}

describe('referenceLabel', () => {
  it('подписывает том, полутом и диапазон печатными номерами', () => {
    expect(referenceLabel(ref({ volume_number: 4, page_start: 85, page_end: 85 }))).toBe(
      'т. 4 · с. 85',
    );
    expect(
      referenceLabel(ref({ volume_number: 26, volume_part: 'II', page_start: 448, page_end: 449 })),
    ).toBe('т. 26, II · с. 448—449');
  });
});

describe('groupByRubric', () => {
  it('группирует по подрубрике, сохраняя порядок order_number', () => {
    const groups = groupByRubric([
      ref({ id: 1, rubric: 'определение', order_number: 2 }),
      ref({ id: 2, rubric: 'его мера', order_number: 3 }),
      ref({ id: 3, rubric: 'определение', order_number: 1 }),
    ]);

    expect(groups.map((g) => g.rubric)).toEqual(['определение', 'его мера']);
    expect(groups[0].refs.map((r) => r.id)).toEqual([3, 1]);
  });
});

describe('ConceptView', () => {
  beforeEach(() => {
    // Поток фрагментов запрашивается для любой статьи — по умолчанию отдаём
    // пустой результат, чтобы тесты, которые проверяют другое, не делали
    // настоящий сетевой запрос. Тесты, которым нужен конкретный ответ,
    // переопределяют этот же спай своим mockImplementation.
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok({ total: 0, entries: [] }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
  });

  it('ведёт резолвнутый адрес на закреплённый в потоке блок страницы, когда страница уже загружена', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    // Страница адреса (work_id: 1, page_number: 220) пришла в потоке — адрес
    // должен прыгать на её якорь внутри документа.
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() =>
      ok({
        total: 1,
        entries: [entry({})],
      }),
    );

    renderConcept();

    // Ждём, пока поток действительно докатится (иначе можно поймать
    // промежуточный рендер, где панель ещё не знает о загруженном адресе).
    await screen.findByText(/тело страницы 233/);

    expect(screen.getByRole('link', { name: /т\. 1 · с\. 233—235/ })).toHaveAttribute(
      'href',
      '#frag-ref-1',
    );
  });

  it('ведёт резолвнутый адрес на страницу тома, пока его страница ещё не пришла в потоке', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    // conceptsApi.fragments по умолчанию (см. beforeEach) отдаёт пустой
    // поток — адрес ещё не загружен, якоря в документе нет.

    renderConcept();

    expect(await screen.findByRole('link', { name: /т\. 1 · с\. 233—235/ })).toHaveAttribute(
      'href',
      '/works/1/pages/220',
    );
  });

  it('ссылка панели ведёт на якорь адреса, а не страницы', async () => {
    // Два адреса на одну страницу — обычное дело: якорь по странице увёл бы
    // обе ссылки в одно место.
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      ok({
        ...CONCEPT,
        articles: [
          {
            ...ARTICLE,
            references: [
              ref({ id: 473, resolved: true, work_id: 1, page_number: 220 }),
              ref({ id: 541, resolved: true, work_id: 1, page_number: 220 }),
            ],
          },
        ],
      }),
    );
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() =>
      ok({
        total: 2,
        entries: [entry({ reference_id: 473 }), entry({ reference_id: 541 })],
      }),
    );

    const { container } = renderConcept();

    await waitFor(() => expect(container.querySelector('#frag-ref-473')).not.toBeNull());
    expect(container.querySelector('a[href="#frag-ref-473"]')).not.toBeNull();
    expect(container.querySelector('a[href="#frag-ref-541"]')).not.toBeNull();
  });

  it('нерезолвнутый адрес показан текстом с пометкой «том не загружен», статья видна рядом', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));

    renderConcept();

    await screen.findByRole('link', { name: /т\. 1/ });
    // Нерезолвнутый адрес по-прежнему не ссылка — том не загружен, прыгать
    // некуда.
    expect(screen.queryByRole('link', { name: /т\. 12/ })).not.toBeInTheDocument();
    expect(screen.getByText(/т\. 12 · с\. 714/)).toBeInTheDocument();
    expect(screen.getByText('— том не загружен')).toBeInTheDocument();
    // Печатная статья указателя остаётся видна рядом с адресами.
    expect(screen.getByRole('heading', { name: 'Статья указателя' })).toBeInTheDocument();
  });

  it('кнопка «Спросить нейросеть» кладёт ссылку на текст понятия', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok({ total: 0, entries: [] }));
    const writeText = vi.fn(async (_text: string) => {});
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });

    renderConcept();
    await userEvent.click(await screen.findByRole('button', { name: 'Спросить нейросеть' }));

    expect(writeText.mock.calls[0][0]).toContain(
      `${window.location.origin}/concepts/${CONCEPT.slug}.md`,
    );
  });

  it('при выбранной подрубрике кнопка ведёт на текст подрубрики', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok({ total: 0, entries: [] }));
    const writeText = vi.fn(async (_text: string) => {});
    Object.defineProperty(navigator, 'clipboard', { configurable: true, value: { writeText } });
    const query = new URLSearchParams({ rubric_path: encodeRubricPath(['определение']) });

    render(
      <MemoryRouter initialEntries={[`/concepts/abstrakciya?${query}`]}>
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );
    await userEvent.click(await screen.findByRole('button', { name: 'Спросить нейросеть' }));

    expect(writeText.mock.calls[0][0]).toContain(
      `${window.location.origin}/concepts/${CONCEPT.slug}.md?${query}\n`,
    );
  });

  it('показывает охват шапкой: сколько фрагментов доступно из скольких адресов', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok({ total: 1, entries: [] }));

    renderConcept();

    expect(await screen.findByText('доступно 1 фрагмент из 2 адресов')).toBeInTheDocument();
  });

  it('помечает сомнительный адрес и показывает note', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      ok({
        ...CONCEPT,
        articles: [
          {
            ...ARTICLE,
            references: [
              ref({
                id: 5,
                resolved: true,
                work_id: 4,
                page_number: 85,
                is_uncertain: true,
                note: 'точка вместо запятой',
              }),
            ],
          },
        ],
      }),
    );

    renderConcept();

    expect(await screen.findByTitle('точка вместо запятой')).toBeInTheDocument();
  });

  it('у отсылочной статьи панели адресов нет', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      ok({
        ...CONCEPT,
        articles: [
          {
            ...ARTICLE,
            kind: 'redirect' as const,
            references: [],
            links: [
              {
                id: 1,
                from_article_id: 1,
                target_title: 'Относительность',
                target_slug: 'otnositelnost',
                kind: 'see' as const,
                order_number: 1,
              },
            ],
          },
        ],
      }),
    );

    renderConcept();

    expect(await screen.findByRole('link', { name: 'Относительность' })).toHaveAttribute(
      'href',
      '/concepts/otnositelnost',
    );
    expect(screen.queryByRole('heading', { name: 'Адреса' })).not.toBeInTheDocument();
  });

  it('отсылку без слага показывает текстом', async () => {
    // Цель ещё не разобрана или лежит во втором указателе — по спеке
    // бэкенда это нормальное состояние, а не ошибка.
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      ok({
        ...CONCEPT,
        articles: [
          {
            ...ARTICLE,
            links: [
              {
                id: 1,
                from_article_id: 1,
                target_title: 'Труд',
                kind: 'see_also' as const,
                order_number: 1,
              },
            ],
          },
        ],
      }),
    );

    renderConcept();

    expect(await screen.findByText('Труд')).toBeInTheDocument();
    expect(screen.queryByRole('link', { name: 'Труд' })).not.toBeInTheDocument();
  });

  it('ведёт на скан статьи в указателе', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));

    renderConcept();

    expect(await screen.findByRole('link', { name: /Указатель, стр\. 11—12/ })).toHaveAttribute(
      'href',
      '/works/90/pages/11',
    );
  });

  it('404 показывает сообщение и путь в навигатор', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      Promise.reject({ response: { status: 404 } }),
    );

    renderConcept();

    expect(await screen.findByText('Понятие не найдено')).toBeInTheDocument();
    expect(screen.getByRole('link', { name: /К указателю/ })).toHaveAttribute('href', '/concepts');
  });

  it('показывает поток текстов рядом с панелью адресов', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() =>
      ok({
        total: 1,
        entries: [entry({})],
      }),
    );

    renderConcept();

    expect(await screen.findByText(/тело страницы 233/)).toBeTruthy();
    expect(screen.getByText('т. 1 · с. 233')).toBeTruthy();
    expect(screen.getByText(/Из ранних произведений/)).toBeTruthy();
  });

  it('передаёт фильтры из URL в запрос фрагментов', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok({ total: 0, entries: [] }));

    render(
      <MemoryRouter initialEntries={['/concepts/abstrakciya?rubric=определение&volume=12']}>
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(fragments).toHaveBeenCalledWith('abstrakciya', {
        rubric: 'определение',
        volume: 12,
        limit: 20,
        offset: 0,
      }),
    );
  });

  it('rubric_path из URL уезжает в запрос фрагментов', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok({ total: 0, entries: [] }));
    const param = encodeRubricPath(['II съезд РСДРП', 'значение съезда']);

    render(
      <MemoryRouter initialEntries={[`/concepts/abstrakciya?rubric_path=${param}`]}>
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(fragments).toHaveBeenCalledWith('abstrakciya', {
        rubric_path: param,
        limit: 20,
        offset: 0,
      }),
    );
  });

  it('старый ?rubric= из URL по-прежнему уезжает плоским и без пути', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok({ total: 0, entries: [] }));

    render(
      <MemoryRouter initialEntries={['/concepts/abstrakciya?rubric=определение']}>
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );

    // Точное сравнение объекта, а не objectContaining: rubric_path не должен
    // появиться в запросе ВООБЩЕ — иначе чужая закладка поехала бы по другой
    // ветке сервера. Форма ожидания взята у соседнего теста «передаёт фильтры
    // из URL», который сравнивает объект целиком.
    await waitFor(() =>
      expect(fragments).toHaveBeenCalledWith('abstrakciya', {
        rubric: 'определение',
        limit: 20,
        offset: 0,
      }),
    );
  });

  it('выбор подрубрики пишет путь в адрес и убирает старый плоский параметр', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    // Показыватель адреса — тот же приём, что у теста «переключение порядка
    // пишет его в URL» ниже в этом файле (useLocation уже импортирован).
    const Location = () => <span data-testid="loc">{useLocation().search}</span>;

    render(
      <MemoryRouter initialEntries={['/concepts/abstrakciya?rubric=определение']}>
        <Routes>
          <Route
            path="/concepts/:slug"
            element={
              <>
                <ConceptView />
                <Location />
              </>
            }
          />
        </Routes>
      </MemoryRouter>,
    );

    // CONCEPT несёт единственную подрубрику — «определение» (обе ссылки
    // ARTICLE.references используют дефолт ref()); «его мера» в этой фикстуре
    // нет, она встречается только в отдельном наборе теста groupByRubric выше
    // в этом файле.
    const select = await screen.findByLabelText('подрубрика');
    await userEvent.selectOptions(select, encodeRubricPath(['определение']));

    const params = new URLSearchParams(screen.getByTestId('loc').textContent ?? '');
    expect(params.get('rubric_path')).toBe(encodeRubricPath(['определение']));
    expect(params.has('rubric')).toBe(false);
  });

  it('порядок из URL уезжает в запрос фрагментов', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok({ total: 0, entries: [] }));

    render(
      <MemoryRouter initialEntries={['/concepts/abstrakciya?order=page']}>
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );

    await waitFor(() =>
      expect(fragments).toHaveBeenCalledWith('abstrakciya', {
        order: 'page',
        limit: 20,
        offset: 0,
      }),
    );
  });

  it('переключение порядка пишет его в URL, а дефолт из URL убирает', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok({ total: 0, entries: [] }));

    render(
      <MemoryRouter initialEntries={['/concepts/abstrakciya']}>
        <LocationSearchProbe />
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );
    await screen.findByRole('button', { name: 'по томам' });
    expect(screen.getByTestId('location-search')).toHaveTextContent('');

    await userEvent.click(screen.getByRole('button', { name: 'по томам' }));
    await waitFor(() =>
      expect(fragments).toHaveBeenLastCalledWith('abstrakciya', {
        order: 'page',
        limit: 20,
        offset: 0,
      }),
    );
    // URL действительно несёт нестандартный порядок, а не только запрос.
    expect(screen.getByTestId('location-search').textContent).toBe('?order=page');

    await userEvent.click(screen.getByRole('button', { name: 'по рубрикам' }));
    await waitFor(() =>
      expect(fragments).toHaveBeenLastCalledWith('abstrakciya', { limit: 20, offset: 0 }),
    );
    // Дефолт убирает параметр из URL целиком, а не пишет его явным значением.
    expect(screen.getByTestId('location-search').textContent).toBe('');
  });

  it('у отсылки потока нет вовсе', async () => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      ok({
        ...CONCEPT,
        articles: [{ ...ARTICLE, kind: 'redirect' as const, references: [], article_markdown: '' }],
      }),
    );
    const fragments = vi
      .spyOn(conceptsApi, 'fragments')
      .mockImplementation(() => ok({ total: 0, entries: [] }));

    renderConcept();

    await screen.findByText('Абстракция, абстрактное и конкретное');
    expect(fragments).not.toHaveBeenCalled();
  });

  it('у понятия с несколькими статьями каждая подписана своим изданием', async () => {
    const secondArticle: ConceptArticle = {
      ...ARTICLE,
      id: 2,
      edition_id: 2,
      edition_title: 'Философское наследие',
      references: [ref({ id: 9, article_id: 2, volume_number: 5, rubric: 'см. также' })],
    };
    vi.spyOn(conceptsApi, 'get').mockImplementation(() =>
      ok({ ...CONCEPT, articles: [ARTICLE, secondArticle] }),
    );

    renderConcept();

    expect(await screen.findByText('Сочинения')).toBeInTheDocument();
    expect(screen.getByText('Философское наследие')).toBeInTheDocument();
  });
});

describe('ConceptView: Поделиться', () => {
  beforeEach(() => {
    vi.spyOn(conceptsApi, 'get').mockImplementation(() => ok(CONCEPT));
    vi.spyOn(conceptsApi, 'fragments').mockImplementation(() => ok({ total: 0, entries: [] }));
  });

  afterEach(() => {
    vi.restoreAllMocks();
    unstubShare();
  });

  function renderAt(url: string) {
    return render(
      <MemoryRouter initialEntries={[url]}>
        <Routes>
          <Route path="/concepts/:slug" element={<ConceptView />} />
        </Routes>
      </MemoryRouter>,
    );
  }

  it('отдаёт понятие с подписью «предметный указатель»', async () => {
    const share = stubShare();
    renderAt('/concepts/abstrakciya');
    await screen.findByRole('heading', { name: CONCEPT.title });
    const data = await shareFrom(share);
    expect(data.title).toBe('Абстракция, абстрактное и конкретное — предметный указатель');
    expect(data.url).toBe(`${window.location.origin}/concepts/abstrakciya`);
  });

  it('несёт фильтр экрана и подрубрику в подписи, а чужие параметры отбрасывает', async () => {
    const share = stubShare();
    const param = encodeRubricPath(['II съезд РСДРП', 'значение съезда']);
    // Адрес собран так же, как его пишет сам экран (setParams): значение
    // подрубрики кодируется второй раз — см. шапку rubricPathParam.ts.
    const query = new URLSearchParams({
      rubric_path: param,
      volume: '12',
      order: 'page',
      q: 'съезд',
    });
    renderAt(`/concepts/abstrakciya?${query}`);
    await screen.findByRole('heading', { name: CONCEPT.title });
    const data = await shareFrom(share);
    expect(data.title).toBe(
      'Абстракция, абстрактное и конкретное: II съезд РСДРП / значение съезда — предметный указатель',
    );
    const url = new URL(data.url ?? '');
    expect(url.pathname).toBe('/concepts/abstrakciya');
    expect(url.searchParams.get('rubric_path')).toBe(param);
    expect(url.searchParams.get('volume')).toBe('12');
    expect(url.searchParams.get('order')).toBe('page');
    expect(url.searchParams.has('q')).toBe(false);
  });

  it('несёт и старый плоский ?rubric=', async () => {
    const share = stubShare();
    renderAt('/concepts/abstrakciya?rubric=определение');
    await screen.findByRole('heading', { name: CONCEPT.title });
    const data = await shareFrom(share);
    expect(data.title).toBe(
      'Абстракция, абстрактное и конкретное: определение — предметный указатель',
    );
    expect(new URL(data.url ?? '').searchParams.get('rubric')).toBe('определение');
  });
});
