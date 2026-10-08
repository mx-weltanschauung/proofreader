// Пузырёк выноски не должен вылезать за экран. jsdom раскладку не считает,
// поэтому меряем настоящим движком — тем же приёмом, что measure-toc-bar.mjs.
// Дамп не обрезать: атрибут живёт в дальнем конце строки.
// Запуск: `npm run measure:feature-hint`. Нужен поднятый фронт — 3100 обычно
// занят рабочим, поэтому свой:
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:feature-hint
// Имя бинарника Chrome настраивается через CHROME (по умолчанию google-chrome) —
// на разных системах он называется google-chrome-stable или chromium.
//
// Два способа измерения, не один — и это не произвол:
// `--window-size` у headless Chrome этой сборки (проверено скэном 200..1280px)
// НЕ опускает окно ниже 500px — запросишь 360, получишь фактические 500.
// Для широких случаев (>=500px) это не мешает: --window-size укладывается в
// диапазон, где клэмпа нет, и обычный `--dump-dom` с виртуальным временем
// работает и достаточно дёшев. Для узких (<500px) он бы молча подменил
// экран — то есть свёл замер узкого пузырька ровно к той дыре, ради которой
// эта задача и заводилась (jsdom/клэмп раскладку не считают, а вердикт
// печатается зелёным). Поэтому узкие случаи идут через CDP:
// `Emulation.setDeviceMetricsOverride` эмулирует viewport независимо от
// размера самого окна — window.innerWidth и раскладка внутри страницы
// реально становятся заданными, что проверяется тут же, самим замером
// (см. requestedVsActual ниже).
import { execFileSync, spawn } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3100';
/** Минимальный отступ пузырька от края окна — VIEWPORT_MARGIN из popoverPlacement.ts. */
const MARGIN = 8;
/** Ниже этой ширины --window-size в этой сборке Chrome клэмпается — см. комментарий выше. */
const CDP_BELOW_WIDTH = 500;

// Экран чтения тома 46 и его карточка: две разные точки привязки — кнопка у
// правого края панели и полоса обреза во всю ширину. На свежем профиле (без
// localStorage) координатор выбирает подсказку с наибольшим приоритетом среди
// видимых сразу — на экране чтения это «№» (page-numbers, 100 против 90 у
// reading-settings в той же панели), на карточке тома — глубина содержания
// (outline-depth, 80: выше и volume-scale, и front-matter). expectedId здесь
// не догадка, а утверждение: document.querySelector по одному [data-hint-box]
// уже один раз молча подобрал не тот пузырёк (см. FeatureHint.tsx,
// data-hint-id) — без явной проверки, чей это пузырёк, эта ошибка
// воспроизводима.
const CASES = [
  { url: `${BASE}/works/46/read/1`, width: 1280, expectedId: 'page-numbers' },
  { url: `${BASE}/works/46/read/1`, width: 360, expectedId: 'page-numbers' },
  { url: `${BASE}/works/46`, width: 1280, expectedId: 'outline-depth' },
  { url: `${BASE}/works/46`, width: 360, expectedId: 'outline-depth' },
];

const TAG_RE = /<div class="feature-hint"[^>]*>/g;

/** Разбирает один открывающий тег пузырька: {id, box} по его атрибутам, независимо от их порядка. */
function parseHintTag(tag) {
  return {
    id: tag.match(/data-hint-id="([^"]*)"/)?.[1] ?? null,
    box: tag.match(/data-hint-box="(-?\d+,-?\d+,\d+,\d+,\d+)"/)?.[1] ?? null,
  };
}

/** Широкие случаи: обычный --dump-dom, без CDP. Возвращает {id, box} нужного пузырька, все найденные id и null, если пузырька нет вовсе. */
function measureViaDumpDom(url, width) {
  const out = execFileSync(
    CHROME,
    [
      '--headless=new',
      '--disable-gpu',
      // Виртуальное время нужно и для шрифтов, и для паузы перед появлением
      // выноски (HINT_APPEAR_MS = 1200) — но меньше её жизни (8000),
      // иначе она успеет погаснуть до дампа.
      '--virtual-time-budget=6000',
      `--window-size=${width},900`,
      '--dump-dom',
      url,
    ],
    { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 },
  );
  const tags = (out.match(TAG_RE) ?? []).map(parseHintTag);
  if (tags.length === 0) return null;
  return tags;
}

