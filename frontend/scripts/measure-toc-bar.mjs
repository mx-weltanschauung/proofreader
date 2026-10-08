// Высота липкой полосы содержания на живом томе. jsdom раскладку не считает,
// поэтому меряем настоящим движком. Дамп не обрезать: правые края и счётчик
// живут в дальнем конце строки.
// Запуск: `npm run measure:toc-bar`. Нужен поднятый фронт — 3100 обычно занят
// рабочим, поэтому свой:
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:toc-bar
import { execFileSync } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3100';

// Том 15 МиЭ (work 46) — худший случай: три фасета, широкий корень.
const CASES = [
  { url: `${BASE}/works/46`, width: 1280, budget: 44 },
  { url: `${BASE}/works/46`, width: 375, budget: 88 },
];

let failed = false;
for (const { url, width, budget } of CASES) {
  const out = execFileSync(
    CHROME,
    [
      '--headless=new',
      '--disable-gpu',
      // Виртуальное время нужно, чтобы страница успела загрузить шрифты:
      // Literata меняет высоту строки, и замер до её загрузки врёт в меньшую
      // сторону. Атрибут ставится только после document.fonts.ready.
      '--virtual-time-budget=15000',
      `--window-size=${width},900`,
      '--dump-dom',
      url,
    ],
    { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 },
  );
  // --dump-dom не отдаёт геометрию, поэтому высоту считает сама страница и
  // кладёт её в атрибут: скрипт ниже выполняется в контексте страницы.
  const match = out.match(/data-toc-bar-height="(\d+(?:\.\d+)?)"/);
  if (!match) {
    console.error(`${url} @${width}px: полосы в дампе нет — проверь, что том открывается`);
    failed = true;
    continue;
  }
  const height = Number(match[1]);
  const verdict = height <= budget ? 'OK' : 'ПРЕВЫШЕНО';
  console.log(`${url} @${width}px: ${height}px при бюджете ${budget}px — ${verdict}`);
  if (height > budget) failed = true;
}

if (failed) {
  console.error(
    '\nСлучай 375px красный НАМЕРЕННО: бюджет 88px записан в спеке\n' +
      '(docs/superpowers/specs/2026-08-22-volume-outline-depth-design.md,\n' +
      'раздел «Замер вёрстки»), а решение по находке 1 было править только\n' +
      '1280px. Красный замер здесь означает записанное и невыполненное\n' +
      'требование, а не поломку. Случай 1280px обязан быть зелёным —\n' +
      'если красный он, это регресс.',
  );
}

process.exit(failed ? 1 : 0);
