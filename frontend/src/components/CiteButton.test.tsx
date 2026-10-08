import { describe, it, expect, vi, beforeEach } from 'vitest';
import { render, screen, fireEvent, act } from '@testing-library/react';
import { MemoryRouter } from 'react-router-dom';
import type { RefObject } from 'react';
import toast from 'react-hot-toast';
import { CiteButton, type CiteButtonProps } from './CiteButton';
import { pagePath, chapterPath } from '../utils/paths';
import { pageAnchorId } from '../utils/pageAnchor';
import type { Work } from '../types';

// Мок вместо настоящего react-hot-toast (он рисует портал в document.body,
// который в jsdom не отслеживает состояние тоста) — нужен, чтобы утверждать
// не «клик не бросает исключение» (мелкое а: старый стаб не проверял ничего
// сверх этого), а что читателю ДЕЙСТВИТЕЛЬНО показали нужное сообщение.
vi.mock('react-hot-toast', () => ({
  default: { success: vi.fn(), error: vi.fn() },
}));

const work = {
  id: 19,
  title: 'К. Маркс и Ф. Энгельс. Сочинения. Том 19',
  author: '',
  edition_title: 'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.',
  volume_number: 19,
  page_offset: 0,
  slug: 'mae-t19',
} as unknown as Work;

let written: Record<string, string> = {};

beforeEach(() => {
  written = {};
  vi.mocked(toast.success).mockClear();
  vi.mocked(toast.error).mockClear();
  window.getSelection()?.removeAllRanges();
  // jsdom не знает ни ClipboardItem, ни navigator.clipboard.write — обе
  // подделываются вручную, как советует бриф задачи 11.
  class ItemStub {
    constructor(public parts: Record<string, Blob>) {}
  }
  vi.stubGlobal('ClipboardItem', ItemStub);
  Object.defineProperty(navigator, 'clipboard', {
    configurable: true,
    value: {
      write: vi.fn(async (items: ItemStub[]) => {
        for (const [type, blob] of Object.entries(items[0].parts)) {
          written[type] = await blob.text();
        }
      }),
    },
  });
});

/**
 * Две полосы, каждая — своя секция с data-page, как их рисует
 * ReadingSurface/PageView. Собрана вручную вне React (append прямо в
 * document.body), тем же приёмом, что и surface() в quoteSelection.test.ts —
 * контейнер нужен как голый DOM-узел, на который указывает contentRef, а не
 * как то, что рендерит сам React.
 */
function surface(): HTMLElement {
  const root = document.createElement('div');
  const five = document.createElement('div');
  five.setAttribute('data-page', '5');
  five.innerHTML = '<div class="page-html-content"><p>Текст пятой полосы.</p></div>';
  const six = document.createElement('div');
  six.setAttribute('data-page', '6');
  six.innerHTML = '<div class="page-html-content"><p>Текст шестой полосы.</p></div>';
  root.append(five, six);
  document.body.innerHTML = '';
  document.body.append(root);
  return root;
}

function selectWithin(root: HTMLElement, pageNumber: number): void {
  const p = root.querySelector(`[data-page="${pageNumber}"] p`)!;
  const text = p.firstChild!;
  const range = document.createRange();
  range.setStart(text, 0);
  range.setEnd(text, (text.textContent ?? '').length);
  const sel = window.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  // jsdom не эмитит selectionchange сам — событие шлём вручную, а обновление
  // состояния кнопки (setHasSelection внутри обработчика) заворачиваем в
  // act(), иначе React ругается на обновление вне теста.
  act(() => {
    document.dispatchEvent(new Event('selectionchange'));
  });
}

function selectAcrossSeam(root: HTMLElement): void {
  const fiveText = root.querySelector('[data-page="5"] p')!.firstChild!;
  const sixText = root.querySelector('[data-page="6"] p')!.firstChild!;
  const range = document.createRange();
  range.setStart(fiveText, 'Текст '.length);
  range.setEnd(sixText, 'Текст шестой'.length);
  const sel = window.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  act(() => {
    document.dispatchEvent(new Event('selectionchange'));
  });
}

/**
 * Выделение целиком внутри колонцифры (.page-marker) — двойной клик по
 * печатному номеру, обычное читательское движение (fix-раунд 1, важное 4).
 */
