import { describe, it, expect } from 'vitest';
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkMath from 'remark-math';
import remarkRehype from 'remark-rehype';
import rehypeRaw from 'rehype-raw';
import rehypeKatex from 'rehype-katex';
import rehypeSanitize from 'rehype-sanitize';
import rehypeStringify from 'rehype-stringify';
import {
  KATEX_CLASS_NAMES,
  markdownPreviewSanitizeSchema,
  networkFetchingSurface,
} from './markdownSanitizeSchema';
import { rehypeAllowlistStyles } from './rehypeAllowlistStyles';
import { KATEX_SAMPLE_FORMULAS } from '../test/katexSampleFormulas';

// Конвейер собран ровно в порядке MarkdownEditor в режиме
// sanitizeUntrustedContent, но без React и jsdom: проверка идёт по строке
// HTML, которую отдаёт rehype. Почему не в jsdom — см. комментарий в
// MarkdownEditor.sanitize.test.tsx (его CSS-парсер сам отвергает часть
// нагрузок, и тест был бы зелёным вовсе без фильтра).
const pipeline = unified()
  .use(remarkParse)
  .use(remarkMath)
  .use(remarkRehype, { allowDangerousHtml: true })
  .use(rehypeRaw)
  .use(rehypeKatex)
  .use(rehypeSanitize, markdownPreviewSanitizeSchema)
  .use(rehypeAllowlistStyles)
  .use(rehypeStringify);

function render(markdown: string): string {
  return String(pipeline.processSync(markdown));
}

const ATTACKER = 'attacker.example';

// ---------------------------------------------------------------------------

const NETWORK_PAYLOADS: [string, string][] = [
  ['markdown-картинка', '![подпись](https://attacker.example/x.png)'],
  ['сырой img[src]', '<img src="https://attacker.example/y.png">'],
  ['img[srcset]', '<img srcset="https://attacker.example/y2.png 1x">'],
  [
    'picture + source[srcset]',
    '<picture><source srcset="https://attacker.example/s.png"><img src="https://attacker.example/f.png"></picture>',
  ],
  [
    'video[src] + poster',
    '<video src="https://attacker.example/v.mp4" poster="https://attacker.example/p.png"></video>',
  ],
  ['audio[src]', '<audio src="https://attacker.example/a.mp3"></audio>'],
  ['track[src]', '<track src="https://attacker.example/t.vtt">'],
  ['input[type=image]', '<input type="image" src="https://attacker.example/i.png">'],
  ['svg use[href]', '<svg><use href="https://attacker.example/u.svg#a"/></svg>'],
  ['svg use[xlink:href]', '<svg><use xlink:href="https://attacker.example/u2.svg#a"/></svg>'],
  ['svg image[href]', '<svg><image href="https://attacker.example/z.png"/></svg>'],
  ['svg image[xlink:href]', '<svg><image xlink:href="https://attacker.example/z2.png"/></svg>'],
  ['link[rel=stylesheet]', '<link rel="stylesheet" href="https://attacker.example/s.css">'],
  ['iframe[src]', '<iframe src="https://attacker.example/f"></iframe>'],
  ['object[data]', '<object data="https://attacker.example/o.swf"></object>'],
  ['embed[src]', '<embed src="https://attacker.example/e">'],
  ['background у body', '<body background="https://attacker.example/bg.png">'],
  [
    'background у table',
    '<table background="https://attacker.example/tb.png"><tr><td>x</td></tr></table>',
  ],
  ['mglyph[src]', '<math><mglyph src="https://attacker.example/g.png"/></math>'],
  // Краска SVG принимает url(...) — это была живая протечка, пережившая
  // круги 4 и 5: атрибут `stroke` у <line> попал в схему в круге 4 как
  // «инертный сосед», не будучи замеренным.
  [
    'line[stroke] с url()',
    '<svg width="20" height="20"><line x1="0" y1="0" x2="20" y2="20" stroke="url(https://attacker.example/x.svg#g)"/></svg>',
  ],
  [
    'line[stroke] с url() в кавычках',
    '<svg><line x1="0" y1="0" x2="9" y2="9" stroke="url(&quot;https://attacker.example/z.svg#g&quot;)"/></svg>',
  ],
  [
    'line[stroke-linecap] с url()',
    '<svg><line stroke-linecap="url(https://attacker.example/y.svg#g)" x1="0" y1="0" x2="9" y2="9"/></svg>',
  ],
  [
    'path[stroke] с url()',
    '<svg><path d="M0 0" stroke="url(https://attacker.example/p.svg#g)"/></svg>',
  ],
  [
    'svg[fill] с url()',
    '<svg fill="url(https://attacker.example/f.svg#g)"><line x1="0" y1="0" x2="9" y2="9"/></svg>',
  ],
];