/**
 * Узкие случаи: реальный Chrome-процесс живёт, пока идёт CDP-сессия — окно
 * никогда не создаётся такой ширины (клэмп), зато viewport внутри страницы
 * эмулируется напрямую. Порт берётся автоматически (`--remote-debugging-port=0`
 * плюс разбор строки Chrome в stderr) — фиксированный порт в этом окружении
 * иногда занят посторонним слушателем, который отвечает похожим на DevTools
 * JSON'ом, но не им.
 */
async function measureViaCdp(url, width) {
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
    // Viewport эмулируется независимо от фактического размера окна — тот
    // самый обход клэмпа --window-size, ради которого этот путь и существует.
    await send('Emulation.setDeviceMetricsOverride', {
      width,
      height: 900,
      deviceScaleFactor: 1,
      mobile: false,
    });
    await send('Page.navigate', { url });

    // Тот же бюджет, что у dump-dom (6000мс): HINT_APPEAR_MS=1200 на появление
    // плюс шрифты, с запасом меньше HINT_HIDE_MS=8000, чтобы не поймать угасшую
    // выноску. Здесь это реальное время (poll), а не виртуальное.
    const deadline = Date.now() + 6000;
    let tags = null;
    while (Date.now() < deadline) {
      const evalResult = await send('Runtime.evaluate', {
        // Все пузырьки на странице разом, парами {id, box} — не только тот,
        // что ожидался: если координатор выбрал не того, скрипт обязан
        // назвать, кого именно увидел, а не молча прождать таймаут.
        expression:
          "JSON.stringify(Array.from(document.querySelectorAll('.feature-hint')).map((n) => " +
          "({ id: n.getAttribute('data-hint-id'), box: n.getAttribute('data-hint-box') })))",
        returnByValue: true,
      });
      const found = JSON.parse(evalResult.result.value ?? '[]');
      if (found.length > 0) {
        tags = found;
        break;
      }
      await new Promise((r) => setTimeout(r, 300));
    }
    ws.close();
    return tags;
  } finally {
    child.kill();
  }
}

/** Разбирает найденные пузырьки, сверяет id, ширину окна и границы, печатает вердикт. Возвращает true, если случай прошёл. */
function evaluateCase(url, width, expectedId, tags) {
  if (!tags) {
    console.error(
      `${url} @${width}px: выноски в дампе нет. Профиль Chrome одноразовый, ` +
        'значит localStorage пуст и подсказка обязана была появиться — ' +
        'проверь, что том открывается и что якорь виден.',
    );
    return false;
  }

  const match = tags.find((t) => t.id === expectedId);
  if (!match) {
    const seen = tags.map((t) => t.id ?? '(без data-hint-id)').join(', ');
    console.error(
      `${url} @${width}px: ожидалась выноска «${expectedId}», а на странице ` +
        `открыт: ${seen}. document.querySelector по первому [data-hint-box] ` +
        'раньше в такой ситуации молча смерял чужой пузырёк — теперь это ' +
        'явный провал, а не зелёный вердикт о не том органе.',
    );
    return false;
  }
  if (!match.box) {
    console.error(
      `${url} @${width}px: у выноски «${expectedId}» нет data-hint-box ` +
        '(бывает, пока не устоялись шрифты) — замер не готов.',
    );
    return false;
  }

  const [left, , boxWidth, height, viewport] = match.box.split(',').map(Number);

  // Требование, а не формальность: узкий случай, замеренный при подмененном
  // (клэмпнутом) окне, дал бы зелёный вердикт о непроверенном экране — той
  // самой дыре, ради которой существует эта задача. Расхождение — провал, а
  // не тихое OK.
  if (viewport !== width) {
    console.error(
      `${url} @${width}px: фактическая ширина окна ${viewport} не совпадает с запрошенной ` +
        `${width} — замер недостоверен, вердикт не выносится.`,
    );
    return false;
  }

  const right = left + boxWidth;
  const ok = left >= MARGIN && right <= viewport - MARGIN;
  console.log(
    `${url} @${width}px: «${expectedId}» ${boxWidth}×${height} на ${left}..${right} ` +
      `при фактическом окне ${viewport} — ${ok ? 'OK' : 'ВЫЛЕЗАЕТ'}`,
  );
  return ok;
}

let failed = false;
for (const { url, width, expectedId } of CASES) {
  let tags;
  try {
    tags =
      width < CDP_BELOW_WIDTH ? await measureViaCdp(url, width) : measureViaDumpDom(url, width);
  } catch (err) {
    console.error(`${url} @${width}px: замер не запустился — ${err.message}`);
    failed = true;
    continue;
  }
  if (!evaluateCase(url, width, expectedId, tags)) failed = true;
}

process.exit(failed ? 1 : 0);
