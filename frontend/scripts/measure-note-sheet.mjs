// Нижний лист примечания: тап пальцем по маркеру сноски. jsdom не считает ни
// раскладку, ни настоящие события указателя — а именно на них держится вся
// развилка «мышь/палец», поэтому меряем живым движком через CDP: эмуляция
// тач-экрана и настоящий Input.dispatchTouchEvent, а не синтетический click.
//
// Запуск (3100 обычно занят рабочим фронтом из docker, там СОБРАННЫЙ бандл —
// свежий код туда не попадает):
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:note-sheet
// Страница задаётся через URL=/works/<id>/read/<номер>; по умолчанию взята
// полоса, где есть и звёздочная сноска, и номерное примечание.
// Имя бинарника Chrome — через CHROME (google-chrome-stable, chromium…).
import { spawn } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3200';
const PATH_ = process.env.URL ?? '/works/138/read/3';
/** Какой маркер щупать: по умолчанию первый на полосе. */
const SELECTOR = process.env.SELECTOR ?? '.chapter-pages-content sup.footnote-ref a';
const VIEWPORT = { width: 390, height: 844 };
/** Комфортный минимум зоны нажатия по рекомендациям Apple/Google. */
const FINGER_PX = 44;
/**
 * Для самого маркера 44px недостижимы честно: по вертикали зона нажатия
 * упирается в межстрочник (строка ~27px), и растянув её до 44 надстрочная
 * цифра начала бы перехватывать тапы у слов строкой выше и ниже. Поэтому от
 * маркера требуется меньшее — но заметно больше исходных 14px.
 */
const MARKER_MIN_PX = 28;

async function connect() {
  const child = spawn(CHROME, [
    '--headless=new',
    '--disable-gpu',
    '--remote-debugging-port=0',
    '--window-size=1280,900',
    'about:blank',
  ]);

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

  return { child, ws, send };
}

/** Считает выражение на странице и возвращает разобранный JSON. */
async function evaluate(send, expression) {
  const res = await send('Runtime.evaluate', {
    expression,
    returnByValue: true,
    awaitPromise: true,
  });
  if (res.exceptionDetails) {
    throw new Error(`ошибка на странице: ${res.exceptionDetails.exception?.description ?? '?'}`);
  }
  return JSON.parse(res.result.value ?? 'null');
}

/** Ждёт, пока выражение вернёт непустое значение. */
async function waitFor(send, expression, what, timeoutMs = 20000) {
  const deadline = Date.now() + timeoutMs;
  for (;;) {
    const value = await evaluate(send, expression);
    if (value) return value;
    if (Date.now() > deadline) throw new Error(`не дождался: ${what}`);
    await new Promise((r) => setTimeout(r, 300));
  }
}

async function tap(send, x, y) {
  const point = [{ x, y, radiusX: 12, radiusY: 12, force: 1 }];
  await send('Input.dispatchTouchEvent', { type: 'touchStart', touchPoints: point });
  await send('Input.dispatchTouchEvent', { type: 'touchEnd', touchPoints: [] });
}

const problems = [];
const check = (ok, message) => {
  console.log(`${ok ? 'ok  ' : 'FAIL'} ${message}`);
  if (!ok) problems.push(message);
};

