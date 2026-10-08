import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen } from '@testing-library/react';
import userEvent from '@testing-library/user-event';
import { MemoryRouter } from 'react-router-dom';
import type { ChapterPage, Work } from '../types';
import { ReadingSurface } from './ReadingSurface';
import { ReadingPreferencesProvider } from '../contexts/ReadingPreferencesContext';

vi.mock('katex/dist/contrib/auto-render', () => ({ default: () => {} }));

// IntersectionObserver в тестовом окружении — заглушка (src/test/setup.ts),
// поэтому настоящий useVisiblePage всегда возвращает null. Мок позволяет
// задать видимую страницу напрямую — тот же приём, что и в ChapterView.test.tsx.
const { visiblePageMock } = vi.hoisted(() => ({
  visiblePageMock: vi.fn((_enabled: boolean, _sectionsKey: unknown) => null as number | null),
}));
vi.mock('../hooks/useVisiblePage', () => ({
  useVisiblePage: (enabled: boolean, sectionsKey: unknown) => visiblePageMock(enabled, sectionsKey),
}));

// Полоса в форме провода: сервер шлёт вёрстку и бит «пустая», без markdown.
function page(page_number: number, html: string, blank = false): ChapterPage {
  return { page_number, html, blank };
}

function renderSurface(
  pages: ChapterPage[],
  props: Partial<React.ComponentProps<typeof ReadingSurface>> = {},
  entry = '/works/41/chapters/7',
) {
  return render(
    <MemoryRouter initialEntries={[entry]}>
      <ReadingPreferencesProvider>
        <ReadingSurface
          pages={pages}
          footnotesHtml="<div>сноска</div>"
          work={{ id: 41, page_offset: 4 }}
          header={<h1>Заголовок</h1>}
          pageHref={(n) => `#chapter-page-${n}`}
          {...props}
        />
      </ReadingPreferencesProvider>
    </MemoryRouter>,
  );
}

