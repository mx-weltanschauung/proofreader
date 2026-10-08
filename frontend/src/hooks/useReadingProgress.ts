import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { useLocation } from 'react-router-dom';

export function computeProgress(
  scrollTop: number,
  scrollHeight: number,
  clientHeight: number,
): number {
  const scrollable = scrollHeight - clientHeight;
  if (scrollable <= 0) return 100;
  const pct = (scrollTop / scrollable) * 100;
  return Math.max(0, Math.min(100, Math.round(pct)));
}

export function storageKey(workId: number, chapterId: number): string {
  return `reading-pos:${workId}:${chapterId}`;
}

/**
 * Список мест, где читатель остановился, — свежие первыми, по строке на
 * главу. Читатель ведёт несколько книг разом, и одна общая запись, которой
 * это место было прежде, затирала начатую главу одного тома, стоило открыть
 * другой.
 */
export const RECENT_KEY = 'reading-recent';

/**
 * Прежняя единственная запись. Читается, пока списка ещё нет, и снимается
 * первой же записью в список — иначе у читателя, пришедшего после выкатки,
 * пропало бы его «Продолжить».
 */
export const LEGACY_LAST_READ_KEY = 'reading-last';

/**
 * Дочитанные главы из списка не убираются: «дочитал» по прокрутке не
 * определить (блок сносок стоит внизу главы, и прыжок к нему выглядел бы
 * концом чтения). Их вытесняет потолок.
 */
export const RECENT_LIMIT = 5;

/**
 * Сколько глава должна пробыть на экране, прежде чем попасть в список:
 * пролистанная мимо глава — не начатая. Считается только время видимой
 * вкладки и только за одно открытие.
 */
export const DWELL_MS = 60_000;

export interface LastRead {
  workId: number;
  chapterId: number;
  workTitle: string;
  chapterTitle: string;
  /** Номер печатной страницы, если он известен. */
  pageNumber: number | null;
  ts: number;
}

/** Место без отметки времени — её ставит запись. */
export type ReadPlace = Omit<LastRead, 'ts'>;

/**
 * Заголовки хранятся вместе с записью, чтобы главная не делала запрос ради
 * строки. Если главу переименуют, надпись устареет до следующего открытия
 * главы — плата приемлемая.
 */
export function readRecent(): LastRead[] {
  // Обращение к хранилищу тоже внутри try: в приватном окне и во вложенном
  // контексте с запретом на хранилище getItem бросает. Чтение идёт из
  // инициализатора состояния главной, то есть во время рендера, — исключение
  // отсюда оставило бы входную страницу сайта пустой.
  try {
    const raw = localStorage.getItem(RECENT_KEY);
    if (raw !== null) {
      const parsed: unknown = JSON.parse(raw);
      return Array.isArray(parsed) ? parsed.filter(isLastRead) : [];
    }
    const legacy = localStorage.getItem(LEGACY_LAST_READ_KEY);
    if (!legacy) return [];
    const parsed: unknown = JSON.parse(legacy);
    return isLastRead(parsed) ? [parsed] : [];
  } catch {
    return [];
  }
}

/**
 * Запись лежит в localStorage, а он открыт на запись кому угодно. Поэтому
 * проверяется каждое поле, а не только идентификаторы: заголовок-объект
 * дойдёт до React как дочерний узел и уронит рендер, а отсутствующий
 * pageNumber напечатается на главной как «· стр. undefined».
 */
function isLastRead(value: unknown): value is LastRead {
  if (typeof value !== 'object' || value === null) return false;
  const v = value as Record<string, unknown>;
  return (
    typeof v.workId === 'number' &&
    typeof v.chapterId === 'number' &&
    typeof v.workTitle === 'string' &&
    typeof v.chapterTitle === 'string' &&
    (typeof v.pageNumber === 'number' || v.pageNumber === null)
  );
}

function samePlace(e: LastRead, workId: number, chapterId: number): boolean {
  return e.workId === workId && e.chapterId === chapterId;
}

function writeRecent(list: LastRead[]): void {
  try {
    localStorage.setItem(RECENT_KEY, JSON.stringify(list));
    localStorage.removeItem(LEGACY_LAST_READ_KEY);
  } catch {
    // Хранилище недоступно или переполнено. Список — удобство, ради которого
    // не стоит ронять чтение главы.
  }
}

/** Ставит главу первой строкой; прежняя строка той же главы уходит. */
export function recordRead(entry: LastRead): void {
  const rest = readRecent().filter((e) => !samePlace(e, entry.workId, entry.chapterId));
  writeRecent([entry, ...rest].slice(0, RECENT_LIMIT));
}

export function forgetRead(workId: number, chapterId: number): void {
  writeRecent(readRecent().filter((e) => !samePlace(e, workId, chapterId)));
}

function isRecorded(workId: number, chapterId: number): boolean {
  return readRecent().some((e) => samePlace(e, workId, chapterId));
}

/**
 * Держит место в списке недавно читанного. Глава, которой в списке нет,
 * заводится только после DWELL_MS видимого времени; уже известная
 * поднимается сразу, и дальше каждая смена страницы пишется без задержки.
 * `null` — места нет (текст не готов, глава не известна) — не пишет ничего.
 */
