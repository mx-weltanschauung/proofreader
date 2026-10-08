import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, waitFor, within } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter, Routes, Route, Link } from 'react-router-dom';
import type { AxiosResponse } from 'axios';
import type { Chapter, Work, Page } from '../types';
import { chaptersApi, conceptsApi, pagesApi, searchApi, worksApi } from '../services/api';

// KaTeX autorender ходит по реальному DOM и к делу здесь не относится.
vi.mock('katex/dist/contrib/auto-render', () => ({ default: vi.fn() }));

import { PageView } from './PageView';

// Ответы axios несут status/headers/config; компонент читает только .data.
function ok<T>(data: T): Promise<AxiosResponse<T>> {
  return Promise.resolve({ data } as unknown as AxiosResponse<T>);
}

// page_offset: 0 — обычный том, колонцифра совпадает с номером страницы.
// Без этого поля printedFolio считает `page_number + undefined = NaN`, и на
// экране всплывает «стр. NaN» — ровно тот дефект, что уже раз ловили в
// ChapterView.test.tsx.
const WORK = { id: 3, title: 'Том 3', author: 'Автор', page_offset: 0 } as Work;

const PAGE_245 = { id: 1001, work_id: 3, page_number: 245, status: 'вычитана' } as Page;
const PAGE_246 = { id: 1002, work_id: 3, page_number: 246, status: 'вычитана' } as Page;

const HTML_245 = 'Так писал он в тысяча восемьсот сорок восьмом.';
const HTML_246 = 'Наутро всё переменилось.';