function selectMarkerFully(root: HTMLElement): void {
  const section = root.querySelector('[data-page="5"]')!;
  const marker = document.createElement('a');
  marker.className = 'page-marker';
  marker.textContent = '5';
  section.prepend(marker);
  const text = marker.firstChild!;
  const range = document.createRange();
  range.setStart(text, 0);
  range.setEnd(text, (text.textContent ?? '').length);
  const sel = window.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
  act(() => {
    document.dispatchEvent(new Event('selectionchange'));
  });
}

function renderButton(
  props: Partial<CiteButtonProps> = {},
  contentRef: RefObject<HTMLElement> = { current: surface() },
) {
  return render(
    <MemoryRouter>
      <CiteButton
        work={work}
        workTitleFor={() => 'Капитал'}
        // Умолчание проб — адрес полосы, как его строит PageView. Экран
        // главы передаёт свой (тест «поверхность решает адрес» ниже).
        quoteHref={(n, search) => `${pagePath(work, n)}${search}`}
        contentRef={contentRef}
        visiblePage={5}
        {...props}
      />
    </MemoryRouter>,
  );
}

describe('CiteButton', () => {
  it('без выделения зовётся «Ссылка»', () => {
    renderButton();
    expect(screen.getByRole('button')).toHaveTextContent('Ссылка');
  });

  it('с выделением зовётся «Цитировать»', () => {
    const root = surface();
    renderButton({}, { current: root });
    selectWithin(root, 5);
    expect(screen.getByRole('button')).toHaveTextContent('Цитировать');
  });

  // Fix-раунд 1, важное 4: hasSelectionInside (дешёвая проверка на каждый
  // selectionchange) обязана применять то же исключение колонцифры/формулы,
  // что и readSelection (fullyInsideExcluded) — иначе надпись сулила бы
  // «Цитировать» там, где настоящий разбор выделения при нажатии отверг бы
  // его как пустое (readSelection тоже отвергает выделение внутри
  // .page-marker — quoteSelection.test.ts).
  it('выделение внутри колонцифры не зовёт «Цитировать»', () => {
    const root = surface();
    renderButton({}, { current: root });
    selectMarkerFully(root);
    expect(screen.getByRole('button')).toHaveTextContent('Ссылка');
  });

  it('pointerdown гасится, иначе нажатие снимет выделение', () => {
    renderButton();
    const button = screen.getByRole('button');
    const event = new Event('pointerdown', { bubbles: true, cancelable: true });
    button.dispatchEvent(event);
    expect(event.defaultPrevented).toBe(true);
  });

  it('пишет обе грани одним ClipboardItem', async () => {
    const root = surface();
    renderButton({}, { current: root });
    selectWithin(root, 5);
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(Object.keys(written).sort()).toEqual(['text/html', 'text/plain']);
    expect(written['text/plain']).toMatch(/^> /);
    expect(written['text/html']).toContain('<blockquote>');
  });

  it('в обеих гранях стоит адрес с ?quote=', async () => {
    const root = surface();
    renderButton({}, { current: root });
    selectWithin(root, 5);
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(written['text/plain']).toContain('/works/19-mae-t19/pages/5?quote=');
    expect(written['text/html']).toContain('/works/19-mae-t19/pages/5?quote=');
  });

  // Жалоба, с которой начато направление: читатель цитирует кусок в ГЛАВЕ, а
  // ссылка ведёт на отдельную полосу — не туда, где он был. Адрес строит
  // поверхность, и кнопка обязана взять её адрес, а не собирать свой.
  it('поверхность решает адрес: в главе ссылка ведёт на главу с якорем полосы', async () => {
    const root = surface();
    const chapter = { id: 2066, slug: 'o-knige-dlya-roditelej' };
    renderButton(
      {
        quoteHref: (n, search) => `${chapterPath(work, chapter)}${search}#${pageAnchorId(n)}`,
      },
      { current: root },
    );
    selectWithin(root, 5);
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(written['text/plain']).toContain(
      '/works/19-mae-t19/chapters/2066-o-knige-dlya-roditelej?quote=',
    );
    // Якорь обязателен и не украшение: без него ReadingSurface не знает
    // полосы и поиск цитаты не начинает вовсе (useQuoteHighlight).
    expect(written['text/plain']).toContain('#chapter-page-5');
    expect(written['text/plain']).not.toContain('/pages/5');
  });

  // Вторая половина жалобы: в адресе стояло два первых слова, и они же
  // подсвечивались. Якорь конца (&to=) везёт, докуда тянется цитата.
  it('адрес несёт якорь конца, когда ключ начала не накрывает выделение', async () => {
    const root = surface();
    renderButton({}, { current: root });
    selectAcrossSeam(root);
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    const url = written['text/plain'];
    expect(url).toContain('quote=');
    expect(url).toContain('&to=');
    // Хвост выделения — на полосе 6 («Текст шестой»), и якорь конца режется
    // из него, а не из полосы адреса.
    expect(decodeURIComponent(url)).toContain('to=шестой');
  });

  // Ключ, накрывший всё выделение (двойной клик по одному слову, и оно на
  // полосе единственно), делает &to= пустой добавкой к длине адреса:
  // кириллица в encodeURIComponent растёт вшестеро, и лишний якорь виден в
  // подписи цитаты глазом.
  it('ключ, накрывший всё выделение, не тянет за собой &to=', async () => {
    const root = surface();
    renderButton({}, { current: root });
    // «Текст» — первое слово полосы 5; на ней оно встречается один раз,
    // поэтому ключ равен всему выделению.
    const text = root.querySelector('[data-page="5"] p')!.firstChild!;
    const range = document.createRange();
    range.setStart(text, 0);
    range.setEnd(text, 'Текст'.length);
    const sel = window.getSelection();
    sel?.removeAllRanges();
    sel?.addRange(range);
    act(() => {
      document.dispatchEvent(new Event('selectionchange'));
    });
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(written['text/plain']).toContain('quote=');
    expect(written['text/plain']).not.toContain('&to=');
  });

  it('адрес ведёт на ПЕРВУЮ полосу выделения через стык', async () => {
    const root = surface();
    renderButton({}, { current: root });
    selectAcrossSeam(root);
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(written['text/plain']).toContain('/pages/5?quote=');
    expect(written['text/plain']).not.toContain('/pages/6?quote=');
  });

  it('без выделения ссылка ведёт на видимую полосу без цитаты в буфере', async () => {
    renderButton({ visiblePage: 7 });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(written['text/plain']).toContain('/works/19-mae-t19/pages/7');
    // Ветка «Ссылка» ничего не цитирует — тела цитаты (текста полосы) в
    // буфере нет, только подпись с адресом.
    expect(written['text/plain']).not.toMatch(/^> /);
    // Адрес — своей строкой, как и в ветке с цитатой: подпись кончается
    // сокращением с точкой, и приклеенный к ней адрес не скопировать
    // отдельно. Проверяется ЗДЕСЬ, а не только в citation.test.ts: форму
    // ветки «Ссылка» кнопка когда-то собирала по месту своей строкой, и
    // именно такая вторая копия разъехалась бы с первой молча.
    expect(written['text/plain']).toMatch(/\.\nhttp/);
  });

  // Fix-раунд 1, важное 3: раньше кнопка рисовалась и молча ничего не
  // делала по нажатию (assertion был на write, а не на разметку). Соседние
  // органы управления панели в этом же состоянии не рендерятся вовсе
  // (`visiblePage !== null && (...)`, ReadingSurface.tsx) — кнопка обязана
  // делать то же самое.
  it('без выделения и без видимой полосы кнопка не рисуется', () => {
    renderButton({ visiblePage: null });
    expect(screen.queryByRole('button')).not.toBeInTheDocument();
  });

  it('успешное копирование цитаты сообщается читателю', async () => {
    const root = surface();
    renderButton({}, { current: root });
    selectWithin(root, 5);
    await screen.findByRole('button', { name: 'Цитировать' });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(toast.success).toHaveBeenCalled());

    expect(toast.success).toHaveBeenCalledWith('Цитата скопирована');
  });

  // Мелкое б: ветка «Ссылка» (без выделения) раньше собирала HTML-тег
  // вручную, минуя esc() — имя автора со знаком `<`/`&` сломало бы разметку
  // письма/документа, куда цитата вставляется. citationLinkHtml (citation.ts)
  // экранирует оба слота тем же esc(), что и citationHtml.
  it('html-грань ветки «Ссылка» экранирует подпись', async () => {
    const authorWork = { ...work, author: 'А. Б. & <В>' } as unknown as Work;
    renderButton({ work: authorWork, visiblePage: 7 });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

    expect(written['text/html']).toContain('А. Б. &amp; &lt;В&gt;');
    expect(written['text/html']).not.toContain('<В>');
  });

  it('успешное копирование ссылки сообщается читателю другим текстом', async () => {
    renderButton({ visiblePage: 7 });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(toast.success).toHaveBeenCalled());

    expect(toast.success).toHaveBeenCalledWith('Ссылка скопирована');
  });

  // Мелкое а: старый тест был стабом (`not.toThrow()`), проверявшим только
  // отсутствие исключения, — а не то, что читателю ДЕЙСТВИТЕЛЬНО показали
  // сообщение об отказе.
  it('отказ буфера сообщается читателю, а не падает молча', async () => {
    Object.defineProperty(navigator, 'clipboard', {
      configurable: true,
      value: { write: vi.fn().mockRejectedValue(new Error('denied')) },
    });
    renderButton({ visiblePage: 7 });

    fireEvent.click(screen.getByRole('button'));
    await vi.waitFor(() => expect(toast.error).toHaveBeenCalled());

    expect(toast.error).toHaveBeenCalledWith('Не удалось скопировать');
    expect(toast.success).not.toHaveBeenCalled();
  });

  // Fix-раунд 1, важное 1 и 2: подпись цитаты не имела ни одного теста, и
  // именно поэтому кнопка молча печатала «с. 5—5» вместо «с. 5» у выделения
  // на одной полосе — folios/pageNumbers строились длины 2 из span[0] и
  // span[span.length-1] независимо от span.length.
  describe('подпись цитаты (fix-раунд 1, важное 1 и 2)', () => {
    it('одна полоса: «с. 5», не «с. 5—5»', async () => {
      const root = surface();
      renderButton({}, { current: root });
      selectWithin(root, 5);
      await screen.findByRole('button', { name: 'Цитировать' });

      fireEvent.click(screen.getByRole('button'));
      await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

      expect(written['text/plain']).toContain('с. 5.');
      expect(written['text/plain']).not.toContain('с. 5—5');
    });

    it('диапазон через стык: «с. 5—6»', async () => {
      const root = surface();
      renderButton({}, { current: root });
      selectAcrossSeam(root);
      await screen.findByRole('button', { name: 'Цитировать' });

      fireEvent.click(screen.getByRole('button'));
      await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

      expect(written['text/plain']).toContain('с. 5—6.');
    });

    it('полоса без колонцифры: «б/н, полоса N»', async () => {
      // page_offset настолько отрицательный, что печатного номера у полосы 5
      // нет вовсе (printedFolio возвращает null при n < 1) — тот же приём,
      // что и в folio.test.ts.
      const noFolioWork = { ...work, page_offset: -10 } as unknown as Work;
      const root = surface();
      renderButton({ work: noFolioWork }, { current: root });
      selectWithin(root, 5);
      await screen.findByRole('button', { name: 'Цитировать' });

      fireEvent.click(screen.getByRole('button'));
      await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

      expect(written['text/plain']).toContain('б/н, полоса 5');
    });

    it('пустой workTitleFor — слот названия работы молчит', async () => {
      const root = surface();
      renderButton({ workTitleFor: () => '' }, { current: root });
      selectWithin(root, 5);
      await screen.findByRole('button', { name: 'Цитировать' });

      fireEvent.click(screen.getByRole('button'));
      await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

      // ' // ' (с пробелами) — сама разметка слота названия работы в
      // citationSignature; голое 'http://' в адресе строки этой проверке не
      // мешает.
      expect(written['text/plain']).not.toContain(' // ');
    });

    it('пустой author — подпись начинается сразу с названия, без слепой точки', async () => {
      const root = surface();
      renderButton({}, { current: root }); // work.author === '' в фикстуре
      selectWithin(root, 5);
      await screen.findByRole('button', { name: 'Цитировать' });

      fireEvent.click(screen.getByRole('button'));
      await vi.waitFor(() => expect(Object.keys(written)).not.toHaveLength(0));

      expect(written['text/plain']).toMatch(/^> Текст пятой полосы\.\n\nКапитал \/\/ /);
    });
  });
});
