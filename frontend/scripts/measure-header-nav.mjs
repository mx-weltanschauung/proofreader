// Меню шапки. jsdom раскладку не считает, поэтому меряем живым движком — тот
// же CDP-приём, что и measure-header-search.mjs (--window-size в этой сборке
// Chrome клэмпает ниже 500px, поэтому все ширины идут через
// Emulation.setDeviceMetricsOverride).
//
// Что стережём: шапка не имеет права делать документ шире экрана НИ НА ОДНОЙ
// ширине — ни у читателя, ни у того, кто вошёл. Набор ссылок в меню зависит от
// роли (у администратора их семь против двух у читателя), поэтому мерить в
// одиночку под анонимом бесполезно: до починки ряд .header-nav был
// нерастяжимым внутри .header-left, который сам не верстается в несколько
// строк, и его собственная ширина (730px) становилась шириной документа —
// страница ехала вбок на любой ширине до ~1390px, а «Кэш», последняя ссылка,
// уезжала за правый край целиком, до неё приходилось листать.
//
// Поэтому проверок две: нет горизонтального переполнения документа, и каждая
// ссылка меню целиком внутри экрана. Первого мало: если бы шапке когда-нибудь
// прописали overflow: hidden, переполнение исчезло бы вместе с доступом к
// ссылке, и проверка осталась бы зелёной.
//
// Запуск: `npm run measure:header-nav`. Нужен поднятый фронт и живой бэкенд за
// ним (вход идёт через тот же origin, /api проксируется дев-сервером):
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:header-nav
// Учётка администратора — из окружения (ADMIN_EMAIL/ADMIN_PASSWORD), умолчания
// те же, что сеет бэкенд при старте.
import { spawn } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3200';
const ADMIN_EMAIL = process.env.ADMIN_EMAIL ?? 'admin@proofreader.local';
const ADMIN_PASSWORD = process.env.ADMIN_PASSWORD ?? 'admin';

// Сверху вниз: десктоп, полоса сразу над брейкпоинтом (там тесно из-за почты
// и кнопки выхода в .header-right), сам брейкпоинт и телефоны вплоть до 320px.
const WIDTHS = [1440, 1280, 1100, 1000, 900, 850, 800, 769, 768, 600, 500, 430, 390, 360, 320];

const EXPR = `(async () => {
  await document.fonts.ready;
  const r = (el) => (el ? el.getBoundingClientRect() : null);
  return JSON.stringify({
    viewport: window.innerWidth,
    scrollWidth: document.documentElement.scrollWidth,
    headerHeight: r(document.querySelector('.site-header'))?.height ?? null,
    // Правая часть — по элементам, а не целиком: подпись роли умеет вылезти из
    // сжатой колонки почты под «Aa», не выходя за границы .header-right.
    blocks: [
      ['логотип и меню', '.header-left'],
      ['кнопка поиска', '.header-search .search-trigger'],
      ['Aa', '.reading-settings-toggle'],
      ['почта', '.user-email'],
      ['роль', '.user-role'],
      ['Выйти', '.header-right .btn'],
    ].map(([name, sel]) => ({ name, rect: r(document.querySelector(sel)) })),
    links: [...document.querySelectorAll('.header-nav-link')].map((a) => ({
      text: a.textContent.trim().replace(/\\s+/g, ' '),
      left: r(a).left,
      right: r(a).right,
    })),
  });
})()`;

/** Токен администратора — через тот же origin, что и страница: /api проксируется дев-сервером. */
async function login() {
  const res = await fetch(`${BASE}/api/auth/login`, {
    method: 'POST',
    headers: { 'Content-Type': 'application/json' },
    body: JSON.stringify({ email: ADMIN_EMAIL, password: ADMIN_PASSWORD }),
  });
  if (!res.ok) {
    throw new Error(
      `вход администратором не удался (${res.status}): нужен поднятый бэкенд за ${BASE} ` +
        'и верные ADMIN_EMAIL/ADMIN_PASSWORD',
    );
  }
  const { token, user } = await res.json();
  if (user?.role !== 'administrator') {
    throw new Error(`учётка ${ADMIN_EMAIL} не администратор (role=${user?.role}) — мерить нечего`);
  }
  return { token, user };
}