describe('PageView', () => {
  beforeEach(() => {
    vi.clearAllMocks();
    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(WORK));
    vi.spyOn(chaptersApi, 'list').mockImplementation(() => ok([] as Chapter[]));
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation((_workId: number, pageNumber: number) =>
      ok(pageNumber === 245 ? PAGE_245 : PAGE_246),
    );
    vi.spyOn(conceptsApi, 'forPage').mockImplementation(() => ok([]));
    vi.spyOn(worksApi, 'pageMap').mockImplementation(() =>
      ok([
        { page_number: 245, status: 'вычитана' },
        { page_number: 246, status: 'вычитана' },
      ]),
    );
  });

  function renderAt(path: string) {
    return render(
      <MemoryRouter initialEntries={[path]}>
        {/* Навигация на соседнюю страницу: сам PageView такой ссылки не даёт. */}
        <Link to="/works/3/pages/246">К 246</Link>
        <Routes>
          <Route path="/works/:workId/pages/:pageNumber" element={<PageView />} />
        </Routes>
      </MemoryRouter>,
    );
  }

  // Выдача поиска ведёт на полосу ссылкой с ?q=. Если бы полоса не умела
  // подсвечивать, переезд ссылки с окна потока на саму полосу молча выключил
  // бы подсветку — то единственное, ради чего q в этой ссылке и едет.
  it('подсвечивает найденное по ?q= в адресе', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation(() =>
      ok({ html: '<p>Гегеля читали, Гегелем восхищались, а ёлка стояла.</p>' }),
    );
    vi.spyOn(searchApi, 'terms').mockImplementation(() =>
      ok({ query: 'Гегеля', terms: ['гегел', 'елк'] }),
    );

    renderAt('/works/3/pages/245?q=%D0%93%D0%B5%D0%B3%D0%B5%D0%BB%D1%8F');

    await waitFor(() => {
      const marks = [...document.querySelectorAll('mark.search-hit')].map((m) => m.textContent);
      expect(marks).toEqual(['Гегеля', 'Гегелем', 'ёлка']);
    });
  });

  // Внешняя ссылка на место в тексте (вырезка указателя, задача 14) ведёт
  // ссылкой с ?quote= — дословной цитатой, а не леммами.
  it('подсвечивает найденное по ?quote= и не показывает полоску', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation(() =>
      ok({ html: '<p>Так писал он в тысяча восемьсот сорок восьмом.</p>' }),
    );

    renderAt('/works/3/pages/245?quote=' + encodeURIComponent('тысяча восемьсот сорок восьмом'));

    await waitFor(() => {
      expect(document.querySelector('mark.quote-hit')?.textContent).toBe(
        'тысяча восемьсот сорок восьмом',
      );
    });
    expect(document.querySelector('.quote-notice')).toBeNull();
  });

  // Промах — самый важный случай: текст полосы с тех пор поправили, и без
  // вслух сказанного предупреждения читатель решил бы, что ссылка просто не
  // годится.
  it('промах по ?quote= говорит об этом вслух и называет дату правки', async () => {
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() =>
      ok({ ...PAGE_245, text_edited_at: '2026-09-12T10:00:00Z' }),
    );
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245?quote=' + encodeURIComponent('этого не было на полосе'));

    expect(await screen.findByRole('alert')).toHaveTextContent(/не нашлась/);
    expect(await screen.findByRole('alert')).toHaveTextContent(/12 сентября 2026/);
  });

  it('без ?quote= в адресе ничего не сравнивает', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    expect(await screen.findByText(HTML_245)).toBeInTheDocument();
    expect(document.querySelector('mark.quote-hit')).toBeNull();
    expect(document.querySelector('.quote-notice')).toBeNull();
  });

  it('без ?q= в адресе ничего не подсвечивает и лемм не запрашивает', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: '<p>Гегеля читали</p>' }));
    const terms = vi.spyOn(searchApi, 'terms');

    renderAt('/works/3/pages/245');

    expect(await screen.findByText('Гегеля читали')).toBeInTheDocument();
    expect(document.querySelector('mark.search-hit')).toBeNull();
    expect(terms).not.toHaveBeenCalled();
  });

  it('показывает дату последней правки текста, когда она известна', async () => {
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() =>
      ok({ ...PAGE_245, text_edited_at: '2026-01-14T00:00:00Z' }),
    );
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    expect(await screen.findByText('Правлено:')).toBeInTheDocument();
    expect(screen.getByText(/14 января 2026/)).toBeInTheDocument();
  });

  it('без даты правки строку «Правлено» не показывает', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    await screen.findByText(HTML_245);
    expect(screen.queryByText('Правлено:')).toBeNull();
  });

  it('показывает содержимое страницы', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    expect(await screen.findByText(HTML_245)).toBeInTheDocument();
  });

  it('зовёт предложить исправление и без входа в читальню', async () => {
    // Точка входа обязана быть видна анониму: ради него всё и делалось.
    // useAuth в этом файле не подменяется — по умолчанию пользователь не
    // залогинен, а ссылка обязана быть видна и в этом состоянии.
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    const link = await screen.findByRole('link', { name: /предложить исправление/i });
    expect(link).toHaveAttribute('href', '/works/3/pages/245/suggest');
  });

  it('не показывает текст прошлой страницы, пока не пришёл рендер новой', async () => {
    // Рендер 245 приходит сразу, рендер 246 зависает: это и есть окно, в
    // котором номер страницы уже новый, а содержимое ещё старое.
    vi.spyOn(pagesApi, 'render')
      .mockImplementationOnce(() => ok({ html: `<p>${HTML_245}</p>` }))
      .mockImplementation(() => new Promise<AxiosResponse<{ html: string }>>(() => {}));

    renderAt('/works/3/pages/245');
    expect(await screen.findByText(HTML_245)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('link', { name: 'К 246' }));

    // Страница 246 уже отрезолвилась, но её html ещё в пути. Пока это так,
    // экран обязан быть в состоянии загрузки, а не показывать текст 245-й.
    await waitFor(() => expect(screen.getByText('Загрузка страницы…')).toBeInTheDocument());
    expect(screen.queryByText(HTML_245)).not.toBeInTheDocument();

    // И убеждаемся, что резолв номера действительно произошёл: иначе тест
    // проверял бы лишь сброс usePageByNumber, а не отсечение чужого контента.
    await waitFor(() => expect(pagesApi.render).toHaveBeenCalledWith(3, PAGE_246.id));
  });

  it('показывает содержимое новой страницы, когда её рендер пришёл', async () => {
    vi.spyOn(pagesApi, 'render').mockImplementation((_workId: number, pageId: number) =>
      ok({ html: pageId === PAGE_245.id ? `<p>${HTML_245}</p>` : `<p>${HTML_246}</p>` }),
    );

    renderAt('/works/3/pages/245');
    expect(await screen.findByText(HTML_245)).toBeInTheDocument();

    await userEvent.click(screen.getByRole('link', { name: 'К 246' }));

    expect(await screen.findByText(HTML_246)).toBeInTheDocument();
    expect(screen.queryByText(HTML_245)).not.toBeInTheDocument();
  });

  it('показывает печатный номер страницы у работы с координатами тома', async () => {
    // печатная = page_number + page_offset, то есть 245 + 12. Указатель
    // адресует именно печатный номер, и сверять его человеку надо здесь же.
    vi.spyOn(worksApi, 'get').mockImplementation(() =>
      ok({ ...WORK, volume_number: 3, page_offset: 12 } as Work),
    );
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    expect(await screen.findByText(/печатная стр\. 257/)).toBeInTheDocument();
  });

  it('без координат тома печатный номер не показывает', async () => {
    // WORK из фикстуры — работа без volume_number: таких в проекте сегодня
    // большинство, и лишней строки в карточке у них быть не должно.
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    await screen.findByText(HTML_245);
    expect(screen.queryByText(/печатная стр\./)).not.toBeInTheDocument();
  });

  it('показывает печатную колонцифру рядом с номером страницы', async () => {
    // Передние листы: page_number 15 при offset −1 — это римская XIV,
    // ровно то, что напечатано в колонтитуле половинки 15 тома 5.
    const frontMatterWork = { ...WORK, page_offset: -1, numbering_style: 'roman' } as Work;
    const page15 = { ...PAGE_245, page_number: 15 } as Page;

    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(frontMatterWork));
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() => ok(page15));
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/15');

    expect(await screen.findByText(/стр\. XIV/i)).toBeInTheDocument();
  });

  it('не показывает колонцифру, когда она совпадает с номером страницы', async () => {
    // WORK — обычный том с page_offset: 0: печатный номер и адресный
    // совпадают, дублировать на экране нечего. volume_number у фикстуры нет,
    // поэтому строка «печатная стр.» из карточки тома тоже не мешает.
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    expect(await screen.findByText(HTML_245)).toBeInTheDocument();
    // Скоуп на карточку страницы: пейджер над колонками легитимно показывает
    // своё «стр. N из M» и попал бы под общий /стр\./i, если проверять весь
    // экран целиком.
    const infoCard = within(document.querySelector('.page-info-card') as HTMLElement);
    expect(infoCard.queryByText(/стр\./i)).not.toBeInTheDocument();
    expect(infoCard.queryByText(/б\/н/i)).not.toBeInTheDocument();
  });

  it('показывает «б/н» у страницы вне печатного счёта', async () => {
    // Обложка передних листов: offset −1 и номер страницы 1 дают 1 + (−1) = 0,
    // printedFolio возвращает null — колонцифры на скане нет вовсе.
    const frontMatterWork = { ...WORK, page_offset: -1, numbering_style: 'roman' } as Work;
    const coverPage = { ...PAGE_245, page_number: 1 } as Page;

    vi.spyOn(worksApi, 'get').mockImplementation(() => ok(frontMatterWork));
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() => ok(coverPage));
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/1');

    expect(await screen.findByText(/б\/н/i)).toBeInTheDocument();
  });

  it('подписи на русском', async () => {
    // Скан рендерится только при наличии preview_url/preview_path — без него
    // заголовок «Скан страницы» не появится вовсе.
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation(() =>
      ok({ ...PAGE_245, preview_url: 'https://example.test/scan-245.png' } as Page),
    );
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    expect(await screen.findByRole('heading', { name: 'Содержимое' })).toBeInTheDocument();
    expect(screen.getByRole('heading', { name: 'Скан страницы' })).toBeInTheDocument();
    expect(screen.getByText('Работа:')).toBeInTheDocument();
    expect(screen.getByText('Статус:')).toBeInTheDocument();
  });

  it('называет статус страницы, а не показывает пустую плашку', async () => {
    // Фикстуры файла все со статусом «вычитана» — именно на нём
    // readerStatusLabel молчит; карточка страницы обязана называть его,
    // иначе на экране остаётся пустой зелёный прямоугольник.
    vi.spyOn(pagesApi, 'render').mockImplementation(() => ok({ html: `<p>${HTML_245}</p>` }));

    renderAt('/works/3/pages/245');

    const infoCard = within((await screen.findByText('Статус:')).closest('.meta-item')!);
    expect(infoCard.getByText('вычитана')).toBeInTheDocument();
  });

  it('не листает стрелками, пока открыт полноэкранный просмотр скана', async () => {
    vi.spyOn(pagesApi, 'getByNumber').mockImplementation((_workId: number, pageNumber: number) =>
      ok({
        ...(pageNumber === 245 ? PAGE_245 : PAGE_246),
        preview_url: 'https://example.test/scan-245.png',
      } as Page),
    );
    vi.spyOn(pagesApi, 'render').mockImplementation((_workId: number, pageId: number) =>
      ok({ html: pageId === PAGE_245.id ? `<p>${HTML_245}</p>` : `<p>${HTML_246}</p>` }),
    );

    renderAt('/works/3/pages/245');
    await screen.findByText(HTML_245);

    await userEvent.click(screen.getByRole('button', { name: 'Во весь экран' }));
    expect(screen.getByRole('dialog')).toBeInTheDocument();

    // Стрелка в открытом оверлее не должна перевести на соседнюю страницу —
    // возить ею скан должна только сама ScanViewer.
    await userEvent.keyboard('{ArrowRight}');
    expect(screen.queryByText(HTML_246)).not.toBeInTheDocument();
    expect(screen.getByText(HTML_245)).toBeInTheDocument();

    // После закрытия оверлея стрелки снова листают.
    await userEvent.keyboard('{Escape}');
    expect(screen.queryByRole('dialog')).toBeNull();
    await userEvent.keyboard('{ArrowRight}');
    expect(await screen.findByText(HTML_246)).toBeInTheDocument();
  });
});
