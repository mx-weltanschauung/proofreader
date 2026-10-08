import { describe, it, expect } from 'vitest';
import { flattenText, markQuote, normalizeQuote } from './quoteMatch';

function root(html: string): HTMLElement {
  const el = document.createElement('div');
  el.innerHTML = html;
  document.body.append(el);
  return el;
}

describe('normalizeQuote', () => {
  it('схлопывает любой пробельный ряд в один пробел и обрезает края', () => {
    expect(normalizeQuote('  в\n  начале  было  ')).toBe('в начале было');
  });

  it('регистр и ё не трогает: ключ точный, это не поиск', () => {
    expect(normalizeQuote('Ещё Раз')).toBe('Ещё Раз');
  });
});

describe('flattenText', () => {
  it('склеивает текст через границы инлайн-разметки', () => {
    const flat = flattenText(root('<p>Геге<em>ля</em> читали</p>'));
    expect(flat.text).toBe('Гегеля читали');
  });

  it('выводы формул в текст не входят', () => {
    const flat = flattenText(root('<p>до <span class="katex">x^2</span> после</p>'));
    expect(flat.text).toBe('до после');
  });

  it('приписывает знаки полосе по ближайшему data-page', () => {
    const flat = flattenText(
      root('<div data-page="5"><p>пять<span data-page="6">шесть</span></p></div>'),
    );
    expect(flat.pages[0]).toBe(5);
    expect(flat.pages[flat.text.indexOf('шесть')]).toBe(6);
  });

  // Разметка — упрощённый шов из stitchPages.ts (makeSeam): числовой маркер
  // страницы лежит текстовым узлом внутри содержимого предыдущей полосы,
  // скрыт CSS-правилом .page-marker { display: none } и в Selection живого
  // читателя никогда не попадает.
  it('маркер номера страницы шва не входит в текст', () => {
    const el = root(
      '<p>конец фразы. <span class="page-seam" data-page="234">' +
        '<a class="page-marker" href="#">234</a>Начало следующей</span></p>',
    );
    const flat = flattenText(el);
    expect(flat.text).toBe('конец фразы. Начало следующей');
  });
});

