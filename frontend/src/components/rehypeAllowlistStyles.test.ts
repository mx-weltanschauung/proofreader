import { describe, it, expect } from 'vitest';
import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkMath from 'remark-math';
import remarkRehype from 'remark-rehype';
import rehypeRaw from 'rehype-raw';
import rehypeKatex from 'rehype-katex';
import rehypeSanitize from 'rehype-sanitize';
import rehypeStringify from 'rehype-stringify';
import { markdownPreviewSanitizeSchema } from './markdownSanitizeSchema';
import { rehypeAllowlistStyles, filterStyleValue } from './rehypeAllowlistStyles';
import { KATEX_SAMPLE_FORMULAS } from '../test/katexSampleFormulas';

// Здесь конвейер собран руками ровно в том же порядке, в каком его собирает
// MarkdownEditor в режиме sanitizeUntrustedContent, но без React и jsdom.
// Так проверка идёт по строке HTML, которую отдаёт rehype, а не по тому,
// что из неё оставил CSS-парсер jsdom: jsdom — не браузер, его cssstyle
// может сам выбросить экзотическую декларацию, и тест «в innerHTML нет
// attacker.example» тогда доказывал бы не фильтр, а jsdom. Парные тесты
// поверх React лежат в MarkdownEditor.sanitize.test.tsx.
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

function withStyle(css: string): string {
  return render(`Текст <span style="${css.replace(/"/g, '&quot;')}">маячок</span> хвост`);
}

const ATTACKER = 'attacker.example';

