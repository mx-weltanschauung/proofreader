import { describe, it, expect } from 'vitest';
import { readdirSync, readFileSync, statSync } from 'node:fs';
import { join, relative } from 'node:path';

const SRC = join(__dirname, '..');
const INDEX_CSS = join(SRC, 'index.css');

type Tokens = Record<string, string>;

/**
 * Разбирает блоки [data-theme='…'] в карту «тема -> её токены».
 * Регулярка, а не настоящий парсер CSS: блоки тем плоские, вложенности в них
 * нет, а тянуть зависимость ради шести блоков не за чем.
 */
export function parseThemes(css: string): Record<string, Tokens> {
  const themes: Record<string, Tokens> = {};
  for (const block of css.matchAll(/\[data-theme='([\w-]+)'\]\s*\{([^}]*)\}/g)) {
    const decls: Tokens = themes[block[1]] ?? {};
    for (const decl of block[2].matchAll(/(--[\w-]+)\s*:\s*([^;]+);/g)) {
      decls[decl[1]] = decl[2].trim();
    }
    themes[block[1]] = decls;
  }
  return themes;
}

function channel(value: number): number {
  const c = value / 255;
  return c <= 0.04045 ? c / 12.92 : ((c + 0.055) / 1.055) ** 2.4;
}

/** Непрозрачный hex — единственная форма, которую умеет считать luminance(). */
const HEX = /^#[0-9a-f]{3,8}$/i;

function luminance(hex: string): number {
  let h = hex.trim().replace('#', '');
  if (h.length === 3) h = [...h].map((ch) => ch + ch).join('');
  const [r, g, b] = [0, 2, 4].map((i) => parseInt(h.slice(i, i + 2), 16));
  return 0.2126 * channel(r) + 0.7152 * channel(g) + 0.0722 * channel(b);
}

/**
 * Контраст по WCAG 2.1. Оба цвета — непрозрачные hex.
 *
 * Проверка формы обязательна и обязана бросать. luminance() читает hex, и на
 * `rgb(45, 45, 45)` parseInt даёт NaN, контраст — NaN, а `NaN < 4.5` это
 * `false`: пара молча перестаёт быть нарушением. Тест, который так «проходит»,
 * хуже отсутствующего — он утверждает, что цвета проверены.
 */
export function contrast(a: string, b: string): number {
  for (const value of [a, b]) {
    if (typeof value !== 'string' || !HEX.test(value.trim())) {
      throw new Error(
        `контраст считается только по непрозрачному hex, получено: ${String(value)}. ` +
          'rgba()-токены (--color-shadow, --color-scrim) в пары контраста попадать не должны.',
      );
    }
  }
  const [hi, lo] = [luminance(a), luminance(b)].sort((x, y) => y - x);
  return (hi + 0.05) / (lo + 0.05);
}

/**
 * Разворачивает один уровень косвенности: `--color-button-primary: var(--color-info)`.
 * Глубже не ходим — цепочек длиннее одной в index.css нет и заводить их незачем.
 */
function resolve(tokens: Tokens, name: string): string | undefined {
  const raw = tokens[name];
  if (raw === undefined) return undefined;
  const alias = raw.match(/^var\(\s*(--[\w-]+)\s*\)$/);
  return alias ? tokens[alias[1]] : raw;
}

const THEMES = ['light', 'dark', 'sepia', 'oled', 'high-contrast', 'gray-dim'];

const ROLE_TOKENS = [
  '--color-accent',
  '--color-accent-on',
  '--color-accent-hover',
  '--color-info-hover',
  '--color-error-hover',
  '--color-focus',
  '--color-border-strong',
  // Затемнение под выезжающей панелью. Тема, забывшая токен, рисует его
  // прозрачным: панель едет по нетронутой странице и теряет край.
  '--color-scrim',
  ...['success', 'warning', 'error', 'info'].flatMap((x) => [
    `--color-${x}`,
    `--color-${x}-on`,
    `--color-${x}-tint`,
    `--color-${x}-ink`,
  ]),
];

