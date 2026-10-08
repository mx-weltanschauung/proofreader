// Замер: что KaTeX реально кладёт в инлайновый `style`.
//
// Белый список в `src/components/rehypeAllowlistStyles.ts` выведен этим
// скриптом, а не по памяти. Прогоняет представительный набор формул через
// НАСТОЯЩИЙ конвейер (remark-math -> remark-rehype -> rehype-raw ->
// rehype-katex), собирает все встреченные декларации `style` и печатает
// таблицу «свойство -> форма значения». Вторым проходом прогоняет то же
// через схему санитайзера и показывает, что схема теряет.
//
//     npm run measure:katex-styles
//
// Запускать после обновления katex/rehype-katex: если в таблице появилось
// свойство, которого нет в белом списке, вёрстка формул в режиме
// `sanitizeUntrustedContent` поедет — тест
// `rehypeAllowlistStyles.test.ts` («ни одна декларация KaTeX не теряется»)
// упадёт на том же самом.

import { readFileSync } from 'node:fs';

import { unified } from 'unified';
import remarkParse from 'remark-parse';
import remarkMath from 'remark-math';
import remarkRehype from 'remark-rehype';
import rehypeRaw from 'rehype-raw';
import rehypeKatex from 'rehype-katex';
import rehypeSanitize from 'rehype-sanitize';
import { markdownPreviewSanitizeSchema } from '../src/components/markdownSanitizeSchema.ts';
import { KATEX_SAMPLE_FORMULAS } from '../src/test/katexSampleFormulas.ts';

const base = unified()
  .use(remarkParse)
  .use(remarkMath)
  .use(remarkRehype, { allowDangerousHtml: true })
  .use(rehypeRaw)
  .use(rehypeKatex);

const sanitized = unified()
  .use(remarkParse)
  .use(remarkMath)
  .use(remarkRehype, { allowDangerousHtml: true })
  .use(rehypeRaw)
  .use(rehypeKatex)
  .use(rehypeSanitize, markdownPreviewSanitizeSchema);

function collect(processor) {
  const tagsOfProp = new Map();
  const propValues = new Map();
  const styleTags = new Map();
  const classNames = new Map(); // класс -> сколько раз встретился
  const classTags = new Map(); // тег -> Set классов
  const tags = new Set();
  let styled = 0;

  for (const formula of KATEX_SAMPLE_FORMULAS) {
    const tree = processor.runSync(processor.parse(formula));
    const walk = (node) => {
      if (node.type === 'element') {
        tags.add(node.tagName);
        for (const key of Object.keys(node.properties ?? {})) {
          if (!tagsOfProp.has(key)) tagsOfProp.set(key, new Set());
          tagsOfProp.get(key).add(node.tagName);
        }
        const className = node.properties?.className;
        if (className !== undefined) {
          const list = Array.isArray(className) ? className : String(className).split(/\s+/);
          if (!classTags.has(node.tagName)) classTags.set(node.tagName, new Set());
          for (const name of list) {
            if (!name) continue;
            classNames.set(name, (classNames.get(name) ?? 0) + 1);
            classTags.get(node.tagName).add(name);
          }
        }

        const style = node.properties?.style;
        if (typeof style === 'string') {
          styled++;
          styleTags.set(node.tagName, (styleTags.get(node.tagName) ?? 0) + 1);
          for (const declaration of style.split(';')) {
            const raw = declaration.trim();
            if (!raw) continue;
            const at = raw.indexOf(':');
            const property = raw.slice(0, at).trim();
            const value = raw.slice(at + 1).trim();
            if (!propValues.has(property)) propValues.set(property, new Set());
            propValues.get(property).add(value);
          }
        }
      }
      (node.children ?? []).forEach(walk);
    };
    walk(tree);
  }
  return { tagsOfProp, propValues, styleTags, classNames, classTags, tags, styled };
}

const before = collect(base);
const after = collect(sanitized);

console.log(`Формул прогнано: ${KATEX_SAMPLE_FORMULAS.length}`);
console.log(`Узлов с инлайновым style: ${before.styled}\n`);

console.log('=== теги, несущие style ===');
for (const [tag, n] of [...before.styleTags].sort((a, b) => b[1] - a[1])) {
  console.log(`  ${tag}: ${n}`);
}

console.log('\n=== свойства style: сколько значений и как они выглядят ===');
for (const [property, values] of [...before.propValues].sort()) {
  const list = [...values].sort();
  console.log(`  ${property.padEnd(22)} ${String(list.length).padStart(4)} знач.  напр. ${list.slice(0, 4).join(' | ')}`);
}

console.log('\n=== теги, несущие className, и сколько разных классов ===');
for (const [tag, names] of [...before.classTags].sort()) {
  console.log(`  ${tag}: ${names.size}`);
}

console.log(`\n=== значения className (${before.classNames.size} разных) ===`);
for (const [name, n] of [...before.classNames].sort((a, b) => b[1] - a[1] || a[0].localeCompare(b[0]))) {
  console.log(`  ${name.padEnd(24)} ${n}`);
}

// Второй источник классов — сам katex.min.css: там перечислено ВСЁ, что
// KaTeX стилизует, включая семейства, которых на наборе формул не
// оказалось (size5..size11, reset-size1..11, delim-size1/4, cd-* у
// коммутативных диаграмм, sout, angl, tag). Набор формул показывает, что
// KaTeX ставит на практике; его собственный CSS — какой у него словарь
// вообще. Белый список схемы — объединение обоих замеров: иначе формула,
// не попавшая в набор, теряла бы класс и ехала бы молча.
const katexCssClasses = (() => {
  const css = readFileSync(
    new URL('../node_modules/katex/dist/katex.min.css', import.meta.url),
    'utf8',
  );
  const names = new Set();
  for (const match of css.matchAll(/\.(-?[_a-zA-Z][\w-]*)/g)) names.add(match[1]);
  // Расширения файлов шрифтов из url(...) — не классы.
  for (const junk of ['woff', 'woff2', 'ttf']) names.delete(junk);
  return names;
})();

console.log(`\n=== классы в katex.min.css (${katexCssClasses.size}) ===`);
console.log(`  ${[...katexCssClasses].sort().join(' ')}`);

const union = [...new Set([...before.classNames.keys(), ...katexCssClasses])].sort();
console.log(`\n=== объединение обоих замеров: белый список схемы (${union.length}) ===`);
console.log(union.map((n) => `'${n}',`).join(' '));

console.log('\n=== классы, потерянные схемой ===');
{
  const lost = [...before.classNames.keys()].filter((n) => !after.classNames.has(n)).sort();
  console.log(`  ${lost.join(' ') || '—'}`);
}

console.log('\n=== что схема санитайзера выбрасывает ===');
const lostTags = [...before.tags].filter((t) => !after.tags.has(t));
console.log(`  теги целиком: ${lostTags.join(' ') || '—'}`);
let lostAttrs = 0;
for (const [property, tags] of [...before.tagsOfProp].sort()) {
  const survivors = after.tagsOfProp.get(property) ?? new Set();
  const lost = [...tags].filter((t) => !survivors.has(t)).sort();
  if (lost.length) {
    lostAttrs++;
    console.log(`  ${property}: у ${lost.join(', ')}`);
  }
}
if (!lostAttrs) console.log('  атрибуты: —');
