import { describe, it, expect } from 'vitest';
import { highlightTerms, normalizeWord } from './searchHighlight';

function root(html: string): HTMLElement {
  const el = document.createElement('div');
  el.innerHTML = html;
  return el;
}

describe('searchHighlight', () => {
  it('normalizeWord: нижний регистр и ё→е', () => {
    expect(normalizeWord('Ёлка')).toBe('елка');
  });

  it('оборачивает слова, начинающиеся с леммы, во всех формах', () => {
    const el = root('<p>Гегеля читали, Гегелем восхищались, гегемония мимо.</p>');
    expect(highlightTerms(el, ['гегел'])).toBe(2);
    const marks = [...el.querySelectorAll('mark.search-hit')].map((m) => m.textContent);
    expect(marks).toEqual(['Гегеля', 'Гегелем']);
    expect(el.textContent).toBe('Гегеля читали, Гегелем восхищались, гегемония мимо.');
  });

  it('ё в тексте совпадает с леммой на е и не портит соседние узлы', () => {
    const el = root('<p><em>Ёлка</em> и <a href="#">ёлки</a></p>');
    expect(highlightTerms(el, ['елк'])).toBe(2);
    expect(el.querySelector('em mark')?.textContent).toBe('Ёлка');
    expect(el.querySelector('a mark')?.textContent).toBe('ёлки');
  });

  it('повторный вызов не вкладывает mark в mark', () => {
    const el = root('<p>Гегеля</p>');
    highlightTerms(el, ['гегел']);
    highlightTerms(el, ['гегел']);
    expect(el.querySelectorAll('mark').length).toBe(1);
  });

  it('без лемм ничего не трогает', () => {
    const el = root('<p>Гегеля</p>');
    expect(highlightTerms(el, [])).toBe(0);
    expect(el.querySelector('mark')).toBeNull();
  });

  // Ниже — случаи, которые набор брифа не покрывает: что функция не должна
  // делать никогда, раз она переписывает текст читаемой книги.

  it('не теряет текст между двумя совпадениями в одном узле', () => {
    const el = root('<p>гегель и гегель снова</p>');
    highlightTerms(el, ['гегел']);
    expect(el.textContent).toBe('гегель и гегель снова');
    expect(el.querySelectorAll('mark.search-hit')).toHaveLength(2);
  });

  it('совпадение в самом начале и в самом конце узла не теряет соседний текст', () => {
    const el = root('<p>гегель кончился гегель</p>');
    highlightTerms(el, ['гегел']);
    expect(el.textContent).toBe('гегель кончился гегель');
    const marks = [...el.querySelectorAll('mark.search-hit')].map((m) => m.textContent);
    expect(marks).toEqual(['гегель', 'гегель']);
  });

  it('не подсвечивает часть слова: подслово в середине не совпадает целиком, но матч не рвёт слово', () => {
    // «загегель» не начинается с «гегел» — подсветки быть не должно, и слово
    // должно остаться целым узлом текста, а не быть разорвано посередине.
    const el = root('<p>загегель рядом</p>');
    expect(highlightTerms(el, ['гегел'])).toBe(0);
    expect(el.textContent).toBe('загегель рядом');
    expect(el.querySelector('mark')).toBeNull();
  });

  it('второй вызов с другими леммами на уже подсвеченном тексте не плодит вложенные mark', () => {
    const el = root('<p>Гегеля и Маркса читали</p>');
    highlightTerms(el, ['гегел']);
    highlightTerms(el, ['маркс']);
    const marks = [...el.querySelectorAll('mark.search-hit')];
    expect(marks).toHaveLength(2);
    // Ни один mark не вложен в другой.
    marks.forEach((m) => {
      expect(m.querySelector('mark')).toBeNull();
    });
    expect(el.textContent).toBe('Гегеля и Маркса читали');
  });

  it('не трогает соседние атрибуты и структуру — оборачивает только текстовый узел', () => {
    const el = root('<p data-x="1">до <b>Гегеля</b> после</p>');
    highlightTerms(el, ['гегел']);
    const p = el.querySelector('p') as HTMLElement;
    expect(p.getAttribute('data-x')).toBe('1');
    expect(p.querySelector('b mark.search-hit')?.textContent).toBe('Гегеля');
    expect(el.textContent).toBe('до Гегеля после');
  });

  // Формулы рисует KaTeX, и её вывод — это и скрытый исходник TeX
  // (annotation внутри katex-mathml), и разложенная по спанам формула, где
  // каждый знак — отдельный текстовый узел. Числовой или однобуквенный
  // запрос («2», «x») обернул бы такие знаки в mark с внутренними отступами
  // и разъехавшейся формулой. В корпусе полос с формулами всего девять —
  // это профилактика, а не починка.
  it('не заходит внутрь формулы KaTeX', () => {
    const el = root(
      '<p>Гегеля читали</p>' +
        '<span class="katex"><span class="katex-mathml"><math><semantics><mrow>' +
        '<mi>x</mi></mrow><annotation encoding="application/x-tex">x^2</annotation>' +
        '</semantics></math></span><span class="katex-html" aria-hidden="true">' +
        '<span class="base"><span class="mord mathnormal">x</span>' +
        '<span class="msupsub"><span class="mord">2</span></span></span></span></span>',
    );
    expect(highlightTerms(el, ['2'])).toBe(0);
    expect(el.querySelector('.katex mark')).toBeNull();
    // Текст рядом с формулой по-прежнему подсвечивается.
    expect(highlightTerms(el, ['гегел'])).toBe(1);
    expect(el.querySelector('.katex mark')).toBeNull();
  });

  it('пропускает содержимое script и style', () => {
    const el = root('<p>Гегеля<script>var гегель = 1;</script><style>.гегель{}</style></p>');
    const count = highlightTerms(el, ['гегел']);
    expect(count).toBe(1);
    expect(el.querySelector('script mark')).toBeNull();
    expect(el.querySelector('style mark')).toBeNull();
  });
});