/** [фон, текст, требуемый контраст] — пары, которые реально встречаются в вёрстке. */
const PAIRS: [string, string, number][] = [
  ['--color-bg-primary', '--color-text-primary', 4.5],
  ['--color-card-bg', '--color-text-secondary', 4.5],
  ['--color-card-bg', '--color-text-tertiary', 4.5],
  ['--color-card-bg', '--color-link', 4.5],
  ['--color-card-bg', '--color-link-visited', 4.5],
  ['--color-bg-primary', '--color-link', 4.5],
  // Третичная поверхность — плашки статусов, панель оглавления, «пустые»
  // состояния. Текст на ней рисуется обоими приглушёнными тонами.
  ['--color-bg-tertiary', '--color-text-secondary', 4.5],
  ['--color-bg-tertiary', '--color-text-tertiary', 4.5],
  // Карточка читательского билета стоит на третичной поверхности, а
  // заголовок и предупреждение внутри неё пишутся основным тоном: правило
  // с фоном и правило с цветом разные, и авто-проверка ниже такую пару не
  // видит — фон и текст в ней задаются в разных селекторах.
  ['--color-bg-tertiary', '--color-text-primary', 4.5],
  // Формы и списки стоят на bg-secondary, а не на card-bg, и несут на себе
  // те же чернила плашек.
  ['--color-bg-secondary', '--color-info-ink', 4.5],
  ['--color-bg-secondary', '--color-warning-ink', 4.5],
  ['--color-bg-secondary', '--color-error-ink', 4.5],
  ['--color-accent', '--color-accent-on', 4.5],
  ['--color-success', '--color-success-on', 4.5],
  ['--color-warning', '--color-warning-on', 4.5],
  ['--color-error', '--color-error-on', 4.5],
  ['--color-info', '--color-info-on', 4.5],
  // Наведение — та же заливка, только сдвинутая; текст на ней тот же -on.
  ['--color-accent-hover', '--color-accent-on', 4.5],
  ['--color-info-hover', '--color-info-on', 4.5],
  ['--color-error-hover', '--color-error-on', 4.5],
  ['--color-success-tint', '--color-success-ink', 4.5],
  ['--color-warning-tint', '--color-warning-ink', 4.5],
  ['--color-error-tint', '--color-error-ink', 4.5],
  ['--color-info-tint', '--color-info-ink', 4.5],
  ['--color-card-bg', '--color-success-ink', 4.5],
  ['--color-card-bg', '--color-error-ink', 4.5],
  ['--color-card-bg', '--color-warning-ink', 4.5],
  ['--color-bg-primary', '--color-focus', 3],
  ['--color-card-bg', '--color-focus', 3],
  ['--color-bg-primary', '--color-border-strong', 3],
  ['--color-card-bg', '--color-border-strong', 3],
  ['--color-bg-primary', '--color-accent', 3],
  // Подвал: до трёх колонок его пара не проверялась вовсе, а текста в нём
  // стало вчетверо больше. Замер на всех шести темах: минимум 5.42.
  ['--color-footer-bg', '--color-footer-text', 4.5],
  ['--color-footer-bg', '--color-link', 4.5],
  ['--color-footer-bg', '--color-link-hover', 4.5],
  // Знак «18+» в нижней строке нарисован одной рамкой без заливки: значимый
  // графический объект, порог WCAG 1.4.11 — 3:1. Замер по шести темам:
  // минимум 3.17 (тёмная).
  ['--color-footer-bg', '--color-border-strong', 3],
];

function cssFiles(dir: string): string[] {
  return readdirSync(dir)
    .flatMap((entry) => {
      const full = join(dir, entry);
      if (statSync(full).isDirectory()) return cssFiles(full);
      return full.endsWith('.css') ? [full] : [];
    })
    .sort();
}

interface Rule {
  file: string;
  selector: string;
  bg: string;
  fg: string;
}

const SINGLE_VAR = /^var\(\s*(--color-[\w-]+)\s*\)$/;
const HEX_LITERAL = /^#[0-9a-f]{3,8}$/i;

/**
 * Правила, которые сами задают и заливку, и цвет текста. Плоская регулярка:
 * @media-обёртка ей не по зубам, но её внутренние правила ловятся как
 * самостоятельные — ровно то, что нужно.
 *
 * `color` принимается и как токен, и как непрозрачный hex-литерал: именно
 * так `.rs-choice.is-active { background-color: var(--color-button-primary);
 * color: #fff; }` раньше проходило мимо этого теста — фон был токеном, а
 * `#fff` не совпадал с SINGLE_VAR, и правило не регистрировалось парой
 * вовсе. `background-color` литералом не принимается: если фон не токен,
 * сверять его не с чем (и в проекте это встречается только в образцах тем,
 * которые размечены и вычищаются до вызова этой функции).
 */