const { child, ws, send } = await connect();
try {
  await send('Page.enable');
  await send('Emulation.setDeviceMetricsOverride', {
    ...VIEWPORT,
    deviceScaleFactor: 2,
    mobile: true,
  });
  await send('Emulation.setTouchEmulationEnabled', { enabled: true, maxTouchPoints: 5 });
  await send('Page.navigate', { url: `${BASE}${PATH_}` });

  // Маркер примечания: надстрочная цифра со ссылкой на #fn:…
  const marker = await waitFor(
    send,
    `(() => {
       const a = document.querySelector(${JSON.stringify(SELECTOR)});
       if (!a) return null;
       a.scrollIntoView({ block: 'center' });
       const r = a.getBoundingClientRect();
       return JSON.stringify({
         text: a.textContent,
         href: a.getAttribute('href'),
         x: r.left + r.width / 2,
         y: r.top + r.height / 2,
         w: r.width,
         h: r.height,
       });
     })()`,
    'маркер сноски на полосе',
  );
  console.log(`маркер «${marker.text}» → ${marker.href}`);
  check(
    marker.w >= MARKER_MIN_PX && marker.h >= MARKER_MIN_PX,
    `зона нажатия маркера ${marker.w.toFixed(1)}×${marker.h.toFixed(1)}px ` +
      `(минимум ${MARKER_MIN_PX}px)`,
  );

  const hashBefore = await evaluate(send, 'JSON.stringify(location.hash)');
  await tap(send, marker.x, marker.y);

  const sheet = await waitFor(
    send,
    `(() => {
       const s = document.querySelector('.note-sheet');
       if (!s) return null;
       // Лист выезжает снизу. Замер в первом кадре анимации показывает его
       // ещё за краем экрана — ждём, пока раскладка устоится.
       const anims = s.getAnimations ? s.getAnimations() : [];
       if (anims.some((a) => a.playState === 'running')) return null;
       const r = s.getBoundingClientRect();
       const body = s.querySelector('.note-sheet-body');
       const goto = s.querySelector('.note-sheet-goto');
       return JSON.stringify({
         top: r.top, bottom: r.bottom, left: r.left, right: r.right,
         title: s.querySelector('.note-sheet-title')?.textContent ?? null,
         text: (body?.textContent ?? '').trim().slice(0, 60),
         scrolls: body ? body.scrollHeight > body.clientHeight + 1 : false,
         goto: goto?.textContent ?? null,
         gotoHeight: goto ? goto.getBoundingClientRect().height : 0,
         closeBox: (() => {
           const c = s.querySelector('.note-sheet-close');
           if (!c) return null;
           const b = c.getBoundingClientRect();
           return { w: b.width, h: b.height };
         })(),
         viewport: { w: window.innerWidth, h: window.innerHeight },
         hash: location.hash,
       });
     })()`,
    'лист примечания после тапа',
  );

  console.log(JSON.stringify(sheet, null, 2));
  check(
    sheet.viewport.w === VIEWPORT.width,
    `эмулированный экран ${sheet.viewport.w}px (запрошено ${VIEWPORT.width})`,
  );
  check(sheet.hash === hashBefore, `тап не уехал по якорю (hash ${sheet.hash || 'пуст'})`);
  check(
    Math.abs(sheet.bottom - sheet.viewport.h) < 1,
    `лист прижат к низу экрана (низ ${sheet.bottom.toFixed(1)} из ${sheet.viewport.h})`,
  );
  check(sheet.top > 0, `лист не вылезает за верх экрана (верх ${sheet.top.toFixed(1)})`);
  check(
    sheet.bottom - sheet.top <= sheet.viewport.h * 0.7 + 1,
    `лист не выше 70% экрана (${(sheet.bottom - sheet.top).toFixed(1)}px)`,
  );
  check(sheet.left >= 0 && sheet.right <= sheet.viewport.w, 'лист по ширине внутри экрана');
  check(!!sheet.text, `в листе есть текст примечания: «${sheet.text}»`);
  check(!!sheet.goto, `в листе есть переход: «${sheet.goto}»`);
  check(
    sheet.gotoHeight >= FINGER_PX,
    `переход пальцевой высоты: ${sheet.gotoHeight.toFixed(1)}px`,
  );
  check(
    (sheet.closeBox?.w ?? 0) >= FINGER_PX && (sheet.closeBox?.h ?? 0) >= FINGER_PX,
    `крестик пальцевого размера: ${sheet.closeBox?.w}×${sheet.closeBox?.h}px`,
  );

  // Переход из листа: лист закрывается, примечание оказывается на экране.
  const gotoBox = await evaluate(
    send,
    `(() => {
       const g = document.querySelector('.note-sheet-goto');
       const r = g.getBoundingClientRect();
       return JSON.stringify({ x: r.left + r.width / 2, y: r.top + r.height / 2 });
     })()`,
  );
  await tap(send, gotoBox.x, gotoBox.y);
  const after = await waitFor(
    send,
    `(() => {
       if (document.querySelector('.note-sheet')) return null;
       const id = ${JSON.stringify(marker.href.slice(1))};
       const note = document.getElementById(id);
       if (!note) return JSON.stringify({ closed: true, note: false });
       const r = note.getBoundingClientRect();
       return JSON.stringify({
         closed: true,
         note: true,
         onScreen: r.top >= 0 && r.top < window.innerHeight,
         top: r.top,
       });
     })()`,
    'закрытие листа после перехода',
  );
  check(after.closed, 'переход закрыл лист');
  check(after.note && after.onScreen, `примечание на экране (верх ${after.top?.toFixed(1)}px)`);
} finally {
  ws.close();
  child.kill();
}

if (problems.length > 0) {
  console.error(`\nпровалено проверок: ${problems.length}`);
  process.exit(1);
}
console.log('\nвсе проверки прошли');
