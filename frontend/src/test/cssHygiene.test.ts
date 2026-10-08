import { describe, it, expect } from 'vitest';
import { existsSync, readdirSync, readFileSync, statSync } from 'node:fs';
import { join } from 'node:path';

const SRC = join(__dirname, '..');
const INDEX_CSS = join(SRC, 'index.css');

function cssFiles(dir: string): string[] {
  return readdirSync(dir).flatMap((entry) => {
    const full = join(dir, entry);
    if (statSync(full).isDirectory()) return cssFiles(full);
    return full.endsWith('.css') ? [full] : [];
  });
}

/** Всё, откуда цвет может попасть в интерфейс: стили и код, их пишущий. */
function sourceFiles(dir: string): string[] {
  return readdirSync(dir)
    .flatMap((entry) => {
      const full = join(dir, entry);
      if (statSync(full).isDirectory()) return sourceFiles(full);
      return /\.(css|ts|tsx)$/.test(full) ? [full] : [];
    })
    .sort();
}

describe('CSS hygiene', () => {
  // These files are plain CSS, not CSS Modules. `:global(...)` is not valid
  // CSS: browsers treat it as an unknown pseudo-class, which invalidates the
  // selector and silently drops the whole rule.
  it('uses no :global() pseudo-class anywhere', () => {
    const offenders = cssFiles(SRC).filter((f) => readFileSync(f, 'utf8').includes(':global('));
    expect(offenders).toEqual([]);
  });

  // The reading-preferences pipeline is entirely custom-property plumbing
  // (readingPrefs -> ReadingPreferencesContext -> data- attributes -> CSS
  // custom properties -> page stylesheets). A var(--reading-x) with no
  // matching declaration is not a syntax error - it computes to the
  // inherited value at that property, silently, with no console warning.
  // That is exactly how --reading-scale kept being read after it was
  // renamed to --reading-size: nothing failed, a control just went dead.
  it('references no --reading-* custom property that index.css never declares', () => {
    const files = cssFiles(SRC);
    const varRefPattern = /var\(\s*(--reading-[\w-]+)/g;
    const declPattern = /(--reading-[\w-]+)\s*:/g;

    const referenced = new Set<string>();
    for (const file of files) {
      const content = readFileSync(file, 'utf8');
      for (const match of content.matchAll(varRefPattern)) {
        referenced.add(match[1]);
      }
    }

    const indexCssPath = join(SRC, 'index.css');
    const indexCss = readFileSync(indexCssPath, 'utf8');
    const declared = new Set<string>();
    for (const match of indexCss.matchAll(declPattern)) {
      declared.add(match[1]);
    }

    const undeclared = [...referenced].filter((name) => !declared.has(name)).sort();
    expect(
      undeclared,
      `--reading-* custom properties referenced via var() but never declared in index.css: ${undeclared.join(', ')}`,
    ).toEqual([]);
  });

  // `content: '·' / ''` (альтернативный текст) Safari до 17.4 не знает: объявление
  // там недействительно, ::before не создаётся, и отступ вместе с ним пропадает.
  // Запасное объявление обычным `content` ставится строкой раньше — старый
  // браузер берёт его, новый перекрывает вариантом с альтернативным текстом.
  it('ставит перед content с альтернативным текстом запасное объявление без него', () => {
    const offenders: string[] = [];
    for (const file of cssFiles(SRC)) {
      const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
      for (const m of css.matchAll(/content:\s*([^;{}/]+?)\s*\/\s*[^;{}]+;/g)) {
        const before = css.slice(0, m.index).trimEnd();
        const fallback = `content: ${m[1]};`;
        if (!before.endsWith(fallback)) offenders.push(`${file}: ${m[0]}`);
      }
    }
    expect(offenders).toEqual([]);
  });

  // The chapter toolbar sticks below the global header at
  // top: var(--site-header-height). A declaration that never matches the root
  // is invalid at computed-value time and the toolbar sticks to top: auto - it
  // silently stops sticking, with no console warning. Checking for the string
  // anywhere under src/ would not catch that, so require a :root block.
  it('declares --site-header-height in a :root block', () => {
    const files = cssFiles(SRC);
    const referenced = files.filter((f) =>
      readFileSync(f, 'utf8').includes('var(--site-header-height'),
    );
    const declaredInRoot = files.some((f) =>
      /:root\s*\{[^}]*--site-header-height\s*:/.test(readFileSync(f, 'utf8')),
    );
    expect(referenced.length).toBeGreaterThan(0);
    expect(declaredInRoot).toBe(true);
  });

  // The whole point of the page-number gutter is that switching the mode on
  // moves no text: the padding that opens the gutter is handed back through
  // max-width, so the text's content box keeps its width and position.
  // Padding without the compensating max-width silently reintroduces exactly
  // the shift this replaced - and nothing else in the suite would notice.
  // The first matching rule is the gutter one; the narrow-screen overrides
  // that follow zero the padding out and are not what this checks.
  // Правило переехало из ChapterView.css в ReadingSurface.css вместе с самой
  // колонкой текста: читалку теперь рисует общий компонент.
  it('compensates the page-info gutter padding with max-width', () => {
    const css = readFileSync(join(SRC, 'components', 'ReadingSurface.css'), 'utf8');
    const rule = css.match(/\.chapter-pages-content\.show-page-info\s*\{[^}]*\}/);
    expect(rule, '.chapter-pages-content.show-page-info rule not found').not.toBeNull();
    expect(rule![0]).toMatch(/padding-inline:\s*3\.5rem/);
    expect(rule![0]).toMatch(/max-width:\s*calc\(var\(--reading-measure\)\s*\+\s*7rem\)/);
  });

  // Разметку сносок отдаёт RenderNotes (pkg/markdown/notes.go):
  // <div class="footnotes"><section class="notes-group"><ul class="fn-list">.
  // Правило под `ol` тут не совпадало ни разу — след того, что настоящую
  // разметку никто не открывал. Оформление сносок живёт в .chapter-footnotes
  // (ReadingSurface.css) и переиспользуется, а окну потока остаётся только
  // положение блока: у главы он один внизу, здесь — после каждой страницы.
  it('окно потока не переоформляет сноски, а лишь ставит блок на место', () => {
    const css = readFileSync(join(SRC, 'components', 'ReadingChunk.css'), 'utf8').replace(
      /\/\*[\s\S]*?\*\//g,
      '',
    );
    const rules = [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)].filter(([, selector]) =>
      selector.includes('.reading-chunk-notes'),
    );
    expect(rules.length, 'нет правил .reading-chunk-notes').toBeGreaterThan(0);

    const POSITIONAL = /^(margin|padding|border|max-width)(-[a-z-]+)?$/;
    for (const [, selector, body] of rules) {
      const name = selector.trim().replace(/\s+/g, ' ');
      expect(selector, `правило под несуществующую разметку: ${name}`).not.toMatch(/\bol\b/);
      const extra = [...body.matchAll(/([a-z][a-z-]*)\s*:/g)]
        .map((m) => m[1])
        .filter((prop) => !POSITIONAL.test(prop));
      expect(extra, `${name} переопределяет не положение: ${extra.join(', ')}`).toEqual([]);
    }
  });

  // Мера строки считается в `ch` — по шрифту того элемента, на котором
  // объявлена. На внешней оболочке экрана это интерфейсный шрифт
  // фиксированного кегля: колонка перестаёт следовать за настройкой размера
  // читального шрифта и застывает уже выбранной. Поэтому мера живёт ровно в
  // одном месте — на .chapter-pages-content в ReadingSurface.css.
  it('меру строки задаёт только колонка текста читалки', () => {
    const offenders = cssFiles(SRC)
      .filter((f) => f !== join(SRC, 'components', 'ReadingSurface.css'))
      .filter((f) => /max-width:\s*[^;}]*var\(--reading-measure/.test(readFileSync(f, 'utf8')));
    expect(offenders, offenders.join('\n')).toEqual([]);
  });

  // .page-info-header / .view-page-link were replaced by .page-marker. Half a
  // removal - markup gone but styles left, or the reverse - is invisible to
  // every other test and shows up only in the browser. Test files are skipped
  // because this one names both classes.
  it('keeps no leftovers of the old page-info header', () => {
    const walk = (dir: string): string[] =>
      readdirSync(dir).flatMap((entry) => {
        const full = join(dir, entry);
        if (statSync(full).isDirectory()) return walk(full);
        return /\.(css|ts|tsx)$/.test(full) && !full.includes('.test.') ? [full] : [];
      });
    const offenders = walk(SRC).filter((f) => {
      const content = readFileSync(f, 'utf8');
      return content.includes('page-info-header') || content.includes('view-page-link');
    });
    expect(offenders).toEqual([]);
  });

  // Единственное кольцо фокуса в проекте было rgba(6, 69, 173, .1) —
  // синий из светлой темы с прозрачностью 10%. В тёмной теме его не видно
  // вовсе, а :focus-visible не было объявлено нигде.
  it('объявляет видимое кольцо фокуса через --color-focus', () => {
    const css = readFileSync(join(SRC, 'index.css'), 'utf8');
    const rule = css.match(/:focus-visible\s*\{[^}]*\}/);
    expect(rule, ':focus-visible не объявлен в index.css').not.toBeNull();
    expect(rule![0]).toMatch(/outline:[^;]*var\(--color-focus\)/);
    expect(css).not.toMatch(/rgba\(6,\s*69,\s*173/);
  });

  // Кольцо объявлено на :focus-visible — специфичность (0,1,0). Любое
  // `outline: none` на :focus бьёт его по специфичности и гасит насовсем:
  // поле остаётся с одной сменой цвета рамки, а клавиатурный обход теряет
  // единственную метку «я здесь». Гасить мышиный фокус можно только через
  // :focus:not(:focus-visible) — тогда правило просто не совпадает там, где
  // кольцо нужно.
  it('не глушит кольцо фокуса правилами на :focus', () => {
    const walk = (dir: string): string[] =>
      readdirSync(dir).flatMap((entry) => {
        const full = join(dir, entry);
        if (statSync(full).isDirectory()) return walk(full);
        return full.endsWith('.css') ? [full] : [];
      });
    const offenders = walk(SRC).flatMap((file) => {
      const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
      return [...css.matchAll(/([^{}]+)\{([^{}]*)\}/g)]
        .filter(
          ([, selector, body]) =>
            /outline:\s*none/.test(body) &&
            selector.includes(':focus') &&
            !selector.includes(':not(:focus-visible)'),
        )
        .map(([, selector]) => `${file}: ${selector.trim().replace(/\s+/g, ' ')}`);
    });
    expect(offenders, offenders.join('\n')).toEqual([]);
  });

  // Карточки и кнопки ездят на hover через transform, а глобальный
  // transition: all на button анимирует вообще всё. Под reduce это должно
  // умолкать — иначе настройка операционной системы игнорируется.
  it('глушит движение под prefers-reduced-motion: reduce', () => {
    const css = readFileSync(join(SRC, 'index.css'), 'utf8');
    const block = css.match(/@media\s*\(prefers-reduced-motion:\s*reduce\)\s*\{[\s\S]*?\n\}/);
    expect(block, 'нет блока @media (prefers-reduced-motion: reduce)').not.toBeNull();
    expect(block![0]).toMatch(/transition-duration:\s*0\.01ms/);
    expect(block![0]).toMatch(/scroll-behavior:\s*auto/);
  });

  // Семь статусов раскрашивались светлыми пастельными плашками, зашитыми
  // в PageView.css: в тёмных темах они светят как фонарики, а
  // #fff4e6/#d97706 даёт 2.93:1 в любой теме. «Вычитано машиной» и
  // «вычитана» делят один цвет, поэтому различаются пунктиром: машинная
  // вычитка не подтверждена человеком, и это должно быть видно без цвета.
  it('красит статусы страниц токенами, а машинную вычитку метит пунктиром', () => {
    const css = readFileSync(join(SRC, 'pages', 'PageView.css'), 'utf8');
    const statuses = [
      'не_вычитана',
      'вычитывается',
      'вычитана',
      'есть_проблемы',
      'пустая_страница',
      'вычитано_машиной',
      'требует_внимания',
    ];
    for (const status of statuses) {
      const rule = css.match(new RegExp(`\\.page-status\\.status-${status}\\s*\\{[^}]*\\}`));
      expect(rule, `нет правила для статуса ${status}`).not.toBeNull();
      expect(rule![0], `статус ${status} покрашен литералом`).not.toMatch(/#[0-9a-f]{3,8}\b/i);
    }
    const machine = css.match(/\.page-status\.status-вычитано_машиной\s*\{[^}]*\}/)![0];
    expect(machine).toMatch(/border-style:\s*dashed/);
  });

  /*
   * «Вычитано машиной» делило цвет с «не вычитана»: --color-success-tint
   * против --color-bg-tertiary даёт 1.03–1.24:1 ВО ВСЕХ шести темах — на
   * полоске в 4px и на клетке обреза это неотличимо. Цветом задача не
   * решается: перебор всей палитры не находит заливки, отстоящей на 3:1 и от
   * дорожки, и от «вычитано» (лучшее — 2.15:1 в gray-dim). Поэтому фактура,
   * тем же приёмом, что и пунктирная рамка у значка статуса выше.
   */
  it('метит машинную вычитку штрихом и на полоске готовности, и на обрезе тома', () => {
    const index = readFileSync(INDEX_CSS, 'utf8');
    const root = index.match(/:root\s*\{[^}]*\}/);
    expect(root, 'нет базового блока :root').not.toBeNull();
    expect(root![0], '--pattern-machine объявлен не в базовом :root').toMatch(
      /--pattern-machine:\s*repeating-linear-gradient/,
    );

    // Чернила узора — именно --color-success: на нём держится видимость штриха
    // (3.47–7.52:1 против дорожки во всех шести темах). Подмена их обратно на
    // --color-success-tint возвращает исходный дефект — 1.03–1.24:1, неотличимо
    // от невычитанного, — и весь набор проверок этого не замечал.
    expect(root![0], 'чернила узора не --color-success').toMatch(
      /--pattern-machine:[^;]*var\(--color-success\)/,
    );

    const places: [string, string][] = [
      ['components/VolumeOutlineRow.css', '.vol-toc-ready-part.is-machine'],
      ['components/VolumeScale.css', '.vol-scale-tick.is-machine'],
      ['components/VolumeScale.css', '.vol-scale-swatch.is-machine'],
    ];
    for (const [file, selector] of places) {
      const css = readFileSync(join(SRC, file), 'utf8');
      const escaped = selector.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
      const rule = css.match(new RegExp(`${escaped}\\s*\\{[^}]*\\}`));
      expect(rule, `нет правила ${selector} в ${file}`).not.toBeNull();
      expect(rule![0], `${selector} не несёт узора`).toMatch(
        /background-image:\s*var\(--pattern-machine\)/,
      );
      expect(rule![0], `${selector} всё ещё красится тинтом`).not.toMatch(/--color-success-tint/);
    }
  });

  // Маркер сноски — надстрочная цифра кегля меньше основного: мышь по ней
  // попадает, палец нет. Зона нажатия расширяется паддингом с компенсирующим
  // отрицательным margin — паддинг на строчном элементе не трогает
  // межстрочник, а margin возвращает соседним словам их место.
  it.each([
    ['components/ReadingSurface.css', '.chapter-pages-content'],
    ['pages/PageView.css', '.page-html-content'],
  ])('в %s даёт маркеру сноски пальцевую зону нажатия', (file, scope) => {
    const css = readFileSync(join(SRC, file), 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
    const coarse = css.match(/@media\s*\(pointer:\s*coarse\)\s*\{([\s\S]*?)\n\}/);
    expect(coarse, `нет блока @media (pointer: coarse) в ${file}`).not.toBeNull();

    const escaped = `${scope} sup.footnote-ref a`.replace(/[.*+?^${}()|[\]\\]/g, '\\$&');
    const rule = coarse![1].match(new RegExp(`${escaped}\\s*\\{([^}]*)\\}`));
    expect(rule, `нет правила для маркера сноски в ${file}`).not.toBeNull();
    expect(rule![1], 'зона нажатия не расширена').toMatch(/padding:/);
    expect(rule![1], 'паддинг не скомпенсирован margin — текст разъедется').toMatch(/margin:\s*-/);
    // Иначе мобильный браузер ждёт 300 мс, не двойной ли это тап.
    expect(rule![1], 'нет touch-action: manipulation').toMatch(/touch-action:\s*manipulation/);
  });

  // Полсотни литеральных цветов вне index.css — то, из-за чего тёмная тема
  // и разъехалась: #0f7a63 это hover зелёной кнопки из СВЕТЛОЙ темы,
  // #1a5fb4 даёт 2.77:1 на тёмном фоне. Исходная поломка при этом жила не в
  // .css, а шестью зашитыми hex'ами в App.tsx, поэтому просматриваются и
  // .ts/.tsx: `style={{ background: '#2d2d2d' }}` — та же самая ошибка,
  // просто в другом файле. Исключения — только там, где литерал по существу.
  it('не содержит литеральных цветов вне index.css', () => {
    // Раньше здесь был allowlist на весь файл ReadingSettings.css целиком —
    // из-за образцов тем (см. ниже), которым литералы нужны по существу. Но
    // «на весь файл» прятал заодно и всё остальное в нём: `.rs-choice.is-active
    // { color: #fff; }` — живой контрол при 2.26:1 в gray-dim — жил рядом
    // никем не замеченный. Исключение сужено до размеченного блока: только
    // текст между маркерами `color-literal-allowed:start`/`:end` вырезается
    // перед проверкой, остальной файл читается как любой другой.
    const ALLOWED_LITERAL_BLOCK =
      /\/\*\s*color-literal-allowed:start[\s\S]*?color-literal-allowed:end\s*\*\//g;
    // Тесты держат ожидаемые значения токенов как литералы: сверять цвет
    // не с чем, если его нельзя назвать.
    const isFixture = (f: string) => /\.test\.(ts|tsx)$/.test(f);

    // Функциональные записи цвета однозначны и ловятся без контекста.
    const FUNCTIONAL =
      /#[0-9a-f]{3,8}\b|(?:rgba?|hsla?|hwb|lab|lch|oklab|oklch|color-mix)\([^)]*\)?/gi;
    // Имена цветов — обычные слова («red flag»), поэтому ловятся только как
    // значение цветового свойства, в CSS-написании и в camelCase из JSX.
    const PROPS =
      'color|background|background-color|backgroundColor|border|border-color|borderColor|' +
      'border-(?:top|bottom|left|right)-color|outline|outline-color|outlineColor|fill|stroke|' +
      'box-shadow|boxShadow|text-shadow|textShadow|caret-color|caretColor|accent-color|' +
      'accentColor|column-rule-color|text-decoration-color|textDecorationColor';
    const NAMES =
      'white|black|red|green|blue|yellow|orange|purple|pink|brown|gr[ae]y|silver|gold|' +
      'navy|teal|olive|maroon|lime|aqua|cyan|magenta|crimson|indigo|violet|turquoise|' +
      'coral|salmon|khaki|beige|ivory|lavender|plum|tomato|orchid';
    const NAMED = new RegExp(
      String.raw`(?:^|[\s;{,'"])(?:${PROPS})\s*:\s*['"]?[^;}\n]*?\b(?:${NAMES})\b`,
      'gi',
    );

    const offenders = sourceFiles(SRC)
      .filter((f) => f !== join(SRC, 'index.css') && !isFixture(f))
      .flatMap((f) => {
        // Комментарии ничего не красят, а ссылку `facebook/react#34905`
        // шаблон hex'а читает как цвет. Снимаются блочные и строчные,
        // причём строчные — только с начала строки, чтобы не резать
        // `https://` внутри кода. Размеченный блок образцов тем вырезается
        // первым — целиком, вместе со своими маркерами-комментариями, — до
        // общего снятия комментариев, а не вместо него.
        const content = readFileSync(f, 'utf8')
          .replace(ALLOWED_LITERAL_BLOCK, '')
          .replace(/\/\*[\s\S]*?\*\//g, '')
          .replace(/^[ \t]*\/\/.*$/gm, '');
        const hits = [...content.matchAll(FUNCTIONAL), ...content.matchAll(NAMED)]
          // Полупрозрачная чёрная тень цветом темы не задаётся: она
          // затемняет то, что под ней, а не красит себя.
          .filter((m) => !/rgba\(\s*0\s*,\s*0\s*,\s*0\s*,/.test(m[0]))
          .map((m) => m[0].trim());
        return hits.length ? [`${f}: ${hits.join(', ')}`] : [];
      });
    expect(offenders, offenders.join('\n')).toEqual([]);
  });

  // .scroll-dock-chapter и .scroll-dock-top приехали в разметку как «на что
  // потом повесить стили» и полгода не значили ничего. Пара «класс есть в
  // разметке, правила нет» — ровно тот вид полуудаления, из-за которого этот
  // файл и появился.
  it('объявляет в стилях каждый класс scroll-dock-* из разметки', () => {
    const tsx = readFileSync(join(SRC, 'components', 'ScrollDock.tsx'), 'utf8');
    const css = readFileSync(join(SRC, 'components', 'ScrollDock.css'), 'utf8');
    const used = new Set([...tsx.matchAll(/\bscroll-dock[\w-]*/g)].map((m) => m[0]));
    expect(used.size).toBeGreaterThan(0);
    const missing = [...used].filter((cls) => !css.includes(`.${cls}`)).sort();
    expect(missing, `классы без правил: ${missing.join(', ')}`).toEqual([]);
  });

  // Обрезка штабеля держится на паре «класс в разметке — правило в стиле», и
  // обе половины пишутся в разных файлах: опечатка в одном из них тиха.
  // Собрание при этом выглядит исправным — просто показывает все пятьдесят
  // томов, то есть ровно то, от чего обрезку и заводили.
  it('прячет обе обрезки штабеля правилами под классы из разметки', () => {
    const tsx = readFileSync(join(SRC, 'components', 'VolumeShelf.tsx'), 'utf8');
    const css = readFileSync(join(SRC, 'components', 'VolumeShelf.css'), 'utf8');
    const used = new Set(
      [...tsx.matchAll(/\bis-overflow[\w-]*|\bshelf--[\w-]+/g)].map((m) => m[0]),
    );
    expect(used).toEqual(new Set(['is-overflow', 'is-overflow-wide', 'shelf--compact']));
    const missing = [...used].filter((cls) => !css.includes(`.${cls}`)).sort();
    expect(missing, `классы без правил: ${missing.join(', ')}`).toEqual([]);
    // Широкая обрезка — не побочный вид узкой: её правило обязано жить вне
    // медиазапроса, иначе главная свернёт собрание только на телефоне.
    const narrow = css.slice(css.indexOf('@media'));
    expect(narrow).not.toContain('is-overflow-wide');
  });

  // Обе кнопки — плавающие цели для пальца: 0.6rem по вертикали давали около
  // 38px при рекомендованных 44. И подпись подглавы на узком экране не должна
  // исчезать вместе с подписью «Наверх»: стрелка ↑ против ⇈ — не подсказка о
  // месте, а ради подсказки блок и задуман.
  it('держит кнопки блока «вверх» пальцеразмерными и с подписью подглавы', () => {
    const css = readFileSync(join(SRC, 'components', 'ScrollDock.css'), 'utf8');
    const button = css.match(/\.scroll-dock-button\s*\{[^}]*\}/);
    expect(button, 'нет правила .scroll-dock-button').not.toBeNull();
    expect(button![0]).toMatch(/min-height:\s*44px/);

    const narrow = css.match(/@media\s*\(max-width:\s*640px\)\s*\{[\s\S]*\n\}/);
    expect(narrow, 'нет блока @media (max-width: 640px)').not.toBeNull();
    expect(narrow![0]).toMatch(/\.scroll-dock-top\s+\.scroll-dock-label\s*\{[^}]*display:\s*none/);
    expect(narrow![0]).toMatch(/\.scroll-dock-chapter\s+\.scroll-dock-label\s*\{[^}]*max-width/);
  });

  // Треугольник раскрытия был знаком в 10px без отступов — цель нажатия
  // около 10×10 при минимуме 24×24 (WCAG 2.5.8). Отрицательные поля по
  // вертикали обязательны: без них кнопка выше строки текста и растит
  // каждую из полутора сотен строк тома.
  it('держит треугольник оглавления нажимаемым, не растя строку', () => {
    const css = readFileSync(join(SRC, 'components', 'VolumeOutlineRow.css'), 'utf8');
    const button = css.match(/\.vol-toc-tri-btn\s*\{[^}]*\}/);
    expect(button, 'нет правила .vol-toc-tri-btn').not.toBeNull();
    expect(button![0]).toMatch(/width:\s*24px/);
    expect(button![0]).toMatch(/height:\s*24px/);
    expect(button![0]).toMatch(/margin:\s*-2px/);
  });

  // Крестик выноски был ~20×18px (padding + шрифт без явного размера) — тот
  // же порог 24×24 (WCAG 2.5.8), закреплённый в проекте двумя коммитами
  // раньше этой ветки (fe2f0071, треугольник оглавления выше).
  it('держит крестик выноски пальцеразмерным', () => {
    const css = readFileSync(join(SRC, 'components', 'FeatureHint.css'), 'utf8');
    const button = css.match(/\.feature-hint-close\s*\{[^}]*\}/);
    expect(button, 'нет правила .feature-hint-close').not.toBeNull();
    expect(button![0]).toMatch(/min-width:\s*24px/);
    expect(button![0]).toMatch(/min-height:\s*24px/);
  });

  // Плашка .status-badge (draft/completed/in-progress) охраняла контраст
  // индикатора work.status на старой странице тома. Задача 9 (сборка
  // /works/{id} из крышки/обреза/содержания) убрала эту плашку вместе со
  // старой разметкой WorkDetail: ни один компонент новой карточки
  // (VolumeMasthead/VolumeScale/VolumeOutline/VolumeManagePanel) work.status
  // не показывает — его место заняла постраничная статистика вычитки в
  // обрезе. grep "status-badge" по src/**/*.tsx подтверждает: класс больше
  // нигде не рендерится. См. также review commit 8295275, где эта же
  // плашка возвращалась, потому что тогда была ещё живой разметкой —
  // сейчас разметки, которую она красила бы, не существует.

  // /editions больше не отдельный экран со списком: собрания показывает
  // главная (VolumeShelf), а /editions без id уходит на неё через
  // перехват-всё. Экран-дубль удалён — страж не даёт ему тихо вернуться.
  it('экрана списка собраний больше нет', () => {
    expect(existsSync(join(SRC, 'pages/EditionList.tsx'))).toBe(false);
  });
});