describe('ReadingSurface', () => {
  beforeEach(() => {
    visiblePageMock.mockReturnValue(null);
    localStorage.clear();
    window.history.replaceState(null, '', '/');
  });

  // Номера полос — не состояние экрана, а настройка чтения: читатель, который
  // выключил их в главе, не должен получить их обратно на следующей.
  it('показывает номера полос читателю без сохранённых настроек', () => {
    const { container } = renderSurface([page(265, '<p>текст</p>')]);

    expect(container.querySelector('.chapter-pages-content')).toHaveClass('show-page-info');
  });

  it('переживает пересборку экрана: выключенные номера остаются выключенными', async () => {
    const first = renderSurface([page(265, '<p>текст</p>')]);
    await userEvent.click(screen.getByRole('button', { name: 'Номера страниц' }));
    expect(first.container.querySelector('.chapter-pages-content')).not.toHaveClass(
      'show-page-info',
    );
    first.unmount();

    const second = renderSurface([page(265, '<p>текст</p>')]);

    expect(second.container.querySelector('.chapter-pages-content')).not.toHaveClass(
      'show-page-info',
    );
  });

  // Полоса прогресса рисуется потребителем экрана поверх окна и цифр не
  // несёт; доля прочитанного называется словом в панели, под самой полосой.
  it('называет долю прочитанного в панели', () => {
    renderSurface([page(265, '<p>текст</p>')], { progressPercent: 42 });
    expect(screen.getByText('42 %')).toBeInTheDocument();
  });

  // Экран чтения зовут и оттуда, где прогресса нет (предпросмотр полосы):
  // пустого места под несуществующую долю оставаться не должно.
  it('без переданной доли ничего про прогресс не пишет', () => {
    const { container } = renderSurface([page(265, '<p>текст</p>')]);
    expect(container.querySelector('.reading-percent')).toBeNull();
  });

  it('в потоке ведёт на правку видимой полосы', async () => {
    visiblePageMock.mockReturnValue(265);

    renderSurface([page(265, '<p>первая</p>'), page(266, '<p>вторая</p>')]);

    const link = await screen.findByRole('link', { name: /предложить исправление/i });
    expect(link.getAttribute('href')).toMatch(/\/pages\/\d+\/suggest$/);
  });

  // Маркер полосы стал якорем, и прежний переход на отдельный экран полосы
  // (скан рядом с текстом) переехал в панель — туда же, где уже стоит ссылка
  // на правку, и на ту же видимую полосу.
  it('ведёт из панели на скан видимой полосы', async () => {
    visiblePageMock.mockReturnValue(265);

    renderSurface([page(265, '<p>первая</p>'), page(266, '<p>вторая</p>')]);

    const link = await screen.findByRole('link', { name: /скан страницы/i });
    expect(link).toHaveAttribute('href', '/works/41/pages/265');
  });

  // Как и в панели потока (ReadingStreamBar): скан открывается отдельным
  // окном, чтобы место в главе не терялось, и говорит об этом стрелкой.
  it('открывает скан отдельным окном и помечает это значком', async () => {
    visiblePageMock.mockReturnValue(265);

    renderSurface([page(265, '<p>первая</p>'), page(266, '<p>вторая</p>')]);

    const link = await screen.findByRole('link', { name: /скан страницы/i });
    expect(link).toHaveAttribute('target', '_blank');
    expect(link).toHaveAttribute('rel', expect.stringContaining('noopener'));

    const mark = link.querySelector('.toolbar-label-external');
    expect(mark).toHaveTextContent('↗');
    expect(mark).toHaveAttribute('aria-hidden', 'true');
  });

  it('не показывает ссылку на скан, пока видимая страница не определена', () => {
    renderSurface([page(265, '<p>первая</p>')]);

    expect(screen.queryByRole('link', { name: /скан страницы/i })).not.toBeInTheDocument();
  });

  it('не показывает ссылку на правку, пока видимая страница не определена', () => {
    // На самом верху/низу прокрутки useVisiblePage отдаёт null — рисовать
    // ссылку не на что, лучше промолчать, чем указать на несуществующую
    // страницу.
    renderSurface([page(265, '<p>первая</p>')]);

    expect(screen.queryByRole('link', { name: /предложить исправление/i })).not.toBeInTheDocument();
  });

  it('рисует секцию на страницу с якорем, по которому прыгает оглавление', () => {
    const { container } = renderSurface([page(265, '<p>первая</p>'), page(266, '<p>вторая</p>')]);

    expect(container.querySelector('#chapter-page-265')).not.toBeNull();
    expect(container.querySelector('#chapter-page-266')).not.toBeNull();
  });

  it('склеивает оборванную на стыке полос фразу в один абзац', () => {
    const { container } = renderSurface([
      page(265, '<p>то очевидно, что я</p>'),
      page(266, '<p>никогда бы не решился.</p>'),
    ]);

    const paragraphs = Array.from(container.querySelectorAll('.page-html-content p'));
    expect(paragraphs).toHaveLength(1);

    // Копия, а не сам абзац: маркер номера выбрасывается только ради сверки
    // текста, и удалять его из живого DOM до подсчёта маркеров нельзя.
    const text = paragraphs[0].cloneNode(true) as HTMLElement;
    text.querySelectorAll('.page-marker').forEach((m) => m.remove());
    expect(text.textContent).toBe('то очевидно, что я никогда бы не решился.');

    // Якорь и маркер переехавшей страницы — на шве, и ровно в одном экземпляре.
    expect(container.querySelectorAll('#chapter-page-266')).toHaveLength(1);
    expect(container.querySelector('#chapter-page-266')?.classList.contains('page-seam')).toBe(
      true,
    );
    expect(container.querySelectorAll('.page-marker')).toHaveLength(2);
  });

  // Переход между полосами случился внутри строки, и межстраничная отбивка
  // перед остатком склеенной полосы — тот же самый лишний зазор, только
  // уехавший на абзац ниже. По этому классу CSS её и снимает.
  it('метит остаток склеенной полосы, чтобы перед ним не было межстраничной отбивки', () => {
    const { container } = renderSurface([
      page(265, '<p>то очевидно, что я</p>'),
      page(266, '<p>никогда бы не решился.</p><p>Второй абзац.</p>'),
    ]);

    const sections = Array.from(container.querySelectorAll('.chapter-page-section'));
    const remainder = sections.find((s) => s.textContent?.includes('Второй абзац'));
    expect(remainder?.classList.contains('is-seamed')).toBe(true);
    // У обычной полосы признака нет: там отбивка между страницами уместна.
    expect(sections[0].classList.contains('is-seamed')).toBe(false);
  });

  // Маркер на шве лежит внутри dangerouslySetInnerHTML и потому не может быть
  // ни <Link>, ни узлом с обработчиком: клик по нему перехватывается
  // делегированно (usePageSeams). Обычная ссылка перезагрузила бы приложение
  // целиком.
  it('уводит клик по маркеру шва к началу полосы, не перезагружая приложение', async () => {
    const { container } = renderSurface([
      page(265, '<p>то очевидно, что я</p>'),
      page(266, '<p>никогда бы не решился.</p>'),
    ]);
    const seam = container.querySelector('#chapter-page-266');
    expect(seam).toHaveClass('page-seam');

    await userEvent.click(screen.getByLabelText(/^Страница 266/));

    expect(document.activeElement).toBe(seam);
    expect(window.location.hash).toBe('#chapter-page-266');
  });

  it('не нумерует пустую страницу: её секция схлопывается и маркер лёг бы на соседний номер', () => {
    // Бит «пустая» приходит с сервера: markdown, по которому это решалось
    // раньше, в ответе больше не ездит.
    const { container } = renderSurface([page(265, '', true)]);

    expect(container.querySelectorAll('.page-marker')).toHaveLength(0);
  });

  // Присланная ссылка на полосу — адрес главы с хэшем. Секции уже несут эти
  // якоря, но браузер по ним не прокручивает: содержимое главы приезжает
  // сетевым ответом, к моменту разбора адреса в документе ничего нет.
  it('уводит к полосе, названной хэшем в адресе', () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(() => {});

    const { container } = renderSurface(
      [page(265, '<p>первая</p>'), page(266, '<p>вторая</p>')],
      {},
      '/works/41/chapters/7#chapter-page-266',
    );

    const section = container.querySelector('#chapter-page-266');
    expect(section).not.toBeNull();
    // Фокус переносится вместе с прокруткой: без него клавиатурный читатель
    // остаётся в начале главы, а скринридер не узнаёт о переезде.
    expect(document.activeElement).toBe(section);
    expect(scroll).toHaveBeenCalled();
    scroll.mockRestore();
  });

  // Маркер — не переход, а метка места: он уводит к началу своей полосы и
  // правит адрес, не добавляя записи в историю. Собственная обработка нужна
  // именно ради повторного нажатия: хэш при нём тот же самый, и одной сменой
  // адреса второй прыжок не сделать.
  it('повторный клик по маркеру снова уводит к началу полосы', async () => {
    const { container } = renderSurface([
      page(265, '<p>первая полоса.</p>'),
      page(266, '<p>Вторая полоса.</p>'),
    ]);
    const section = container.querySelector('#chapter-page-266');
    const marker = screen.getByRole('link', { name: /^Страница 266/ });

    await userEvent.click(marker);
    expect(document.activeElement).toBe(section);
    expect(window.location.hash).toBe('#chapter-page-266');

    (document.activeElement as HTMLElement).blur();
    await userEvent.click(marker);

    expect(document.activeElement).toBe(section);
  });

  it('без хэша ничего не прокручивает', () => {
    const scroll = vi.spyOn(Element.prototype, 'scrollIntoView').mockImplementation(() => {});

    renderSurface([page(265, '<p>первая</p>'), page(266, '<p>вторая</p>')]);

    expect(scroll).not.toHaveBeenCalled();
    scroll.mockRestore();
  });

  // Хэш может указывать на полосу, которой в этой главе нет (ссылка собрана
  // руками, диапазон главы изменился). Молчание лучше исключения.
  it('переживает хэш на несуществующей полосе', () => {
    expect(() =>
      renderSurface([page(265, '<p>первая</p>')], {}, '/works/41/chapters/7#chapter-page-999'),
    ).not.toThrow();
  });

  it('показывает сноски отдельным блоком', () => {
    renderSurface([page(265, '<p>текст</p>')]);

    expect(screen.getByText('сноска')).toBeTruthy();
  });

  it('показывает шапку и навигацию, полученные пропами', () => {
    render(
      <MemoryRouter>
        <ReadingPreferencesProvider>
          <ReadingSurface
            pages={[page(265, '<p>текст</p>')]}
            footnotesHtml=""
            work={{ id: 41, page_offset: 0 }}
            header={<h1>Людвиг Фейербах</h1>}
            nav={<a href="/next">дальше</a>}
            pageHref={(n) => `/works/41/pages/${n}`}
          />
        </ReadingPreferencesProvider>
      </MemoryRouter>,
    );

    expect(screen.getByText('Людвиг Фейербах')).toBeTruthy();
    // Навигация рисуется и сверху, и снизу текста — как было в ChapterView.
    expect(screen.getAllByText('дальше')).toHaveLength(2);
  });

  it('подсвечивает найденное по леммам с ё→е', () => {
    renderSurface([{ page_number: 1, html: '<p>Ёлка стояла, Гегеля читали</p>', blank: false }], {
      highlightTerms: ['елк', 'гегел'],
    });
    const marks = [...document.querySelectorAll('mark.search-hit')].map((m) => m.textContent);
    expect(marks).toEqual(['Ёлка', 'Гегеля']);
  });

  it('без лемм подсветки нет', () => {
    renderSurface([{ page_number: 1, html: '<p>Гегеля читали</p>', blank: false }]);
    expect(document.querySelector('mark.search-hit')).toBeNull();
  });

  // Обход подсветки сужен до .page-html-content: маркер номера страницы —
  // сосед этого блока, а не его часть, и несёт голую цифру. Регулярка слова
  // матчит цифры, поэтому числовой запрос («265») без сужения подсвечивал бы
  // сам номер страницы, а не только вхождения в тексте.
  it('не подсвечивает маркер номера страницы числовым запросом', () => {
    const { container } = renderSurface(
      [{ page_number: 265, html: '<p>Здесь 265 страниц в этом издании.</p>', blank: false }],
      { highlightTerms: ['265'] },
    );
    const marker = container.querySelector('.page-marker');
    expect(marker?.textContent).toBe('265');
    expect(marker?.querySelector('mark.search-hit')).toBeNull();
    // В тексте полосы то же число подсвечивается как обычно.
    const contentMarks = [...container.querySelectorAll('.page-html-content mark.search-hit')].map(
      (m) => m.textContent,
    );
    expect(contentMarks).toEqual(['265']);
  });

  // Выделение цитаты атрибутируется полосе по ближайшему data-page. У шва он
  // уже есть (stitchPages), у секции полосы — нет, и без него цитата на
  // несклеенной полосе осталась бы без номера вовсе.
  //
  // У полосы 6 два абзаца — иначе она целиком уезжает в шов, её секция не
  // рисуется вовсе (html==='' у stitched), и оставшийся в контейнере
  // '[data-page]' элемент — только шов, у которого этот атрибут стоял ещё до
  // задачи 9 (stitchPages.ts делает это сам). Ровно так и ловилась
  // рецензией слабость прежней версии теста: assertion проходил, даже когда
  // атрибут на СОБСТВЕННОЙ секции не ставился совсем. Второй абзац остаётся
  // на полосе 6 и рисует именно ту секцию, которую задача 9 добавляла.
  it('каждая секция полосы называет свой номер', () => {
    const { container } = renderSurface([
      page(5, '<p>пятая</p>'),
      page(6, '<p>начало шестой</p><p>остаток шестой</p>'),
    ]);
    const seam = container.querySelector('.page-seam[data-page="6"]');
    const ownSection = container.querySelector('.chapter-page-section.is-seamed[data-page="6"]');
    expect(seam).not.toBeNull();
    expect(ownSection).not.toBeNull();

    const plainSection = container.querySelector(
      '.chapter-page-section:not(.is-seamed)[data-page="5"]',
    );
    expect(plainSection).not.toBeNull();
  });
});

