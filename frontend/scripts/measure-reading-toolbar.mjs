// Липкая панель экрана чтения на узком экране. jsdom раскладку не считает,
// поэтому меряем живым движком.
//
// Что сторожим: панель — один flex-ряд «цепочка слева, действия справа», и на
// телефоне ряд действий шире экрана. Цепочке разрешено сжиматься (min-width: 0),
// поэтому она ужимается до нулевой ширины, а её текст рисуется поверх соседа —
// первым в ряду действий стоит доля прочитанного, и «1 %» садится прямо на
// ссылку «← В. И. Ленин…». Второе следствие того же — последняя кнопка уезжает
// за правый край экрана. Обе проверки ниже.
//
// Запуск: `npm run measure:reading-toolbar`. Нужен поднятый фронт — 3100 занят
// собранным бандлом из docker, свежий код туда не попадает, поэтому свой:
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:reading-toolbar
// Имя бинарника Chrome — через CHROME (google-chrome-stable, chromium…).
//
// Замер идёт через CDP, а не через `--window-size`: ниже 500px эта сборка
// Chrome окно клэмпает и молча меряет не тот экран (см. measure-feature-hint.mjs).
// Ширина вьюпорта печатается фактическая, из самого замера, и расхождение с
// запрошенной — провал, а не примечание.
import { spawn } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3200';

// Том 6 Ленина, глава «Что делать?» — на ней снят отчёт о дефекте. Ширины:
// 360 — самый ходовой Android, 390 — iPhone, 1280 — контроль, что на десктопе
// панель осталась одной строкой.
// budget — потолок высоты липкой панели: она стоит над текстом всё чтение, и
// её высота это прямо отнятые у читателя строки. Числа с запасом к замеренным
// (135 на телефоне в полном виде, 62 на десктопе).
const CASES = [
  { url: `${BASE}/works/49/chapters/10125`, width: 360, budget: 145 },
  { url: `${BASE}/works/49/chapters/10125`, width: 390, budget: 145 },
  { url: `${BASE}/works/49/chapters/10125`, width: 1280, budget: 70, singleRow: true },
];

// Панель живёт в двух состояниях, и мерить надо оба. Наверху главы она
// полная, но без ссылки на правку — её ставит наблюдатель видимости полосы.
// После прокрутки ссылка появляется (ряд действий становится шире всего), зато
// сама панель сжимается (.is-condensed уменьшает кнопки). Что из двух хуже —
// не угадать, поэтому проверяются оба.
const SCROLL_TO = 1200;

const EXPR = `(() => {
  const rect = (sel) => {
    const n = document.querySelector(sel);
    if (!n) return null;
    const b = n.getBoundingClientRect();
    return { left: +b.left.toFixed(1), right: +b.right.toFixed(1), top: +b.top.toFixed(1), bottom: +b.bottom.toFixed(1) };
  };
  const crumbs = document.querySelector('.chapter-view-crumbs');
  if (!crumbs) return null;
  // Меряем не коробку цепочки, а её чернила: коробка сжата до нуля, а
  // наезжает на соседа именно текст, который из неё вылез.
  const walker = document.createTreeWalker(crumbs, NodeFilter.SHOW_TEXT);
  const boxes = [];
  for (let n = walker.nextNode(); n; n = walker.nextNode()) {
    const r = document.createRange();
    r.selectNodeContents(n);
    boxes.push(...Array.from(r.getClientRects()).filter((b) => b.width > 0));
  }
  const ink = boxes.length
    ? {
        left: +Math.min(...boxes.map((b) => b.left)).toFixed(1),
        right: +Math.max(...boxes.map((b) => b.right)).toFixed(1),
        top: +Math.min(...boxes.map((b) => b.top)).toFixed(1),
        bottom: +Math.max(...boxes.map((b) => b.bottom)).toFixed(1),
      }
    : null;
  return JSON.stringify({
    viewport: window.innerWidth,
    loaded: Boolean(document.querySelector('.chapter-page-section')),
    sections: document.querySelectorAll('.chapter-page-section').length,
    suggestLink: Boolean(document.querySelector('.reading-suggest-link')),
    condensed: document.querySelector('.chapter-view-header').classList.contains('is-condensed'),
    crumbInk: ink,
    actions: rect('.chapter-view-actions'),
    // Первый в ряду действий: номер видимой полосы (переход к странице), а
    // где перехода нет — доля прочитанного цифрой.
    percent: rect('.page-jump-trigger') ?? rect('.reading-percent'),
    header: rect('.chapter-view-header'),
  });
})()`;