export function useRecordRead(place: ReadPlace | null): void {
  const placeKey = place ? `${place.workId}:${place.chapterId}` : null;
  // Таймер порога срабатывает позже рендера, а записать должен ту страницу,
  // что на экране в этот момент, а не ту, с которой глава открылась.
  const latest = useRef(place);
  // Глава, которой запись уже разрешена: известна списку или отбыла порог.
  const counted = useRef<string | null>(null);

  useEffect(() => {
    latest.current = place;
  });

  useEffect(() => {
    counted.current = null;
    const current = latest.current;
    if (placeKey === null || current === null) return;
    if (isRecorded(current.workId, current.chapterId)) {
      counted.current = placeKey;
      return;
    }

    let remaining = DWELL_MS;
    let startedAt = 0;
    let timer = 0;
    const done = () => {
      timer = 0;
      counted.current = placeKey;
      const now = latest.current;
      if (now) recordRead({ ...now, ts: Date.now() });
    };
    const start = () => {
      if (timer || document.visibilityState !== 'visible') return;
      startedAt = Date.now();
      timer = window.setTimeout(done, remaining);
    };
    const pause = () => {
      if (!timer) return;
      window.clearTimeout(timer);
      timer = 0;
      remaining -= Date.now() - startedAt;
    };
    const onVisibility = () => (document.visibilityState === 'visible' ? start() : pause());

    document.addEventListener('visibilitychange', onVisibility);
    start();
    return () => {
      document.removeEventListener('visibilitychange', onVisibility);
      if (timer) window.clearTimeout(timer);
    };
  }, [placeKey]);

  // Отдельный эффект: запись обновляется при смене видимой страницы, а счёт
  // порога при этом не начинается заново. Объявлен после эффекта порога —
  // на смене главы тот успевает решить, известна ли она списку.
  const pageNumber = place?.pageNumber;
  const workTitle = place?.workTitle;
  const chapterTitle = place?.chapterTitle;
  useEffect(() => {
    const current = latest.current;
    if (placeKey === null || current === null || counted.current !== placeKey) return;
    recordRead({ ...current, ts: Date.now() });
  }, [placeKey, pageNumber, workTitle, chapterTitle]);
}

interface ProgressOptions {
  workId?: number;
  chapterId?: number;
  workTitle?: string;
  chapterTitle?: string;
  pageNumber?: number | null;
  ready?: boolean;
  /**
   * Вести ли главу в списке недавно читанного (RECENT_KEY).
   * По умолчанию true — ради ChapterView и всех нынешних потребителей.
   *
   * Строка списка адресует главу тома (её читают Dashboard и VolumeMasthead,
   * строя ссылку `/works/{workId}/chapters/{chapterId}`). У элемента
   * подборки chapterId — это id строки collection_items, а не id главы:
   * попади он в список, «Продолжить» на главной вело бы либо на
   * несуществующую главу, либо — при случайном совпадении id — в чужую.
   * Позиция прокрутки при этом всё равно нужна, поэтому не отключается
   * целиком, а только запись в общий виджет.
   */
  trackLastRead?: boolean;
}

export function useReadingProgress(options: ProgressOptions): number {
  const {
    workId,
    chapterId,
    workTitle,
    chapterTitle,
    pageNumber,
    ready,
    trackLastRead = true,
  } = options;
  const [progress, setProgress] = useState(0);
  const { hash } = useLocation();

  // Куда смотрит окно, когда глава открылась. Отдельно от подписки на
  // прокрутку ниже — и слоем раньше (useLayoutEffect), чтобы читатель не
  // увидел кадра с новым текстом на старом смещении.
  //
  // Сброс наверх обязателен, а не «на всякий случай»: переход по ссылке
  // «следующая глава» — это pushState в том же документе, и смещение окна не
  // трогает никто. Дочитавший главу до конца получал следующую открытой на
  // том же смещении, а если она короче — прижатой браузером к её низу, то
  // есть открытой своим концом.
  //
  // Якорем из адреса распоряжается useHashAnchor (ReadingSurface): у входящей
  // ссылки на полосу прокрутка уже назначена, и сброс наверх увёл бы читателя
  // ровно с той полосы, ради которой ссылку прислали.
  useLayoutEffect(() => {
    if (!ready || workId == null || chapterId == null) return;
    if (hash) return;
    const saved = localStorage.getItem(storageKey(workId, chapterId));
    window.scrollTo(0, saved ? parseInt(saved, 10) || 0 : 0);
  }, [workId, chapterId, ready, hash]);

  useEffect(() => {
    if (!ready || workId == null || chapterId == null) return;
    const key = storageKey(workId, chapterId);

    let raf = 0;
    const onScroll = () => {
      if (raf) return;
      raf = window.requestAnimationFrame(() => {
        raf = 0;
        const el = document.documentElement;
        setProgress(computeProgress(el.scrollTop, el.scrollHeight, el.clientHeight));
        localStorage.setItem(key, String(el.scrollTop));
      });
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => {
      window.removeEventListener('scroll', onScroll);
      if (raf) cancelAnimationFrame(raf);
    };
  }, [workId, chapterId, ready]);

  // Отдельный хук: запись обновляется при смене видимой страницы, а
  // подписка на скролл при этом не пересоздаётся.
  useRecordRead(
    ready && trackLastRead && workId != null && chapterId != null
      ? {
          workId,
          chapterId,
          workTitle: workTitle ?? '',
          chapterTitle: chapterTitle ?? '',
          pageNumber: pageNumber ?? null,
        }
      : null,
  );

  return progress;
}