// citation необязателен намеренно: у элемента подборки (CollectionRead) нет
// данных для подписи, и панель молчит про цитату вовсе (см. CollectionRead —
// он этот проп не передаёт). Глава (ChapterView) и поток тома (WorkRead)
// собирают его и получают кнопку.
describe('ReadingSurface: цитата', () => {
  it('с переданным citation рисует кнопку «Цитировать»/«Ссылка»', () => {
    // Без видимой полосы и без выделения кнопка молчит вовсе (fix-раунд 1,
    // важное 3) — видимая полоса задаётся явно, как и в остальных тестах
    // этого файла, использующих useVisiblePage через мок.
    visiblePageMock.mockReturnValue(265);
    renderSurface([page(265, '<p>текст</p>')], {
      citation: {
        work: { id: 41, page_offset: 4, author: 'Автор', title: 'Название' } as Work,
        workTitleFor: () => 'Глава',
        quoteHref: (n, search) => `/works/41/chapters/7${search}#chapter-page-${n}`,
      },
    });

    expect(screen.getByRole('button', { name: /Цитировать|Ссылка/ })).toBeInTheDocument();
  });

  it('без citation кнопки цитаты нет', () => {
    renderSurface([page(265, '<p>текст</p>')]);

    expect(screen.queryByRole('button', { name: /Цитировать|Ссылка/ })).not.toBeInTheDocument();
  });
});

