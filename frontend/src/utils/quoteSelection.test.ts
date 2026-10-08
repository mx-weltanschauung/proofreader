import { describe, it, expect, beforeEach } from 'vitest';
import { pageText, readSelection } from './quoteSelection';
import { stitchPages, type SeamMarker } from './stitchPages';

const marker: SeamMarker = {
  href: (n) => `#p${n}`,
  label: (n) => `Страница ${n}`,
};

/**
 * Разметка, которую собирает НАСТОЯЩИЙ stitchPages, обёрнутая в те же
 * элементы, что ставят ReadingSurface.tsx/ReadingChunk.tsx вокруг него
 * (секция с data-page, свой маркер-колонцифра снаружи содержимого у
 * несклеенной полосы, .page-html-content внутри). Раньше фикстура была
 * рукописной имитацией шва — рецензия прогнала настоящий stitchPages на этих
 * же входных данных и показала, что рукописный вариант расходился с ним
 * ровно там, где это важно (пробел между абзацами), и что фикстура была
 * права, а не бриф. Здесь эта разница больше не может возникнуть: секция
 * строится тем же кодом, что рисует экран.
 */
function buildSurface(pages: { pageNumber: number; html: string }[]): HTMLElement {
  const stitched = stitchPages(pages, marker);
  const root = document.createElement('div');
  for (const { pageNumber, html, seamed } of stitched) {
    // Как в ReadingSurface.tsx/ReadingChunk.tsx: полоса, целиком уехавшая в
    // предыдущую, своей секции не рисует — рисовать нечего.
    if (seamed && html === '') continue;
    const section = document.createElement('div');
    section.className = `chapter-page-section${seamed ? ' is-seamed' : ''}`;
    section.setAttribute('data-page', String(pageNumber));
    // Свой маркер-колонцифра есть только у несклеенной полосы — у склеенной
    // он уже стоит внутри шва, свёрстанного stitchPages в ПРЕДЫДУЩУЮ секцию.
    if (!seamed) {
      const a = document.createElement('a');
      a.className = 'page-marker';
      a.setAttribute('href', marker.href(pageNumber));
      a.textContent = String(pageNumber);
      section.append(a);
    }
    const content = document.createElement('div');
    content.className = 'page-html-content';
    content.innerHTML = html;
    section.append(content);
    root.append(section);
  }
  document.body.innerHTML = '';
  document.body.append(root);
  return root;
}

/**
 * Пятая полоса продолжается шестой (роняющее конец предложение «конец
 * пятой» велит stitchPages склеить), внутри её абзаца — шов шестой. Перенос
 * строки между двумя абзацами шестой — настоящий, как в HTML, который отдаёт
 * сервер: именно он, пережив склейку, остаётся текстовым узлом внутри
 * СОБСТВЕННОЙ секции полосы 6 и даёт разделяющий пробел между «начало
 * шестой» (шов, лежащий в секции полосы 5) и «остаток шестой» (секция
 * полосы 6) — без него они слиплись бы в «шестойостаток».
 */
function surface(): HTMLElement {
  return buildSurface([
    { pageNumber: 5, html: '<p>конец пятой</p>' },
    { pageNumber: 6, html: '<p>начало шестой</p>\n<p>остаток шестой</p>' },
  ]);
}

function selectBetween(from: Node, fo: number, to: Node, tofs: number): void {
  const range = document.createRange();
  range.setStart(from, fo);
  range.setEnd(to, tofs);
  const sel = window.getSelection();
  sel?.removeAllRanges();
  sel?.addRange(range);
}

describe('pageText', () => {
  it('текст полосы не вбирает шов соседней', () => {
    const el = surface();
    expect(pageText(el, 5)).toBe('конец пятой');
  });

  it('текст полосы склеивает шов и её собственную секцию', () => {
    const el = surface();
    expect(pageText(el, 6)).toBe('начало шестой остаток шестой');
  });
});