describe('markQuote', () => {
  it('подсвечивает точную подстроку через границу разметки', () => {
    const el = root('<p>Геге<em>ля</em> читали внимательно</p>');
    expect(markQuote(el, { start: 'Гегеля читали' })).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Гегеля читали');
  });

  it('говорит о нескольких вхождениях и подсвечивает первое', () => {
    const el = root('<p>и наоборот. Затем и наоборот снова</p>');
    expect(markQuote(el, { start: 'и наоборот' })).toBe('multiple');
    expect(el.querySelectorAll('mark.quote-hit').length).toBeGreaterThan(0);
    expect(el.textContent).toBe('и наоборот. Затем и наоборот снова');
  });

  it('промах ничего не портит', () => {
    const el = root('<p>текст полосы</p>');
    expect(markQuote(el, { start: 'этого тут нет' })).toBe('miss');
    expect(el.querySelector('mark.quote-hit')).toBeNull();
    expect(el.textContent).toBe('текст полосы');
  });

  it('совпадение через перевод строки в исходнике находится', () => {
    const el = root('<p>первая\n   вторая</p>');
    expect(markQuote(el, { start: 'первая вторая' })).toBe('hit');
  });

  it('цитата через шов находится, несмотря на скрытый номер полосы', () => {
    const el = root(
      '<p>конец фразы. <span class="page-seam" data-page="234">' +
        '<a class="page-marker" href="#">234</a>Начало следующей</span></p>',
    );
    expect(markQuote(el, { start: 'фразы. Начало' })).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('фразы. Начало');
  });

  // Узкий неразрывный пробел (U+202F) — тот же знак, что уже ловил
  // groupThousands.ts; раньше isSpace его не признавал, хотя normalizeQuote
  // (через \s) — признавал, и текст с ним не совпадал сам с собой.
  it('узкий неразрывный пробел приравнивается к обычному', () => {
    const flat = flattenText(root('<p>до после</p>'));
    expect(flat.text).toBe('до после');
  });

  it('область поиска сужается до конкретной полосы', () => {
    const el = root(
      '<div>' +
        '<p data-page="10">общая фраза тут</p>' +
        '<p data-page="11">общая фраза тут</p>' +
        '</div>',
    );
    expect(markQuote(el, { start: 'общая фраза' })).toBe('multiple');

    const el2 = root(
      '<div>' +
        '<p data-page="10">общая фраза тут</p>' +
        '<p data-page="11">общая фраза тут</p>' +
        '</div>',
    );
    expect(markQuote(el2, { start: 'общая фраза' }, 11)).toBe('hit');
    const hit = el2.querySelector('mark.quote-hit');
    expect(hit?.closest('[data-page]')?.getAttribute('data-page')).toBe('11');
  });

  // Полоса 5 приходит двумя несмежными кусками (голова в шве плюс хвост
  // дальше в документе), а между ними — чужой материал другой полосы (в
  // потоке чтения это сноски полосы 5, нарисованные ReadingChunk.tsx между
  // текстом полосы и следующей полосой). Склейка кусков полосы 5 в один
  // текст-подмножество не должна выдавать совпадение, которое реально
  // перепрыгивает через этот разрыв, — такую строку читатель никогда не
  // видел подряд.
  it('совпадение через разрыв между кусками одной полосы не находится', () => {
    const el = root('<p data-page="5">abc</p><p data-page="6">xyz</p><p data-page="5">def</p>');
    expect(markQuote(el, { start: 'cde' }, 5)).toBe('miss');
    expect(el.querySelector('mark.quote-hit')).toBeNull();
  });

  it('совпадение целиком внутри одного куска полосы по-прежнему находится', () => {
    const el = root('<p data-page="5">abc</p><p data-page="6">xyz</p><p data-page="5">def</p>');
    expect(markQuote(el, { start: 'abc' }, 5)).toBe('hit');
  });

  it('совпадение целиком во втором куске той же полосы тоже находится', () => {
    const el = root('<p data-page="5">abc</p><p data-page="6">xyz</p><p data-page="5">def</p>');
    expect(markQuote(el, { start: 'def' }, 5)).toBe('hit');
    const hit = el.querySelector('mark.quote-hit');
    expect(hit?.textContent).toBe('def');
  });

  // Ядро направления: адрес несёт ДВА якоря, и подсвечивается всё между
  // ними — иначе читателю, пришедшему по ссылке, светится ключ из двух слов
  // вместо предложения, которое цитировали.
  it('пара якорей подсвечивает весь пролёт между ними', () => {
    const el = root(
      '<p data-page="5">До цитаты. Если до 6 лет ребенок воспитан, дурно. После.</p>',
    );
    expect(markQuote(el, { start: 'Если до', end: 'воспитан, дурно.' }, 5)).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Если до 6 лет ребенок воспитан, дурно.');
  });

  it('пролёт идёт через границы разметки', () => {
    const el = root('<p data-page="5">Геге<em>ля</em> читали <b>внимательно</b> и молча</p>');
    expect(markQuote(el, { start: 'Гегеля', end: 'внимательно' }, 5)).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Гегеля читали внимательно');
  });

  // Якорь конца ищется ВПЕРЁД от найденного начала: повтор тех же слов
  // раньше начала — не конец цитаты, и пролёт от него пошёл бы назад.
  it('якорь конца берётся первым ПОСЛЕ начала, а не первым в тексте', () => {
    const el = root('<p data-page="5">конец. Начало цитаты и её конец. Хвост.</p>');
    expect(markQuote(el, { start: 'Начало цитаты', end: 'конец.' }, 5)).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Начало цитаты и её конец.');
  });

  // Полоса правилась после того, как на неё сослались: начало нашлось, конец
  // нет. Промолчать тут нельзя — подсветка окажется короче цитаты, и читатель
  // должен узнать, что это правка текста, а не кривая ссылка.
  it('конец не нашёлся — подсвечено начало и сказано вслух', () => {
    const el = root('<p data-page="5">Если до 6 лет ребенок воспитан иначе.</p>');
    expect(markQuote(el, { start: 'Если до', end: 'подействует дурно' }, 5)).toBe('partial');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Если до');
  });

  it('начало не нашлось — промах, даже если конец в тексте есть', () => {
    const el = root('<p data-page="5">Только хвост тут.</p>');
    expect(markQuote(el, { start: 'этого тут нет', end: 'хвост' }, 5)).toBe('miss');
    expect(el.querySelector('mark.quote-hit')).toBeNull();
  });

  // Цитата через стык полос: начало сужено полосой из адреса, а конец лежит
  // на следующей. Пролёт красится целиком — в главе полосы склеены в один
  // поток, и читатель выделял их подряд.
  it('пролёт переходит на следующую полосу, если конец цитаты там', () => {
    const el = root('<p data-page="5">конец пятой</p> <p data-page="6">начало шестой</p>');
    expect(markQuote(el, { start: 'конец', end: 'начало шестой' }, 5)).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('конец пятой начало шестой');
  });

  // Якорь конца короткой цитаты законно оказывается ВНУТРИ ключа начала:
  // ключ растёт вправо до уникальности, якорь конца — влево, и на цитате в
  // три слова они перекрываются. Пролёт от этого не должен становиться короче
  // самого ключа.
  it('якорь конца внутри ключа начала не укорачивает пролёт', () => {
    const el = root('<p data-page="5">Если до 6 лет, и снова если до 7</p>');
    expect(markQuote(el, { start: 'Если до 6', end: 'до' }, 5)).toBe('hit');
    const marks = [...el.querySelectorAll('mark.quote-hit')];
    expect(marks.map((m) => m.textContent).join('')).toBe('Если до 6');
  });

  // Два исхода сразу — начало неоднозначно И конец не нашёлся. Говорим про
  // конец: подсветка короче цитаты заметнее читателю, чем выбор вхождения,
  // а сказать за один раз можно только одно.
  it('неоднозначное начало при ненайденном конце сообщается как «конец не нашёлся»', () => {
    const el = root('<p data-page="5">общая фраза тут, общая фраза там</p>');
    expect(markQuote(el, { start: 'общая фраза', end: 'этого нет' }, 5)).toBe('partial');
  });
});
