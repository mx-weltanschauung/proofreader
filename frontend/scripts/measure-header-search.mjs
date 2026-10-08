// Строка поиска в шапке. jsdom раскладку не считает, поэтому меряем живым
// движком — тот же CDP-приём, что и measure-feature-hint.mjs/
// measure-reading-toolbar.mjs (--window-size в этой сборке Chrome клэмпает
// ниже 500px, поэтому все ширины идут через Emulation.setDeviceMetricsOverride,
// не только узкие — так один код путь считает и то, и другое, и расхождение
// «запрошено/фактически» видно в каждом случае, а не только в узких).
//
// Что стережём: требование задачи 8 — строка поиска уходит на свою строку
// ниже брейкпоинта .header-content (768px) и никогда не прячется за иконкой;
// и что она не устраивает горизонтальное переполнение страницы ни на одной
// проверенной ширине. Полоса 800–1200px — не для галочки: сама шапка выросла
// примерно на 220px (ширина .header-search), а .header-content не начинает
// переноситься раньше 768px, так что именно в этой полосе новое переполнение
// возникло бы впервые и осталось бы незамеченным, если мерить только классические
// мобильные ширины.
//
// Запуск: `npm run measure:header-search`. Нужен поднятый фронт — 3100 обычно
// занят рабочим, поэтому свой:
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:header-search
import { spawn } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3200';

// Шире брейкпоинта — строка поиска обязана остаться в одном ряду с логотипом
// и правой частью шапки. 1024/900 закрывают ровно ту полосу между
// брейкпоинтом и десктопом, где выросшая шапка могла впервые не влезть.
const ROW_WIDTHS = [1280, 1024, 900];
// На и ниже брейкпоинта (.header-content: max-width: 768px) — строка поиска
// обязана уйти на свою строку.
// 320px раньше был вынесен в отдельный «известный» случай: .header-left
// (логотип + меню) сам по себе не помещался на такой ширине ещё до всякой
// строки поиска. Теперь меню переносится по строкам (.header-nav:
// flex-wrap), header-left помещается, и 320px проверяется наравне со всеми —
// см. scripts/measure-header-nav.mjs, где та же теснота стережётся с полным
// набором ссылок вошедшего.
const WRAP_WIDTHS = [768, 600, 500, 400, 320];

const EXPR = `JSON.stringify({
  viewport: window.innerWidth,
  scrollWidth: document.documentElement.scrollWidth,
  headerSearch: document.querySelector('.header-search')?.getBoundingClientRect() ?? null,
  headerLeft: document.querySelector('.header-left')?.getBoundingClientRect() ?? null,
  headerRight: document.querySelector('.header-right')?.getBoundingClientRect() ?? null,
})`;

/** Открывает headless Chrome, эмулирует вьюпорт заданной ширины через CDP, возвращает JSON из EXPR. */
async function measure(width) {
  const child = spawn(CHROME, [
    '--headless=new',
    '--disable-gpu',
    '--remote-debugging-port=0',
    '--window-size=1280,900',
    'about:blank',
  ]);
  try {
    const port = await new Promise((resolve, reject) => {
      let buf = '';
      const onData = (chunk) => {
        buf += chunk.toString();
        const m = buf.match(/DevTools listening on ws:\/\/[^:]+:(\d+)\//);
        if (m) {
          child.stderr.off('data', onData);
          resolve(Number(m[1]));
        }
      };
      child.stderr.on('data', onData);
      child.once('error', reject);
      child.once('exit', (code) => reject(new Error(`chrome вышел раньше времени (код ${code})`)));
      setTimeout(() => reject(new Error('chrome не открыл порт отладки за 5с')), 5000);
    });

    const targets = await (await fetch(`http://127.0.0.1:${port}/json`)).json();
    const target = targets.find((t) => t.type === 'page') ?? targets[0];
    if (!target) throw new Error('CDP: страница не найдена в списке целей');

    const ws = new WebSocket(target.webSocketDebuggerUrl);
    let nextId = 1;
    const pending = new Map();
    ws.onmessage = (ev) => {
      const msg = JSON.parse(ev.data);
      if (msg.id && pending.has(msg.id)) {
        pending.get(msg.id)(msg);
        pending.delete(msg.id);
      }
    };
    await new Promise((resolve, reject) => {
      ws.onopen = resolve;
      ws.onerror = () => reject(new Error('CDP: не удалось открыть WebSocket'));
    });
    const send = (method, params = {}) =>
      new Promise((resolve, reject) => {
        const id = nextId++;
        pending.set(id, (msg) => {
          if (msg.error) reject(new Error(`CDP ${method}: ${msg.error.message}`));
          else resolve(msg.result);
        });
        ws.send(JSON.stringify({ id, method, params }));
      });

    await send('Page.enable');
    // mobile: false намеренно (см. measure-reading-toolbar.mjs): с эмуляцией
    // телефона Chrome применяет мета-вьюпорт и раздаёт больше пикселей, чем
    // запрошено.
    await send('Emulation.setDeviceMetricsOverride', {
      width,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    });
    await send('Page.navigate', { url: `${BASE}/` });
    // Шрифты + первичный рендер шапки; шапка не грузит данные асинхронно
    // помимо самой страницы, поэтому запас скромный.
    await new Promise((r) => setTimeout(r, 2000));

    const res = await send('Runtime.evaluate', { expression: EXPR, returnByValue: true });
    ws.close();
    const value = res.result?.value;
    return value ? JSON.parse(value) : null;
  } finally {
    child.kill();
  }
}

/** Пересекаются ли по вертикали два прямоугольника — то есть на одной ли они строке. */
function verticallyOverlaps(a, b) {
  return Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top) > 0.5;
}