describe('readSelection', () => {
  beforeEach(() => {
    document.body.innerHTML = '';
  });

  it('выделение внутри одной полосы: маркеров нет', () => {
    const el = surface();
    const p = el.querySelectorAll('.page-html-content p')[0].firstChild!;
    selectBetween(p, 0, p, 11);
    const got = readSelection(el, () => '[м]');
    expect(got?.pages).toEqual([5]);
    expect(got?.pageSpan).toEqual([5]);
    expect(got?.text).toBe('конец пятой');
    expect(got?.head).toBe('конец пятой');
    expect(got?.firstPageText).toBe('конец пятой');
    // Одна полоса — голова и хвост это одно и то же выделение, но якоря из
    // них режутся с разных концов (quoteKey/quoteTail).
    expect(got?.tail).toBe('конец пятой');
    expect(got?.lastPageText).toBe('конец пятой');
  });

  it('выделение через стык: маркер на каждом стыке, кроме первого', () => {
    const el = surface();
    const paragraphs = el.querySelectorAll('.page-html-content p');
    const first = paragraphs[0].firstChild!;
    const last = paragraphs[1].firstChild!;
    selectBetween(first, 6, last, (last.textContent ?? '').length);
    const got = readSelection(el, (n) => `[с. ${n}]`);
    expect(got?.pages).toEqual([5, 6]);
    expect(got?.pageSpan).toEqual([5, 6]);
    // «начало шестой» и «остаток шестой» — два настоящих отдельных <p> в
    // исходной вёрстке (шов срастил только первый с концом полосы 5); фикс-
    // раунд 1 (п. 5) обязан развести их переводом строки, а не слитной
    // строкой через пробел, который раньше стоял на месте границы абзаца.
    expect(got?.text).toBe('пятой [с. 6] начало шестой\nостаток шестой');
    // Ключ режется из головы на ПЕРВОЙ полосе — адрес ведёт туда же.
    expect(got?.head).toBe('пятой');
    // А якорь конца — из хвоста на ПОСЛЕДНЕЙ полосе, и уникальность ему
    // считается по ней же: полосы у начала и конца цитаты через стык разные.
    expect(got?.tail).toBe('начало шестой остаток шестой');
    expect(got?.lastPageText).toBe('начало шестой остаток шестой');
  });

  it('выделение через три абзаца одной полосы: перевод строки между ними', () => {
    const el = document.createElement('div');
    el.innerHTML =
      '<div class="chapter-page-section" data-page="9"><div class="page-html-content">' +
      '<p>Первый абзац.</p>\n<p>Второй абзац.</p>\n<p>Третий абзац.</p>' +
      '</div></div>';
    document.body.innerHTML = '';
    document.body.append(el);

    const paragraphs = el.querySelectorAll('p');
    const first = paragraphs[0].firstChild!;
    const last = paragraphs[2].firstChild!;
    selectBetween(first, 0, last, (last.textContent ?? '').length);

    const got = readSelection(el, () => '[м]');
    // Одна полоса — маркеров стыка нет, но границы абзацев внутри неё нельзя
    // терять: раньше normalizeQuote схлопывал их в пробел наравне с обычной
    // склейкой строк, и цитата из трёх абзацев уезжала в буфер одной строкой.
    expect(got?.pages).toEqual([9]);
    expect(got?.text).toBe('Первый абзац.\nВторой абзац.\nТретий абзац.');
    // head и firstPageText участвуют в СРАВНЕНИИ — переводов строк не несут.
    expect(got?.head).toBe('Первый абзац. Второй абзац. Третий абзац.');
  });

  it('пустое выделение — null', () => {
    const el = surface();
    window.getSelection()?.removeAllRanges();
    expect(readSelection(el, () => '[м]')).toBeNull();
  });

  it('выделение вне контейнера не считается', () => {
    const el = surface();
    const outside = document.createElement('p');
    outside.textContent = 'чужой текст';
    document.body.append(outside);
    selectBetween(outside.firstChild!, 0, outside.firstChild!, 5);
    expect(readSelection(el, () => '[м]')).toBeNull();
  });

  // Указание контроллера поверх брифа: голова обязана кончиться на первом
  // разрыве, а не собрать все куски первой полосы по всему выделению. Разрыв
  // моделирует реальный поток чтения (ReadingChunk.tsx): конец абзаца полосы
  // 5, дальше — приклеенная голова полосы 6 (шов внутри того же абзаца), а
  // за ним — снова текст полосы 5 (сноски полосы 5, которые в потоке рисуются
  // между головой 6 и её собственным остатком). Без headBroken голова
  // склеила бы «кусок пятой» с «ещё кусок пятой» через чужой кусок текста
  // (полосу 6), которого читатель не видел подряд. Независимая рецензия
  // прогнала этот же сценарий на разметке, порождённой настоящим stitchPages
  // и настоящим расположением сносок ReadingChunk.tsx, и подтвердила: держит
  // оба случая.
  it('разрыв внутри первой полосы: голова кончается на первом разрыве', () => {
    const el = document.createElement('div');
    el.innerHTML =
      '<div data-page="5"><p>кусок пятой <span data-page="6">полоса шестая</span></p></div>' +
      ' <div data-page="5"><p>ещё кусок пятой</p></div>';
    document.body.innerHTML = '';
    document.body.append(el);

    const paragraphs = el.querySelectorAll('p');
    const first = paragraphs[0].firstChild!;
    const last = paragraphs[1].firstChild!;
    selectBetween(first, 0, last, (last.textContent ?? '').length);

    const got = readSelection(el, (n) => `[с. ${n}]`);
    expect(got?.pages[0]).toBe(5);
    expect(got?.head).toBe('кусок пятой');
    // Хвост — зеркально: последний непрерывный кусок последней полосы
    // перехода, а не все её знаки по всему выделению. Здесь выделение
    // кончается снова на полосе 5, и хвост — только «ещё кусок пятой»:
    // склей его с головой, и якорь конца искался бы в тексте-фантоме.
    expect(got?.tail).toBe('ещё кусок пятой');
  });

  // Рецензия довела до подсветки: двойной клик по колонцифре — обычное
  // читательское движение, а не редкость. flattenText отсеивает .page-marker
  // по предку, но Range.cloneContents, когда весь диапазон лежит внутри
  // одного элемента, переносит в клон голый текст без class — фильтру не за
  // что зацепиться, и без этой проверки got.text было бы "265", а markQuote
  // честно нашёл бы и подсветил ДРУГОЕ «265» на той же полосе.
  it('выделение внутри колонцифры (page-marker) не считается', () => {
    const el = buildSurface([{ pageNumber: 265, html: '<p>в 265 году было так</p>' }]);
    const markerLink = el.querySelector('a.page-marker')!;
    const text = markerLink.firstChild!;
    selectBetween(text, 0, text, (text.textContent ?? '').length);
    expect(readSelection(el, () => '[м]')).toBeNull();
  });

  // pages — последовательность ПЕРЕХОДОВ (с повторами при старте в шве),
  // pageSpan — отсортированный уникальный диапазон для подписи. Выделение
  // начинается внутри шва полосы 6 (встроен в абзац полосы 5), затем идёт
  // хвост абзаца полосы 5, затем собственная секция полосы 6: pages честно
  // повторяет 6 дважды, pageSpan — нет.
  it('pages — переходы с возможным повтором, pageSpan — диапазон без повторов', () => {
    const el = document.createElement('div');
    el.innerHTML =
      '<div class="chapter-page-section" data-page="5">' +
      '<p><span class="chapter-page-section page-seam" data-page="6">шов шестой</span> хвост пятой</p>' +
      '</div>' +
      '<div class="chapter-page-section" data-page="6"><p>остаток шестой</p></div>';
    document.body.innerHTML = '';
    document.body.append(el);

    const seamText = el.querySelector('.page-seam')!.firstChild!;
    const lastText = el.querySelectorAll('p')[1].firstChild!;
    selectBetween(seamText, 0, lastText, (lastText.textContent ?? '').length);

    const got = readSelection(el, (n) => `[с. ${n}]`);
    expect(got?.pages).toEqual([6, 5, 6]);
    expect(got?.pageSpan).toEqual([5, 6]);
  });
});