// Внешняя ссылка на место в тексте (вырезка указателя, задача 14) ведёт сюда
// же, что и на отдельную полосу, — ?quote= с дословной цитатой.
describe('ReadingSurface: приём ?quote=', () => {
  // Без номера полосы в адресе (#chapter-page-N) markQuote не зовётся вовсе —
  // решение рецензента: адрес, не назвавший места, ничего не подтверждает, и
  // подсветка первого совпадения в главе до 742 полос завела бы читателя не
  // туда молча. Ссылка без номера полосы обязана нести якорь — здесь он есть.
  it('подсвечивает найденное и не показывает полоску', () => {
    const { container } = renderSurface(
      [page(265, '<p>Так писал он в тысяча восемьсот сорок восьмом году.</p>')],
      { quote: { start: 'тысяча восемьсот сорок восьмом' } },
      '/works/41/chapters/7#chapter-page-265',
    );
    expect(container.querySelector('mark.quote-hit')?.textContent).toBe(
      'тысяча восемьсот сорок восьмом',
    );
    expect(container.querySelector('.quote-notice')).toBeNull();
  });

  // То, что видит получатель присланной ссылки: подсвечено процитированное
  // предложение целиком, а не ключ адреса из двух слов.
  it('пара якорей подсвечивает весь пролёт цитаты', () => {
    const { container } = renderSurface(
      [page(265, '<p>До. Если до 6 лет ребенок воспитан правильно, дурно. После.</p>')],
      { quote: { start: 'Если до', end: 'правильно, дурно.' } },
      '/works/41/chapters/7#chapter-page-265',
    );
    const marks = [...container.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe(
      'Если до 6 лет ребенок воспитан правильно, дурно.',
    );
    expect(container.querySelector('.quote-notice')).toBeNull();
  });

  it('ненайденный конец пролёта говорит об этом вслух', () => {
    const { container } = renderSurface(
      [page(265, '<p>Если до 6 лет ребенок воспитан иначе.</p>')],
      { quote: { start: 'Если до', end: 'этого тут больше нет' } },
      '/works/41/chapters/7#chapter-page-265',
    );
    expect(container.querySelector('mark.quote-hit')?.textContent).toBe('Если до');
    expect(screen.getByRole('status')).toHaveTextContent(/Конец цитаты не нашёлся/);
  });

  it('промах говорит об этом вслух', () => {
    renderSurface(
      [page(265, '<p>Другой текст.</p>')],
      { quote: { start: 'этого тут нет' } },
      '/works/41/chapters/7#chapter-page-265',
    );
    expect(screen.getByRole('alert')).toHaveTextContent(/не нашлась/);
  });

  it('без ?quote= не сравнивает вовсе', () => {
    const { container } = renderSurface([page(265, '<p>текст</p>')]);
    expect(container.querySelector('mark.quote-hit')).toBeNull();
    expect(container.querySelector('.quote-notice')).toBeNull();
  });

  // Прыжок к #chapter-page-{n} уже делает useHashAnchor — здесь проверяется
  // только то, что якорь из адреса сужает поиск цитаты до своей полосы, а не
  // дублирование самой прокрутки.
  it('якорь #chapter-page-N в адресе сужает поиск цитаты до этой полосы', () => {
    const { container } = renderSurface(
      [
        page(1, '<p>Так писал он в тысяча восемьсот сорок восьмом году.</p>'),
        page(2, '<p>Так писал он в тысяча восемьсот сорок восьмом году.</p>'),
      ],
      { quote: { start: 'Так писал он в тысяча восемьсот сорок восьмом году.' } },
      '/works/41/chapters/7#chapter-page-2',
    );
    const mark = container.querySelector('mark.quote-hit');
    expect(mark).not.toBeNull();
    expect(mark?.closest('[data-page]')?.getAttribute('data-page')).toBe('2');
    expect(container.querySelector('.quote-notice')).toBeNull();
  });

  // Жалоба читателя: придя по ссылке на цитату, он видел кольцо фокуса на
  // куске текста ВЫШЕ цитаты. Кольцо честное — это `:focus-visible` на шве
  // полосы (#chapter-page-N), куда фокус ставит useHashAnchor, а шов у
  // склеенной полосы начинается посреди чужого абзаца. Якорь называет полосу,
  // ссылка называет место: фокус обязан доехать до места, иначе он метит
  // соседний абзац, а клавиатурный читатель и скринридер встают не там.
  it('оставляет фокус на подсветке, а не на якоре полосы выше', () => {
    const { container } = renderSurface(
      [page(265, '<p>До цитаты. Так писал он в тысяча восемьсот сорок восьмом году.</p>')],
      { quote: { start: 'тысяча восемьсот сорок восьмом' } },
      '/works/41/chapters/7#chapter-page-265',
    );
    const mark = container.querySelector('mark.quote-hit');
    expect(mark).not.toBeNull();
    expect(document.activeElement).toBe(mark);
  });

  // Обратная половина того же: подсветки нет — забирать фокус нечему, и
  // прежняя посадка на полосу остаётся единственной, какая есть.
  it('промах оставляет фокус на якоре полосы', () => {
    const { container } = renderSurface(
      [page(265, '<p>Другой текст.</p>')],
      { quote: { start: 'этого тут нет' } },
      '/works/41/chapters/7#chapter-page-265',
    );
    expect(document.activeElement).toBe(container.querySelector('#chapter-page-265'));
  });

  // Регрессия, воспроизведённая рецензентом: адрес БЕЗ #chapter-page-N, но
  // цитата дословно совпадает с текстом ДРУГОЙ полосы главы. Решение —
  // «без номера полосы markQuote не звать вовсе»: раньше здесь искалась вся
  // глава целиком, и совпадение на чужой полосе подсвечивалось молча, без
  // единой оговорки, — читателя это вело не туда, а сообщить было нечем.
  it('без #chapter-page-N в адресе не ищет по всей главе — ни подсветки, ни полоски', () => {
    const { container } = renderSurface(
      [
        page(1, '<p>Так писал он в тысяча восемьсот сорок восьмом году.</p>'),
        page(2, '<p>Другой текст этой полосы.</p>'),
      ],
      { quote: { start: 'тысяча восемьсот сорок восьмом' } },
      '/works/41/chapters/7',
    );
    expect(container.querySelector('mark.quote-hit')).toBeNull();
    expect(container.querySelector('.quote-notice')).toBeNull();
  });
});
