import { describe, it, expect } from 'vitest';
import { readFileSync } from 'node:fs';
import { join } from 'node:path';

/**
 * Маршруты читальни грузятся лениво (React.lazy в App.tsx), и Vite режет CSS
 * по тем же чанкам: стили, объявленные в файле, который данный маршрут не
 * импортирует, до него НЕ доезжают — ни в сборке, ни в дев-сервере.
 *
 * Отсюда правило: компонент, рисующий разметку читалки, обязан сам
 * импортировать стили, которыми она держится, а не рассчитывать на соседа.
 * Проверено на сборке: в `WorkRead-*.css` не было ни строчки про
 * `.page-marker`, и номера полос в потоковом чтении ехали неоформленной
 * синей ссылкой над текстом вместо мелкой цифры на поле. Пока номера
 * включались тумблером, это видел редкий читатель; с умолчанием «показывать»
 * — каждый.
 *
 * Проверка исходников, а не поведения: раскладку по чанкам делает сборщик, и
 * в jsdom её не увидеть — там глобальны вообще все стили.
 */
const DIR = join(__dirname);

function source(file: string): string {
  return readFileSync(join(DIR, file), 'utf8');
}

/** Файлы стилей, объявляющие разметку читалки, и кто обязан их импортировать. */
const SHARED: { css: string; marker: string; importers: string[] }[] = [
  {
    css: './ReadingSurface.css',
    // Правила маркера полосы и пары подписей на кнопках панели.
    marker: '.page-marker',
    importers: ['ReadingSurface.tsx', 'ReadingChunk.tsx', 'ReadingStreamBar.tsx'],
  },
];

describe('стили читалки доезжают до всех ленивых маршрутов', () => {
  for (const { css, marker, importers } of SHARED) {
    const file = css.replace('./', '');

    it(`${file} объявляет ${marker}`, () => {
      expect(source(file)).toContain(marker);
    });

    for (const importer of importers) {
      it(`${importer} импортирует ${file} сам`, () => {
        expect(source(importer)).toContain(`import '${css}'`);
      });
    }
  }
});

/**
 * Фикс-раунд 2 (кнопка «Цитировать»/«Ссылка»): та же беда с ленивыми чанками,
 * но с другим лицом — не «стиль не доехал», а «доехал вторым и молча выиграл
 * чужое правило». `.cite-button .toolbar-label-short { display: none }` имел
 * ту же специфичность (0,2,0), что и `.chapter-view-actions .toolbar-label-
 * short { display: inline }` в ReadingSurface.css под @media(max-width:
 * 768px) — ничья решалась порядком загрузки CSS-чанков, который назначает
 * Vite, а не автор файла. На ChapterView чанк CiteButton ехал ПОСЛЕ
 * ReadingSurface и на ≤768px гасил оба спана: кнопка оставалась совсем без
 * подписи. На WorkRead порядок чанков был обратный, и совпадение спасало.
 *
 * Починка — не победа в этой специфичности (та же ненадёжность на шаг
 * дальше), а отказ от пары подписей: «Цитировать» и «Ссылка» короткие сами по
 * себе, короткая форма не отличается от полной, и переключать вообще нечего.
 * Сторож не даёт паре вернуться незаметно — источник, а не поведение в
 * рантайме, потому что именно раскладку по чанкам jsdom не считает.
 */
/** Комментарии описывают прошлую ошибку словами «toolbar-label» нарочно —
 *  сторожить нужно код, а не рассказ о нём. */
function stripCssComments(text: string): string {
  return text.replace(/\/\*[\s\S]*?\*\//g, '');
}

function stripLineComments(text: string): string {
  return text
    .split('\n')
    .filter((line) => !line.trim().startsWith('//') && !line.trim().startsWith('*'))
    .join('\n');
}

describe('кнопка «Цитировать»/«Ссылка» не заводит пару подписей', () => {
  it('CiteButton.css не содержит правил toolbar-label', () => {
    expect(stripCssComments(source('CiteButton.css'))).not.toMatch(/toolbar-label/);
  });

  it('CiteButton.tsx не рисует класс toolbar-label-*', () => {
    expect(stripLineComments(source('CiteButton.tsx'))).not.toMatch(/toolbar-label/);
  });
});