describe('схема предпросмотра — автоматические сетевые обращения закрыты как класс', () => {
  it.each(NETWORK_PAYLOADS)('браузер модератора не пойдёт в сеть сам: %s', (_name, markdown) => {
    const html = render(markdown);
    expect(html).not.toContain(ATTACKER);
  });

  it('markdown-картинка не оставляет узла, способного сделать запрос', () => {
    const html = render('![подпись](https://attacker.example/x.png)');
    expect(html).not.toContain('<img');
    expect(html).not.toContain(ATTACKER);
  });

  it('ссылка остаётся ссылкой — переход по клику это осознанное действие', () => {
    // Граница класса: `a href` ведёт в сеть по клику, адрес виден. А вот
    // `ping` уходит сам при клике, фоном и невидимо — он снят.
    const html = render(
      '<a href="https://example.org/l" ping="https://attacker.example/p">текст</a>',
    );
    expect(html).toContain('href="https://example.org/l"');
    expect(html).not.toContain('ping');
    expect(html).not.toContain(ATTACKER);
  });

  it('в эффективном наборе схемы не осталось ни одного сетевого атрибута', () => {
    // Сторожевой тест к КЛАССУ, а не к перечню тегов: если обновление
    // hast-util-sanitize вернёт в defaultSchema что-нибудь с src или
    // poster, вычитание сработает само, а этот тест подтвердит, что
    // сработало. Проверяется тот же перечень, которым схема вычитает, —
    // иначе тест сверялся бы сам с собой по другому списку.
    const nameOf = (definition: unknown) =>
      Array.isArray(definition) ? String(definition[0]) : String(definition);
    const attributes = markdownPreviewSanitizeSchema.attributes ?? {};
    const survivors: string[] = [];

    for (const [tagName, definitions] of Object.entries(attributes)) {
      for (const definition of definitions ?? []) {
        const name = nameOf(definition).toLowerCase();
        if (networkFetchingSurface.attributeNames.has(name)) survivors.push(`${tagName}[${name}]`);
        if (name === 'href' && !networkFetchingSurface.hrefAllowedOn.has(tagName)) {
          survivors.push(`${tagName}[href]`);
        }
      }
    }

    expect(survivors).toEqual([]);
  });

  it('в tagNames не осталось ни одного тега, который грузит ресурс сам', () => {
    const survivors = (markdownPreviewSanitizeSchema.tagNames ?? []).filter((tagName) =>
      networkFetchingSurface.tagNames.has(tagName),
    );
    expect(survivors).toEqual([]);
  });

  it('набор тегов не расширился незаметно — новый тег требует пересмотра', () => {
    // Вычитание закрывает то, что уже названо классом. Этот тест ловит
    // обратное: тег, которого в разобранном наборе ещё не было. Обновили
    // hast-util-sanitize, в defaultSchema приехало что-то новое — тест
    // падает и требует решить, лезет оно в сеть или нет, вместо того
    // чтобы пропустить молча.
    const reviewed = new Set([
      // разметка текста и таблиц из defaultSchema
      'a',
      'b',
      'blockquote',
      'br',
      'code',
      'dd',
      'del',
      'details',
      'div',
      'dl',
      'dt',
      'em',
      'h1',
      'h2',
      'h3',
      'h4',
      'h5',
      'h6',
      'hr',
      'i',
      'input',
      'ins',
      'kbd',
      'li',
      'ol',
      'p',
      'pre',
      'q',
      'rp',
      'rt',
      'ruby',
      's',
      'samp',
      'section',
      'span',
      'strike',
      'strong',
      'sub',
      'summary',
      'sup',
      'table',
      'tbody',
      'td',
      'tfoot',
      'th',
      'thead',
      'tr',
      'tt',
      'ul',
      'var',
      // MathML + SVG, добавленные под KaTeX
      'math',
      'semantics',
      'annotation',
      'annotation-xml',
      'mrow',
      'mi',
      'mn',
      'mo',
      'ms',
      'mtext',
      'mspace',
      'msqrt',
      'mroot',
      'mfrac',
      'msub',
      'msup',
      'msubsup',
      'munder',
      'mover',
      'munderover',
      'mmultiscripts',
      'mtable',
      'mtr',
      'mtd',
      'mlabeledtr',
      'maction',
      'mstyle',
      'mpadded',
      'mphantom',
      'menclose',
      'mprescripts',
      'none',
      'svg',
      'path',
      'line',
    ]);
    const unreviewed = (markdownPreviewSanitizeSchema.tagNames ?? []).filter(
      (tagName) => !reviewed.has(tagName),
    );
    expect(unreviewed).toEqual([]);
  });

  it('у <line> остаются только замеренные атрибуты — краску туда не записать', () => {
    // Регресс к правке после круга 5. Из схемы убраны `stroke` и
    // `strokeLinecap`: KaTeX их не выдаёт (замер 82 формул, включая всё
    // семейство \cancel/\bcancel/\xcancel/\sout/\cancelto/\not), а цвет
    // обводки приходит из katex.min.css — `.katex svg{stroke:currentColor}`.
    const definitions = markdownPreviewSanitizeSchema.attributes?.line ?? [];
    const names = definitions.map((definition) =>
      Array.isArray(definition) ? String(definition[0]) : String(definition),
    );
    expect(names.sort()).toEqual(['strokeWidth', 'x1', 'x2', 'y1', 'y2']);
  });

  it('обводка \\cancel рисуется и без атрибута stroke — цвет идёт из CSS KaTeX', () => {
    // Парное требование к удалению: черта должна остаться на месте.
    const html = render('$\\cancel{x}$');
    expect(html).toContain('<line');
    expect(html).toMatch(/<line[^>]*stroke-width="[\d.]+em"/);
    expect(html).not.toMatch(/<line[^>]*\sstroke=/);
  });

  it('формулы вычитание не задело: \\cancel, \\vec и радикал целы', () => {
    // Парное требование. Вычитание убрало из схемы svg-теги use/image и
    // атрибут href — надо было убедиться, что KaTeX ими не пользуется.
    expect(render('$\\cancel{x}$')).toContain('<line');
    const vec = render('$\\vec{v}$');
    expect(vec).toContain('<svg');
    expect(vec).toMatch(/<svg[^>]*style="width:[\d.]+em"/);
    const sqrt = render('$\\sqrt{y}$');
    expect(sqrt).toContain('<path');
    expect(sqrt).toContain('class="katex"');
    expect(render('$$\\underbrace{a+b}_{c}$$')).toContain('class="katex"');
  });
});