/** Открывает Chrome, эмулирует вьюпорт заданной ширины, возвращает замеры двух состояний панели. */
async function measure(url, width) {
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
        pending.set(id, (msg) => (msg.error ? reject(new Error(`CDP ${method}: ${msg.error.message}`)) : resolve(msg.result)));
        ws.send(JSON.stringify({ id, method, params }));
      });

    await send('Page.enable');
    // mobile: false намеренно — с эмуляцией телефона Chrome применяет мета-
    // вьюпорт и на переполненной странице раздаёт больше пикселей, чем
    // запрошено (замер приезжал 407 вместо 360).
    await send('Emulation.setDeviceMetricsOverride', { width, height: 780, deviceScaleFactor: 1, mobile: false });
    await send('Page.navigate', { url });

    const snapshot = async () => {
      const res = await send('Runtime.evaluate', { expression: EXPR, returnByValue: true });
      const value = res.result?.value;
      return value ? JSON.parse(value) : null;
    };

    // Глава грузится целиком (сотни полос), поэтому срок щедрый. Мало дождаться
    // первой секции: пока React дорисовывает остальные, страница возвращается
    // наверх и прокрутку ниже сбрасывает. Ждём, пока число секций перестанет
    // расти.
    const deadline = Date.now() + 90000;
    let top = null;
    let stable = 0;
    let seen = -1;
    while (Date.now() < deadline) {
      await new Promise((r) => setTimeout(r, 700));
      top = await snapshot();
      if (!top?.loaded || !top.crumbInk || !top.actions) continue;
      stable = top.sections === seen ? stable + 1 : 0;
      seen = top.sections;
      if (stable >= 2) break;
    }
    if (!top?.loaded) {
      ws.close();
      return null;
    }

    // Ссылку ставит IntersectionObserver, класс .is-condensed — он же по
    // маячку; обоим нужен кадр после прокрутки. Ждать надо ОБА: глава
    // догружается на ходу, и прокрутка, отданная слишком рано, оставляла
    // панель в полном виде — замер тогда мерил верх главы под видом
    // прокрученного и скакал от прогона к прогону (135 против 105).
    let scrolled = null;
    const scrollDeadline = Date.now() + 25000;
    while (Date.now() < scrollDeadline) {
      await send('Runtime.evaluate', { expression: `window.scrollTo(0, ${SCROLL_TO})` });
      // Секунда на кадр и на оба наблюдателя; прокрутку повторяем редко —
      // частая перебивала собственную прокрутку страницы при догрузке.
      await new Promise((r) => setTimeout(r, 1500));
      scrolled = await snapshot();
      if (scrolled?.suggestLink && scrolled.condensed) break;
    }
    ws.close();
    return [
      { state: 'верх главы', ...top },
      { state: 'после прокрутки', ...scrolled },
    ];
  } finally {
    child.kill();
  }
}

let failed = false;
for (const { url, width, budget, singleRow } of CASES) {
  const states = await measure(url, width);
  if (!states) {
    console.error(`${url} @${width}px: глава не открылась — текста полос в DOM нет.`);
    failed = true;
    continue;
  }
  /** Высоты панели по состояниям — для сверки сжатой с полной. */
  const heights = {};
  for (const m of states) {
  if (!m || !m.crumbInk || !m.actions) {
    console.error(`${url} @${width}px, ${m?.state ?? 'состояние'}: панели на странице нет.`);
    failed = true;
    continue;
  }
  if (m.viewport !== width) {
    console.error(
      `${url}: запрошено ${width}px, фактический вьюпорт ${m.viewport}px — замер шёл не на том экране.`,
    );
    failed = true;
    continue;
  }
  if (m.state === 'после прокрутки' && !(m.suggestLink && m.condensed)) {
    console.error(
      `${url} @${width}px: прокрученное состояние не сложилось ` +
        `(ссылка на правку: ${m.suggestLink}, сжатие панели: ${m.condensed}) — мерить нечего.`,
    );
    failed = true;
    continue;
  }

  const problems = [];
  // Наезд — пересечение по ОБЕИМ осям: после починки цепочка и действия стоят
  // разными строками, и одного горизонтального пересечения там сколько угодно.
  const overlapX = Math.min(m.crumbInk.right, m.actions.right) - Math.max(m.crumbInk.left, m.actions.left);
  const overlapY = Math.min(m.crumbInk.bottom, m.actions.bottom) - Math.max(m.crumbInk.top, m.actions.top);
  if (overlapX > 0.5 && overlapY > 0.5) {
    problems.push(
      `текст цепочки (${m.crumbInk.left}..${m.crumbInk.right} x ${m.crumbInk.top}..${m.crumbInk.bottom}) ` +
        `наезжает на ряд действий (${m.actions.left}..${m.actions.right} x ${m.actions.top}..${m.actions.bottom}); ` +
        `номер страницы ${m.percent?.left}..${m.percent?.right}`,
    );
  }
  const height = m.header.bottom - m.header.top;
  if (height > budget) {
    problems.push(`липкая панель высотой ${height.toFixed(1)}px при потолке ${budget}px`);
  }
  if (m.actions.right > width + 0.5) {
    problems.push(`правый край ряда действий x=${m.actions.right} за экраном шириной ${width}`);
  }
  // На десктопе панель обязана остаться одной строкой: перенос там означал бы,
  // что мобильное правило протекло на широкий экран.
  if (singleRow && m.crumbInk.bottom > m.actions.bottom + 0.5) {
    problems.push(
      `панель разъехалась на две строки: цепочка кончается на y=${m.crumbInk.bottom}, ` +
        `ряд действий — на y=${m.actions.bottom}`,
    );
  }

  // Сжатая панель обязана быть НЕ выше полной. Условие не формальное: первая
  // же починка наезда дала ровно обратное — прокрутка добавляла кнопку
  // «Исправить», та уносила третий ряд, и «сжатая» панель выходила на 41px
  // выше полной.
  if (m.state === 'после прокрутки' && height > heights['верх главы'] + 0.5) {
    problems.push(
      `сжатая панель (${height.toFixed(1)}px) выше полной (${heights['верх главы'].toFixed(1)}px)`,
    );
  }
  heights[m.state] = height;

  if (problems.length) {
    failed = true;
    console.error(`FAIL ${url} @${width}px, ${m.state}:\n  - ${problems.join('\n  - ')}`);
  } else {
    console.log(
      `ok ${url} @${width}px, ${m.state}: цепочка до x=${m.crumbInk.right}, ` +
        `действия ${m.actions.left}..${m.actions.right}, высота панели ${height.toFixed(1)}px`,
    );
  }
  }
}

process.exit(failed ? 1 : 0);