/** Открывает headless Chrome, эмулирует вьюпорт заданной ширины через CDP, возвращает JSON из EXPR. */
async function measure(width, session) {
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
    if (session) {
      // Состояние входа читается из localStorage (zustand persist, ключ
      // auth-storage); checkAuth() приложение не зовёт, поэтому одного
      // 'token' мало.
      const persisted = JSON.stringify({
        state: { token: session.token, user: session.user, isAuthenticated: true },
        version: 0,
      });
      await send('Page.addScriptToEvaluateOnNewDocument', {
        source:
          `try { localStorage.setItem('token', ${JSON.stringify(session.token)});` +
          ` localStorage.setItem('auth-storage', ${JSON.stringify(persisted)}); } catch (e) {}`,
      });
    }
    await send('Page.navigate', { url: `${BASE}/` });
    // Шрифты ждём внутри EXPR (document.fonts.ready): ширина ссылок меню
    // меняется вместе со шрифтом, и на недогруженном замер на границе пляшет.
    await new Promise((r) => setTimeout(r, 2000));

    const res = await send('Runtime.evaluate', {
      expression: EXPR,
      returnByValue: true,
      awaitPromise: true,
    });
    ws.close();
    const value = res.result?.value;
    return value ? JSON.parse(value) : null;
  } finally {
    child.kill();
  }
}

function check(width, m, { minLinks }) {
  const problems = [];
  if (!m) {
    problems.push('шапка не отрисовалась — .header-nav-link нет в DOM');
    return problems;
  }
  if (m.viewport !== width) {
    problems.push(
      `запрошено ${width}px, фактический вьюпорт ${m.viewport}px — замер шёл не на том экране`,
    );
  }
  if (m.links.length < minLinks) {
    problems.push(
      `в меню ${m.links.length} ссылок, ожидалось не меньше ${minLinks} — ` +
        'состояние входа не доехало, замер не про то',
    );
  }
  if (m.scrollWidth > m.viewport + 0.5) {
    problems.push(
      `страница переполнена по горизонтали: scrollWidth=${m.scrollWidth} при viewport=${m.viewport}`,
    );
  }
  const escaped = m.links.filter((l) => l.right > m.viewport + 0.5 || l.left < -0.5);
  if (escaped.length) {
    problems.push(
      'ссылки меню за краем экрана: ' +
        escaped.map((l) => `«${l.text}» ${l.left.toFixed(0)}..${l.right.toFixed(0)}`).join(', '),
    );
  }
  // Переполнения документа мало: блок шапки может залезть на соседа, не выйдя
  // за экран. Так кнопка поиска (200px) вылезала из сжатой обёртки
  // .header-search под «Aa» на ~1000px у администратора, а обе проверки выше
  // оставались зелёными. Мерим видимую кнопку, а не обёртку: обёртка
  // сжималась честно, наезжало её содержимое.
  const present = m.blocks.filter((b) => b.rect && b.rect.width > 0 && b.rect.height > 0);
  for (let i = 0; i < present.length; i++) {
    for (let j = i + 1; j < present.length; j++) {
      const a = present[i].rect;
      const b = present[j].rect;
      const dx = Math.min(a.right, b.right) - Math.max(a.left, b.left);
      const dy = Math.min(a.bottom, b.bottom) - Math.max(a.top, b.top);
      if (dx > 0.5 && dy > 0.5) {
        problems.push(
          `«${present[i].name}» и «${present[j].name}» наезжают друг на друга на ${dx.toFixed(0)}px`,
        );
      }
    }
  }
  return problems;
}

let failed = false;
const session = await login();

for (const role of ['аноним', 'администратор']) {
  const auth = role === 'администратор' ? session : null;
  // У читателя три ссылки, у администратора ещё семь в служебной полосе
  // (.header-staff) — класс ссылок тот же, поэтому они считаются вместе.
  const minLinks = auth ? 10 : 3;
  for (const width of WIDTHS) {
    const m = await measure(width, auth);
    const problems = check(width, m, { minLinks });
    if (problems.length) {
      failed = true;
      console.error(`FAIL ${role} @${width}px: ${problems.join('; ')}`);
    } else {
      console.log(
        `ok ${role} @${width}px: ${m.links.length} ссылок в экране, переполнения нет ` +
          `(шапка ${m.headerHeight.toFixed(0)}px)`,
      );
    }
  }
}

process.exit(failed ? 1 : 0);