describe('схема предпросмотра — значение className по белому списку', () => {
  // Схема разрешала на span голое имя `className`, то есть любое значение:
  // hast-util-sanitize сверяет имена атрибутов, а не их значения. Скрипта
  // для этого не нужно — достаточно класса читальни, чей CSS глобален.
  it('класс полотна панели сносок не доезжает до вывода', () => {
    const html = render('Текст <span class="note-sheet-scrim">маячок</span> хвост');
    // noteSheet.css: .note-sheet-scrim { position: fixed; inset: 0; z-index: 1000 }
    // — полотно во весь экран поверх интерфейса модерации.
    expect(html).not.toContain('note-sheet-scrim');
    expect(html).toContain('маячок');
  });

  it.each([
    ['полотно панели сносок', 'note-sheet-scrim'],
    ['лист панели сносок', 'note-sheet'],
    ['шапка читальни', 'header'],
    ['ловушка формы подачи', 'suggest-trap'],
    ['служебный класс очереди', 'suggestion-queue-two-columns'],
  ])('класс читальни не проносится через className: %s', (_name, cls) => {
    const html = render(`<span class="${cls}">маячок</span>`);
    expect(html).not.toContain(cls);
  });

  it('чужой класс рядом с классом KaTeX выбрасывается поштучно', () => {
    // hast-util-sanitize фильтрует список классов поэлементно, а не
    // выбрасывает атрибут целиком — на этом и держится целость вёрстки
    // формул: непрошедший класс уходит, замеренный остаётся.
    const html = render('<span class="mord note-sheet-scrim">маячок</span>');
    expect(html).not.toContain('note-sheet-scrim');
    expect(html).toContain('mord');
  });

  it('ни один класс, который KaTeX выдаёт на наборе формул, не теряется', () => {
    // Сторож к белому списку: если katex/rehype-katex в очередной версии
    // начнут ставить класс, которого в списке нет, вёрстка формул в режиме
    // sanitizeUntrustedContent поедет молча — а этот тест упадёт и назовёт
    // класс. Пересобрать список: npm run measure:katex-styles.
    const bare = unified()
      .use(remarkParse)
      .use(remarkMath)
      .use(remarkRehype, { allowDangerousHtml: true })
      .use(rehypeRaw)
      .use(rehypeKatex);

    const allowed = new Set(KATEX_CLASS_NAMES);
    const lost = new Set<string>();
    let seen = 0;

    for (const formula of KATEX_SAMPLE_FORMULAS) {
      const tree = bare.runSync(bare.parse(formula));
      const walk = (node: {
        type: string;
        properties?: Record<string, unknown>;
        children?: unknown[];
      }) => {
        if (node.type === 'element' && node.properties?.className !== undefined) {
          const raw = node.properties.className;
          const list = Array.isArray(raw) ? raw.map(String) : String(raw).split(/\s+/);
          for (const name of list) {
            if (!name) continue;
            seen++;
            if (!allowed.has(name)) lost.add(name);
          }
        }
        (node.children as (typeof node)[] | undefined)?.forEach(walk);
      };
      walk(tree as never);
    }

    expect(seen).toBeGreaterThan(500);
    expect([...lost].sort()).toEqual([]);
  });

  it('формула сохраняет свои классы после санитайзера', () => {
    // Парное требование: фикс, достигнутый выбрасыванием className целиком,
    // — не фикс. Классы несут ВСЮ вёрстку KaTeX наравне со стилями.
    const html = render('Формула $x^2 + \\sqrt{y}$ рядом');
    expect(html).toContain('class="katex"');
    expect(html).toContain('class="katex-mathml"');
    expect(html).toContain('class="katex-html"');
    expect(html).toMatch(/class="[^"]*\bvlist\b/);
    expect(html).toMatch(/class="[^"]*\bmord\b/);
    // Классов в одной формуле — десятки, а не пара.
    expect((html.match(/class="/g) ?? []).length).toBeGreaterThan(20);
  });
});