export function pairedRules(css: string, file: string): Rule[] {
  const rules: Rule[] = [];
  for (const block of css.replace(/\/\*[\s\S]*?\*\//g, '').matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
    const decls: Record<string, string> = {};
    for (const decl of block[2].split(';')) {
      const idx = decl.indexOf(':');
      if (idx < 0) continue;
      const prop = decl.slice(0, idx).trim().toLowerCase();
      if (prop.startsWith('--')) continue;
      decls[prop] = decl.slice(idx + 1).trim();
    }
    const bg = (decls['background-color'] ?? decls['background'] ?? '').match(SINGLE_VAR);
    const fgRaw = (decls['color'] ?? '').trim();
    const fgVar = fgRaw.match(SINGLE_VAR);
    const fg = fgVar ? fgVar[1] : HEX_LITERAL.test(fgRaw) ? fgRaw : undefined;
    if (bg && fg) {
      rules.push({ file, selector: block[1].trim().replace(/\s+/g, ' '), bg: bg[1], fg });
    }
  }
  return rules;
}

describe('контрасты цветовых токенов', () => {
  const themes = parseThemes(readFileSync(INDEX_CSS, 'utf8'));

  it('разбирает все шесть тем', () => {
    expect(Object.keys(themes).sort()).toEqual([...THEMES].sort());
  });

  it.each(THEMES)('тема %s объявляет все ролевые токены', (name) => {
    const missing = ROLE_TOKENS.filter((t) => !themes[name]?.[t]);
    expect(missing, `не объявлены: ${missing.join(', ')}`).toEqual([]);
  });

  it.each(THEMES)('тема %s выдерживает пороги контраста', (name) => {
    const tokens = themes[name];
    const failures = PAIRS.flatMap(([bg, fg, need]) => {
      const bgValue = resolve(tokens, bg);
      const fgValue = resolve(tokens, fg);
      try {
        const ratio = contrast(bgValue!, fgValue!);
        return ratio < need
          ? [`${fg} (${fgValue}) на ${bg} (${bgValue}): ${ratio.toFixed(2)}:1, нужно ${need}`]
          : [];
      } catch (err) {
        return [`${fg} на ${bg}: ${(err as Error).message}`];
      }
    });
    expect(failures, failures.join('\n')).toEqual([]);
  });

  /*
   * Чернила узора машинной вычитки — --color-success, а промежутки узора
   * открывают дорожку --color-bg-tertiary. Значимый графический объект
   * обязан отстоять от соседнего на 3:1 (WCAG 1.4.11), и именно этого не
   * давал прежний --color-success-tint: 1.03–1.24:1 во всех шести темах.
   */
  it.each(THEMES)('тема %s: штрих машинной вычитки виден на пустой дорожке', (name) => {
    const tokens = themes[name];
    const ink = resolve(tokens, '--color-success')!;
    const track = resolve(tokens, '--color-bg-tertiary')!;
    const ratio = contrast(ink, track);
    expect(ratio, `${ink} на ${track}: ${ratio.toFixed(2)}:1, нужно 3`).toBeGreaterThanOrEqual(3);
  });

  /*
   * Неписаный инвариант: около десятка правил кладут `color: var(--color-info-on)`
   * на `background-color: var(--color-button-primary)`. Это верно ровно потому,
   * что оба токена во всех шести темах держат одно значение. Выбран не тест на
   * равенство, а псевдоним: `--color-button-primary: var(--color-info)` делает
   * расхождение невозможным, а не всего лишь замеченным — новая тема физически
   * не может объявить два разных синих. Тест сторожит сам псевдоним.
   */
  it.each(THEMES)('тема %s определяет button-primary через info', (name) => {
    expect(themes[name]['--color-button-primary']).toBe('var(--color-info)');
    expect(themes[name]['--color-button-primary-hover']).toBe('var(--color-info-hover)');
  });

  /*
   * Пары по списку проверяют токены, а вёрстку — нет. Плашка ошибки семь раз
   * подряд клала `color: var(--color-error)` на `--color-error-tint` (3.90:1 в
   * светлой теме), и ни одна пара из PAIRS этого не видела: по отдельности оба
   * токена «правильные». Здесь читается само правило — чем покрашен фон и чем
   * текст на нём — и сверяется во всех шести темах.
   */
  it('каждое правило с фоном и текстом выдерживает 4.5:1 во всех темах', () => {
    const rules = cssFiles(SRC).flatMap((file) =>
      pairedRules(readFileSync(file, 'utf8'), relative(SRC, file)),
    );
    expect(rules.length, 'ни одного правила с парой фон+текст не найдено').toBeGreaterThan(20);

    const failures: string[] = [];
    for (const rule of rules) {
      for (const theme of THEMES) {
        const bgValue = resolve(themes[theme], rule.bg);
        // rule.fg is either a --color-* token name to resolve per theme, or
        // an already-literal hex value (see pairedRules) that is the same
        // in every theme by construction.
        const fgValue = rule.fg.startsWith('--') ? resolve(themes[theme], rule.fg) : rule.fg;
        try {
          const ratio = contrast(bgValue!, fgValue!);
          if (ratio < 4.5) {
            failures.push(
              `${rule.file} ${rule.selector} [${theme}]: ${rule.fg} (${fgValue}) на ` +
                `${rule.bg} (${bgValue}) — ${ratio.toFixed(2)}:1`,
            );
          }
        } catch (err) {
          failures.push(`${rule.file} ${rule.selector} [${theme}]: ${(err as Error).message}`);
        }
      }
    }
    expect(failures, failures.join('\n')).toEqual([]);
  });

  /*
   * Правило выше видит пару, только если оба свойства заданы в ОДНОМ
   * правиле. `color: var(--color-error)` без background-color рядом — текст
   * ложится на фон, который задаёт какой-то предок, и здесь этого фона нет:
   * без настоящего каскада взять его неоткуда, а подделывать каскад или
   * гадать про предка — значит утверждать проверку, которой на самом деле
   * нет. Восемь мест ровно так и остались непойманы: `.error-state { color:
   * var(--color-error); }` в разных файлах, без единой заливки в правиле.
   *
   * Настоящую проверку «текст на фоне предка» тест дать не может. Но у неё
   * есть более узкий, зато безусловно верный инвариант: --color-error,
   * --color-success, --color-warning, --color-info — роли ЗАЛИВКИ (fill).
   * Они рассчитаны на то, чтобы под ними лежал --color-X-on (см. пары выше).
   * Класть их в `color:` — то есть использовать заливку как текст — само по
   * себе и есть нарушение, независимо от того, что за поверхность вокруг:
   * читаемый текст на статусе всегда --color-X-ink (приглушённая поверхность)
   * или --color-X-on (заливка), никогда не сырой X. Проверка не знает, что
   * находится под правилом, — и не нужно знать, чтобы это утверждать.
   */
  it('не красит текст сырой заливкой success/warning/error/info — только -ink или -on', () => {
    const FILL_ROLES = new Set([
      '--color-success',
      '--color-warning',
      '--color-error',
      '--color-info',
    ]);
    const offenders: string[] = [];
    for (const file of cssFiles(SRC)) {
      if (file === INDEX_CSS) continue; // index.css сами роли и объявляет
      const css = readFileSync(file, 'utf8').replace(/\/\*[\s\S]*?\*\//g, '');
      for (const block of css.matchAll(/([^{}]+)\{([^{}]*)\}/g)) {
        for (const decl of block[2].split(';')) {
          const idx = decl.indexOf(':');
          if (idx < 0) continue;
          const prop = decl.slice(0, idx).trim().toLowerCase();
          if (prop !== 'color') continue; // не background-color, не border-color
          const value = decl.slice(idx + 1).trim();
          const m = value.match(SINGLE_VAR);
          if (m && FILL_ROLES.has(m[1])) {
            offenders.push(
              `${relative(SRC, file)} ${block[1].trim().replace(/\s+/g, ' ')}: color: ${value}`,
            );
          }
        }
      }
    }
    expect(offenders, offenders.join('\n')).toEqual([]);
  });
});