function checkCommon(width, m) {
  const problems = [];
  if (!m) {
    problems.push('шапка не отрисовалась — .header-search/.header-left/.header-right нет в DOM');
    return problems;
  }
  if (m.viewport !== width) {
    problems.push(
      `запрошено ${width}px, фактический вьюпорт ${m.viewport}px — замер шёл не на том экране`,
    );
  }
  if (!m.headerSearch || !m.headerLeft || !m.headerRight) {
    problems.push('одного из трёх узлов шапки нет в DOM');
  }
  return problems;
}

let failed = false;

for (const width of ROW_WIDTHS) {
  const m = await measure(width);
  const problems = checkCommon(width, m);
  if (problems.length === 0) {
    if (m.scrollWidth > m.viewport + 0.5) {
      problems.push(
        `страница переполнена по горизонтали: scrollWidth=${m.scrollWidth} при viewport=${m.viewport}`,
      );
    }
    if (!verticallyOverlaps(m.headerSearch, m.headerLeft)) {
      problems.push(
        `строка поиска ушла на отдельную строку на десктопной ширине ` +
          `(header-search y=${m.headerSearch.top}..${m.headerSearch.bottom}, ` +
          `header-left y=${m.headerLeft.top}..${m.headerLeft.bottom})`,
      );
    }
  }
  if (problems.length) {
    failed = true;
    console.error(`FAIL ${BASE}/ @${width}px (один ряд): ${problems.join('; ')}`);
  } else {
    console.log(
      `ok ${BASE}/ @${width}px: строка поиска в одном ряду с логотипом (search ` +
        `${m.headerSearch.left.toFixed(0)}..${m.headerSearch.right.toFixed(0)}), переполнения нет`,
    );
  }
}

for (const width of WRAP_WIDTHS) {
  const m = await measure(width);
  const problems = checkCommon(width, m);
  if (problems.length === 0) {
    if (m.scrollWidth > m.viewport + 0.5) {
      problems.push(
        `страница переполнена по горизонтали: scrollWidth=${m.scrollWidth} при viewport=${m.viewport}`,
      );
    }
    if (verticallyOverlaps(m.headerSearch, m.headerLeft)) {
      problems.push(
        `строка поиска осталась в первом ряду вместо своей строки ` +
          `(header-search y=${m.headerSearch.top}..${m.headerSearch.bottom}, ` +
          `header-left y=${m.headerLeft.top}..${m.headerLeft.bottom})`,
      );
    }
  }
  if (problems.length) {
    failed = true;
    console.error(`FAIL ${BASE}/ @${width}px (своя строка): ${problems.join('; ')}`);
  } else {
    console.log(
      `ok ${BASE}/ @${width}px: строка поиска на своей строке (y=` +
        `${m.headerSearch.top.toFixed(0)}..${m.headerSearch.bottom.toFixed(0)}), переполнения нет`,
    );
  }
}

process.exit(failed ? 1 : 0);
