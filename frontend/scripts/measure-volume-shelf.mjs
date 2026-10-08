// Заглавия на торцах книг: сколько их обрывается, хватает ли строке места и
// сколько книг показывает свёрнутый штабель. jsdom раскладку не считает,
// поэтому меряем настоящим движком; замер кладёт в data-fit сама страница
// (useClippedFlag), потому что --dump-dom геометрии не отдаёт. Дамп не
// обрезать: атрибуты стоят в дальнем конце строки.
// Запуск: `npm run measure:volume-shelf`. Нужен поднятый фронт — 3100 обычно
// занят рабочим, поэтому свой:
//   npm run dev -- --port 3200 --strictPort
//   BASE=http://localhost:3200 npm run measure:volume-shelf
import { execFileSync } from 'node:child_process';

const CHROME = process.env.CHROME ?? 'google-chrome';
const BASE = process.env.BASE ?? 'http://localhost:3100';

/**
 * Ниже этой ширины --window-size в этой сборке Chrome клэмпается: запросишь
 * 390 — получишь фактические 500 (проверено, window.innerWidth в дампе). Обход
 * есть, `Emulation.setDeviceMetricsOverride` через CDP, — образец в
 * measure-feature-hint.mjs. Здесь он не нужен, см. случай узкого экрана ниже,
 * но молча принять 390 и напечатать «@390px» скрипт не имеет права: это
 * зелёный отчёт о непроверенном.
 */
const CLAMP_WIDTH = 500;

// ПСС Ленина — худший случай: 45 томов, и в каждом по шесть десятков работ,
// так что заглавие почти всегда собирается из двух и обрывается.
//
// Первый случай — широкий экран, два столбца штабеля. Второй — узкий, то есть
// ветка @media (max-width: 700px): один столбец и свёрнутое собрание. Он
// стережёт две вещи разом. Первая — само наличие заглавия: на телефоне оно
// однажды уже пряталось в display: none, и пустой книги не ловил никто.
// Вторая — сворачивание: показанных книг должно быть ровно десять, и если
// медиазапрос разъедется с COLLAPSED_VOLUMES в shelfGeometry.ts, разойдутся
// именно эти числа.
//
// Ширина взята 500, а не 390: обе проверки от ширины окна не зависят, обе
// лежат внутри медиазапроса, а 500 — единственная ширина у нижнего края,
// которую этот Chrome отдаёт честно. Настоящие 390 проверяются скриншотом,
// там клэмпа нет.
const CASES = [
  { url: `${BASE}/editions/4`, width: 1280, minLabel: 300 },
  { url: `${BASE}/editions/4`, width: CLAMP_WIDTH, minLabel: 200, shown: 10 },
];

const tooNarrow = CASES.filter((c) => c.width < CLAMP_WIDTH);
if (tooNarrow.length > 0) {
  console.error(
    `случаи ýже ${CLAMP_WIDTH}px: ${tooNarrow.map((c) => c.width).join(', ')} — ` +
      '--window-size их поджимает, и отчёт назовёт ширину, при которой не мерил. ' +
      'Такой случай ведётся через CDP, образец в scripts/measure-feature-hint.mjs',
  );
  process.exit(1);
}

let failed = false;
for (const { url, width, minLabel, shown } of CASES) {
  const out = execFileSync(
    CHROME,
    [
      '--headless=new',
      '--disable-gpu',
      // Виртуальное время нужно, чтобы страница успела загрузить шрифты:
      // Literata шире запасной Georgia, и замер до её загрузки объявил бы
      // поместившимся то, что потом обрежется. Атрибут ставится только
      // после document.fonts.ready.
      '--virtual-time-budget=20000',
      `--window-size=${width},1200`,
      '--dump-dom',
      url,
    ],
    { encoding: 'utf8', maxBuffer: 64 * 1024 * 1024 },
  );

  const fits = [...out.matchAll(/data-fit="(\d+),(\d+)"/g)].map(([, scroll, client]) => ({
    scroll: Number(scroll),
    client: Number(client),
  }));
  if (fits.length === 0) {
    console.error(`${url} @${width}px: заглавий в дампе нет — проверь, что штабель открывается`);
    failed = true;
    continue;
  }

  // Свёрнутая книга скрыта в display: none, и ширины у неё нули. Это и есть
  // счётчик показанных строк: живыми остаются ровно те, что видны.
  const visible = fits.filter((f) => f.client > 0);
  const clipped = visible.filter((f) => f.scroll > f.client);
  const cramped = visible.filter((f) => f.client < minLabel);
  // Книги считаются отдельно от строк: подпись тома разобрана на работы и
  // занимает до двух строк, поэтому счётчик строк на сворачивание не
  // отвечает. Считанный по строкам, он объявлял шестнадцать книг там, где их
  // десять, и обвинял в этом медиазапрос.
  //
  // Считать по классу is-overflow тоже нельзя: класс стоит на книге при любой
  // ширине, а прячет её медиазапрос — на широком экране все книги с этим
  // классом видны. Видимость меряется тем же, чем у строки: у скрытой книги
  // все её строки имеют нулевую клиентскую ширину.
  const chunks = out.split(/class="volume-slab(?![-\w])/).slice(1);
  const books = chunks.length;
  const shownBooks = chunks.filter((chunk) => /data-fit="\d+,[1-9]\d*"/.test(chunk)).length;
  const mute = chunks.filter((chunk) => !/data-fit="/.test(chunk)).length;
  if (mute > 0) {
    console.error(`  книг без единой строки подписи: ${mute} — книга едет пустой`);
    failed = true;
  }
  console.log(
    `${url} @${width}px: книг ${books}, показано ${shownBooks}; ` +
      `строк подписи ${fits.length}, видно ${visible.length}, ` +
      `оборвано ${clipped.length}, ýже ${minLabel}px — ${cramped.length}`,
  );

  if (visible.length === 0) {
    console.error('  ни одного видимого заглавия — книга едет пустой');
    failed = true;
  }
  if (cramped.length > 0) {
    // Строке остаётся меньше, чем нужно, чтобы книгу можно было прочесть:
    // либо номер в хвосте съел ширину, либо у заглавия потерялся min-width: 0
    // и флекс отдал ему нулевую долю.
    console.error(
      `  тесные строки: ${[...new Set(cramped.map((f) => f.client))].join(', ')} — ` +
        'заглавию не осталось места, книга читается обрывком',
    );
    failed = true;
  }
  if (shown !== undefined && shownBooks !== shown) {
    console.error(
      `  свёрнутый штабель показывает ${shownBooks} книг вместо ${shown} — ` +
        'медиазапрос в VolumeShelf.css разошёлся с COLLAPSED_VOLUMES в shelfGeometry.ts',
    );
    failed = true;
  }
}

process.exit(failed ? 1 : 0);