describe('rehypeAllowlistStyles — значение style по белому списку', () => {
  // Обычный url() ловил и чёрный список круга 3.
  it('обычный url() в style не доезжает до вывода', () => {
    const html = withStyle('background:url(https://attacker.example/x.png)');
    expect(html).not.toContain(ATTACKER);
    expect(html).not.toMatch(/url\s*\(/i);
    expect(html).toContain('маячок');
  });

  // А это — то, на чём чёрный список круга 3 ломался. По CSS Syntax L3
  // экраны в ident-token раскрываются ДО сравнения с «url», поэтому
  // браузер видит настоящий url() и делает запрос, а регулярка — нет.
  it.each([
    ['\\75 вместо u', 'background:\\75rl(https://attacker.example/x.png)'],
    ['\\72 вместо r', 'background:u\\72l(https://attacker.example/x.png)'],
    ['\\6c вместо l', 'background:ur\\6c(https://attacker.example/x.png)'],
    ['экран с пробелом-терминатором', 'background:\\75 rl(https://attacker.example/x.png)'],
    ['экран в имени свойства', '\\62ackground:url(https://attacker.example/x.png)'],
    ['экран в @import', '@\\69mport url(https://attacker.example/z.css)'],
  ])('CSS-экран не проносит загрузку ресурса: %s', (_name, css) => {
    const html = withStyle(css);
    expect(html).not.toContain(ATTACKER);
    expect(html).not.toContain('\\');
  });

  // Загрузку ресурса в CSS можно записать и вовсе без токена url() —
  // ещё одна причина, по которой чёрный список тут не работает в принципе.
  it.each([
    ['image-set()', 'background-image:image-set("https://attacker.example/x.png" 1x)'],
    ['cursor', 'cursor:url(https://attacker.example/c.cur),auto'],
    ['border-image', 'border-image:url(https://attacker.example/b.png) 30'],
    ['list-style-image', 'list-style-image:url(https://attacker.example/l.png)'],
    ['mask', 'mask:url(https://attacker.example/m.svg)'],
    ['var() с запасным url()', 'background:var(--x,url(https://attacker.example/x.png))'],
    ['@import', '@import url(https://attacker.example/z.css)'],
  ])('загрузка ресурса без url()-токена тоже не проходит: %s', (_name, css) => {
    const html = withStyle(css);
    expect(html).not.toContain(ATTACKER);
  });

  it('легальная длина рядом с инъекцией выживает, инъекция — нет', () => {
    const html = withStyle('height:1em;background:url(https://attacker.example/x.png)');
    expect(html).not.toContain(ATTACKER);
    expect(html).toContain('height:1em');
  });

  it('обрывок, спрятанный за кавычкой с точкой с запятой, не выживает', () => {
    // Значение собирается заново из понятых деклараций, поэтому обрывку
    // негде уцелеть, как бы его ни резал токенизатор CSS.
    const html = withStyle('height:1em;/*";*/background:url(https://attacker.example/x.png)');
    expect(html).not.toContain(ATTACKER);
    expect(html).not.toContain('/*');
    expect(html).toContain('height:1em');
  });

  it('expression() и прочий мусор без белого списка отбрасываются', () => {
    expect(withStyle('width:expression(alert(1))')).not.toContain('expression');
    expect(withStyle('position:fixed;z-index:99999')).not.toContain('fixed');
    expect(withStyle('background:red')).not.toContain('background');
  });

  it('пустой после фильтра style удаляется целиком, а не остаётся пустым', () => {
    const html = withStyle('background:url(https://attacker.example/x.png)');
    expect(html).toMatch(/<span>маячок/);
  });
});

describe('rehypeAllowlistStyles — вёрстка KaTeX цела', () => {
  it('ни одна декларация style, которую выдаёт KaTeX, не теряется на фильтре', () => {
    // Сторожевой тест к белому списку: если katex/rehype-katex в очередной
    // версии начнут выдавать свойство, которого в списке нет, формулы в
    // режиме sanitizeUntrustedContent поедут молча — а этот тест упадёт и
    // назовёт свойство. Пересобрать список: npm run measure:katex-styles.
    const bare = unified()
      .use(remarkParse)
      .use(remarkMath)
      .use(remarkRehype, { allowDangerousHtml: true })
      .use(rehypeRaw)
      .use(rehypeKatex);

    const lost = new Set<string>();
    let seen = 0;

    for (const formula of KATEX_SAMPLE_FORMULAS) {
      const tree = bare.runSync(bare.parse(formula));
      const walk = (node: {
        type: string;
        properties?: Record<string, unknown>;
        children?: unknown[];
      }) => {
        if (node.type === 'element' && typeof node.properties?.style === 'string') {
          const original = node.properties.style;
          const kept = filterStyleValue(original);
          for (const declaration of original.split(';')) {
            const raw = declaration.trim();
            if (!raw) continue;
            seen++;
            const property = raw.slice(0, raw.indexOf(':')).trim();
            const value = raw.slice(raw.indexOf(':') + 1).trim();
            if (!kept.includes(`${property.toLowerCase()}:${value}`)) {
              lost.add(raw);
            }
          }
        }
        (node.children as (typeof node)[] | undefined)?.forEach(walk);
      };
      walk(tree as never);
    }

    expect(seen).toBeGreaterThan(1000);
    expect([...lost]).toEqual([]);
  });

  it('формула рендерится со своими инлайновыми стилями, svg и path', () => {
    const html = render('Формула $x^2 + \\sqrt{y}$ рядом');
    expect(html).toContain('class="katex"');
    expect(html).toMatch(/<span class="strut" style="height:[^"]*vertical-align:[^"]*"/);
    expect(html).toContain('<svg');
    expect(html).toContain('<path');
    // Инлайновых стилей в формуле — не один-два, а вся вёрстка.
    expect((html.match(/style="/g) ?? []).length).toBeGreaterThan(10);
  });

  it('svg стрелки над \\vec сохраняет свой инлайновый width', () => {
    // Замер: единственный не-span тег с осмысленным для экрана `style` —
    // стрелка вектора. Круг 2 её style терял (схема разрешала style только
    // на span), круг 4 разрешил и отфильтровал по той же форме значения.
    const html = render('$\\vec{v}$');
    expect(html).toMatch(/<svg[^>]*style="width:[\d.]+em"/);
  });

  it('mstyle у \\pmb сохраняет text-shadow из трёх длин', () => {
    const html = render('$\\pmb{b}$');
    expect(html).toMatch(/<mstyle[^>]*style="text-shadow:[\d.]+em [\d.]+em [\d.]+px"/);
  });

  it('обводка \\cancel рисуется — тег line не выброшен схемой', () => {
    const html = render('$\\cancel{x}$');
    expect(html).toContain('<line');
  });
});

describe('filterStyleValue — единичные проверки формы значения', () => {
  it.each([
    ['height:0.8974em', 'height:0.8974em'],
    ['vertical-align:-0.0833em', 'vertical-align:-0.0833em'],
    ['margin:0 -0.02em', 'margin:0 -0.02em'],
    ['text-shadow:0.02em 0.01em 0.04px', 'text-shadow:0.02em 0.01em 0.04px'],
    ['position:relative', 'position:relative'],
    ['border-style:solid', 'border-style:solid'],
    ['color:transparent', 'color:transparent'],
    ['color:#cc0000', 'color:#cc0000'],
    ['width:0.471em', 'width:0.471em'],
    ['min-width:1.02em', 'min-width:1.02em'],
    ['HEIGHT: 1em ;', 'height:1em'],
  ])('пропускает %s', (input, expected) => {
    expect(filterStyleValue(input)).toBe(expected);
  });

  it.each([
    'background:url(https://attacker.example/x.png)',
    'background:\\75rl(https://attacker.example/x.png)',
    'position:fixed',
    'border-style:none',
    'color:rgb(1,2,3)',
    'height:calc(1em + 2px)',
    'height:1em!important',
    'content:"\\75rl"',
    'margin:1em 1em 1em 1em 1em',
    'text-shadow:0.02em',
    '--custom:url(https://attacker.example/x.png)',
    'height',
    ':1em',
  ])('отбрасывает %s', (input) => {
    expect(filterStyleValue(input)).toBe('');
  });
});
