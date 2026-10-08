import { describe, it, expect } from 'vitest';
import {
  citationHtml,
  citationLinkHtml,
  citationLinkMarkdown,
  citationMarkdown,
  citationSignature,
  folioLabel,
  seamMarker,
} from './citation';

const base = {
  author: '',
  workTitle: 'Критика Готской программы',
  editionTitle: 'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд.',
  volumeTitle: 'К. Маркс и Ф. Энгельс. Сочинения. Том 19',
  volumeNumber: 19,
  folios: ['9'] as (string | null)[],
  pageNumbers: [9],
};

describe('citationSignature', () => {
  // Основной случай корпуса, а не край: works.author пуст у всех 159 работ,
  // автор приезжает первым словом издания.
  it('без автора печатает произведение и издание', () => {
    expect(citationSignature(base)).toBe(
      'Критика Готской программы // К. Маркс и Ф. Энгельс. Сочинения, 2-е изд., т. 19, с. 9.',
    );
  });

  it('автор, когда он есть, идёт первым и с точкой', () => {
    expect(citationSignature({ ...base, author: 'К. Маркс' })).toBe(
      'К. Маркс. Критика Готской программы // К. Маркс и Ф. Энгельс. Сочинения, 2-е изд., т. 19, с. 9.',
    );
  });

  // Разделитель — запятая: издания корпуса оканчиваются точкой, и Join через
  // ". " печатал бы двойную (те самые грабли joinSourceParts).
  it('точка в конце издания не даёт двойной точки', () => {
    expect(citationSignature(base)).not.toContain('изд.., ');
    expect(citationSignature(base)).toContain('изд., т. 19');
  });

  it('аппаратная полоса идёт без названия произведения', () => {
    expect(citationSignature({ ...base, workTitle: '' })).toBe(
      'К. Маркс и Ф. Энгельс. Сочинения, 2-е изд., т. 19, с. 9.',
    );
  });

  it('работа вне собрания подписывается своим названием', () => {
    expect(
      citationSignature({
        ...base,
        editionTitle: '',
        volumeNumber: undefined,
        volumeTitle: 'Сам по себе том',
      }),
    ).toBe('Критика Готской программы // Сам по себе том, с. 9.');
  });

  it('часть тома печатается после номера', () => {
    expect(citationSignature({ ...base, volumeNumber: 26, volumePart: 'III' })).toContain(
      'т. 26, ч. III, с. 9.',
    );
  });

  // Пустой folioLabel убирается source.filter(Boolean), а не печатает
  // «с. undefined» перед точкой.
  it('неопределённые страницы не печатают undefined', () => {
    expect(citationSignature({ ...base, folios: [], pageNumbers: [] })).toBe(
      'Критика Готской программы // К. Маркс и Ф. Энгельс. Сочинения, 2-е изд., т. 19.',
    );
  });
});

describe('folioLabel', () => {
  it('одна полоса с колонцифрой', () => {
    expect(folioLabel(['233'], [233])).toBe('с. 233');
  });

  it('полоса без колонцифры называет сквозной номер', () => {
    expect(folioLabel([null], [4])).toBe('б/н, полоса 4');
  });

  it('диапазон печатает реальные концы', () => {
    expect(folioLabel(['233', '235'], [233, 235])).toBe('с. 233—235');
  });

  it('диапазон без колонцифр идёт полосами', () => {
    expect(folioLabel([null, null], [4, 6])).toBe('б/н, полосы 4—6');
  });

  // Пустой вход — не край, а обычный «страницы цитаты не определены»:
  // folios.every(...) на пустом массиве истинно вакуумно, и без явной
  // проверки первая ветка печатала бы «с. undefined».
  it('пустые массивы не печатают undefined', () => {
    expect(folioLabel([], [])).toBe('');
  });

  // Массивы приходят с разных концов подготовки цитаты и не обязаны
  // совпадать длиной; folios короче pageNumbers не должен решать за диапазон,
  // который называет pageNumbers, — считать allFolios нужно по позициям
  // first/last САМОГО pageNumbers, а не по всей длине folios.
  it('несовпадающая длина массивов не печатает undefined', () => {
    expect(folioLabel(['233', '235'], [233, 235, 240])).toBe('б/н, полосы 233—240');
  });
});

describe('две грани буфера', () => {
  // Адрес — отдельной строкой, а не хвостом подписи: читатель копирует его
  // отдельно (двойным щелчком, «выделить строку»), и склеенный с «с. 370.»
  // он каждый раз захватывался вместе с концом подписи.
  it('markdown: цитата, пустая строка, подпись, адрес своей строкой', () => {
    expect(citationMarkdown('первая\nвторая', 'Подпись.', 'https://x/y')).toBe(
      '> первая\n> вторая\n\nПодпись.\nhttps://x/y\n',
    );
  });

  it('markdown ветки «Ссылка»: подпись и адрес своей строкой, без цитаты', () => {
    expect(citationLinkMarkdown('Подпись.', 'https://x/y')).toBe('Подпись.\nhttps://x/y\n');
  });

  it('html: blockquote и ЖИВАЯ ссылка — она единственный носитель состояния', () => {
    const html = citationHtml('текст', 'Подпись.', 'https://x/y');
    expect(html).toContain('<blockquote>');
    expect(html).toContain('<a href="https://x/y">https://x/y</a>');
    // Обе грани буфера говорят одно и то же: адрес отбит от подписи и там.
    expect(html).toContain('Подпись.<br><a ');
  });

  it('html экранирует текст цитаты', () => {
    expect(citationHtml('<b>не тег</b>', 'П.', 'https://x/y')).toContain(
      '&lt;b&gt;не тег&lt;/b&gt;',
    );
  });
});

// Фикс-раунд 1, мелкое б: ветка «Ссылка» (без выделения) раньше собирала тег
// вручную, минуя esc(), которым проходит citationHtml, — имя автора со
// знаком `<`/`&` сломало бы разметку письма/документа, куда цитата
// вставляется.
describe('citationLinkHtml', () => {
  it('подпись и адрес, без цитаты', () => {
    expect(citationLinkHtml('Подпись.', 'https://x/y')).toBe(
      '<p>Подпись.<br><a href="https://x/y">https://x/y</a></p>',
    );
  });

  it('экранирует подпись', () => {
    expect(citationLinkHtml('А. Б. & <В>', 'https://x/y')).toContain('А. Б. &amp; &lt;В&gt;');
  });

  it('экранирует адрес — и в тексте, и в href', () => {
    const html = citationLinkHtml('Подпись.', 'https://x/y?a=1&b=2');
    expect(html).toContain('href="https://x/y?a=1&amp;b=2"');
    expect(html).toContain('>https://x/y?a=1&amp;b=2<');
  });
});

describe('seamMarker', () => {
  it('печатает колонцифру стыка', () => {
    expect(seamMarker('234', 234)).toBe('[с. 234]');
  });

  it('у полосы без колонцифры — сквозной номер', () => {
    expect(seamMarker(null, 4)).toBe('[полоса 4]');
  });
});
