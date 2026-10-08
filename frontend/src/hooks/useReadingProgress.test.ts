import { describe, it, expect, beforeEach, afterEach, vi } from 'vitest';
import { renderHook, act } from '@testing-library/react';
import { createElement, type ReactNode } from 'react';
import { MemoryRouter } from 'react-router-dom';
import {
  computeProgress,
  storageKey,
  RECENT_KEY,
  RECENT_LIMIT,
  LEGACY_LAST_READ_KEY,
  DWELL_MS,
  readRecent,
  recordRead,
  forgetRead,
  useRecordRead,
  useReadingProgress,
  type LastRead,
  type ReadPlace,
} from './useReadingProgress';

describe('computeProgress', () => {
  it('is 0 at top', () => expect(computeProgress(0, 2000, 1000)).toBe(0));
  it('is 100 at bottom', () => expect(computeProgress(1000, 2000, 1000)).toBe(100));
  it('clamps and rounds mid', () => expect(computeProgress(500, 2000, 1000)).toBe(50));
  it('handles no-scroll pages as 100', () => expect(computeProgress(0, 800, 1000)).toBe(100));
});
describe('storageKey', () => {
  it('is stable per chapter', () => expect(storageKey(3, 7)).toBe('reading-pos:3:7'));
});

function entry(workId: number, chapterId: number, pageNumber: number | null = 10): LastRead {
  return {
    workId,
    chapterId,
    workTitle: `Том ${workId}`,
    chapterTitle: `Глава ${chapterId}`,
    pageNumber,
    ts: 1,
  };
}

const places = () => readRecent().map((e) => [e.workId, e.chapterId]);

describe('список недавно читанного', () => {
  beforeEach(() => localStorage.clear());

  it('без записей пуст', () => {
    expect(readRecent()).toEqual([]);
  });

  it('не падает на испорченной записи', () => {
    localStorage.setItem(RECENT_KEY, 'не json');
    expect(readRecent()).toEqual([]);
  });

  it('не принимает не-список', () => {
    localStorage.setItem(RECENT_KEY, JSON.stringify(entry(8, 55)));
    expect(readRecent()).toEqual([]);
  });

  // localStorage открыт на запись кому угодно, а прочитанное отсюда идёт
  // прямо в разметку главной: заголовок-объект уронит рендер, пропавший
  // pageNumber напечатается как «стр. undefined». Выбрасывается только
  // негодная строка — одна битая запись не должна гасить весь список.
  it.each([
    ['заголовок главы — объект', { chapterTitle: { ru: 'Глава' } }],
    ['заголовок тома пропал', { workTitle: undefined }],
    ['номер страницы — строка', { pageNumber: '214' }],
    ['номера страницы нет', { pageNumber: undefined }],
    ['идентификатор тома — строка', { workId: '8' }],
  ])('выбрасывает строку, где %s, и оставляет соседние', (_case, override) => {
    localStorage.setItem(
      RECENT_KEY,
      JSON.stringify([entry(3, 10), { ...entry(8, 55), ...override }, entry(4, 20)]),
    );
    expect(places()).toEqual([
      [3, 10],
      [4, 20],
    ]);
  });

  // Страницу знаем не всегда: пока IntersectionObserver не сообщил видимую,
  // в записи стоит null — и это законная запись, а не мусор.
  it('принимает запись без известного номера страницы', () => {
    localStorage.setItem(RECENT_KEY, JSON.stringify([entry(8, 55, null)]));
    expect(readRecent()[0]?.pageNumber).toBeNull();
  });

  // В приватном окне и во вложенном контексте с запретом на хранилище доступ
  // к нему бросает. Чтение идёт во время рендера главной — исключение отсюда
  // оставило бы входную страницу пустой.
  it('переживает недоступное хранилище', () => {
    const deny = () => {
      throw new DOMException('denied', 'SecurityError');
    };
    const getItem = vi.spyOn(Storage.prototype, 'getItem').mockImplementation(deny);
    const setItem = vi.spyOn(Storage.prototype, 'setItem').mockImplementation(deny);
    const removeItem = vi.spyOn(Storage.prototype, 'removeItem').mockImplementation(deny);
    try {
      expect(readRecent()).toEqual([]);
      expect(() => recordRead(entry(8, 55))).not.toThrow();
      expect(() => forgetRead(8, 55)).not.toThrow();
    } finally {
      getItem.mockRestore();
      setItem.mockRestore();
      removeItem.mockRestore();
    }
  });

  it('свежая глава встаёт первой, прежние остаются', () => {
    recordRead(entry(3, 10));
    recordRead(entry(8, 55));
    expect(places()).toEqual([
      [8, 55],
      [3, 10],
    ]);
  });

  // Одна строка на главу: вернувшийся к главе поднимает её наверх с новой
  // страницей, а не заводит вторую.
  it('повторное чтение главы поднимает её строку, а не дублирует', () => {
    recordRead(entry(3, 10, 100));
    recordRead(entry(8, 55));
    recordRead(entry(3, 10, 140));
    expect(places()).toEqual([
      [3, 10],
      [8, 55],
    ]);
    expect(readRecent()[0]?.pageNumber).toBe(140);
  });

  // Глава одного id в двух томах — разные места: ключ — пара, а не глава.
  it('различает главы по паре «том, глава»', () => {
    recordRead(entry(3, 10));
    recordRead(entry(4, 10));
    expect(places()).toEqual([
      [4, 10],
      [3, 10],
    ]);
  });

  it(`держит не больше ${RECENT_LIMIT} строк, вытесняя самую давнюю`, () => {
    for (let c = 1; c <= RECENT_LIMIT + 1; c++) recordRead(entry(1, c));
    const kept = places();
    expect(kept).toHaveLength(RECENT_LIMIT);
    expect(kept[0]).toEqual([1, RECENT_LIMIT + 1]);
    expect(kept).not.toContainEqual([1, 1]);
  });

  it('убирает ровно названную главу', () => {
    recordRead(entry(3, 10));
    recordRead(entry(4, 10));
    recordRead(entry(3, 11));
    forgetRead(3, 10);
    expect(places()).toEqual([
      [3, 11],
      [4, 10],
    ]);
  });

  describe('перенос прежней единственной записи', () => {
    // До списка место хранилось одной записью: у читателя, пришедшего после
    // выкатки, «Продолжить» не должно пропасть.
    it('старая запись читается первой строкой списка', () => {
      localStorage.setItem(LEGACY_LAST_READ_KEY, JSON.stringify(entry(8, 55, 214)));
      expect(places()).toEqual([[8, 55]]);
    });

    it('первая запись в список сохраняет старую и снимает её ключ', () => {
      localStorage.setItem(LEGACY_LAST_READ_KEY, JSON.stringify(entry(8, 55, 214)));
      recordRead(entry(3, 10));
      expect(places()).toEqual([
        [3, 10],
        [8, 55],
      ]);
      expect(localStorage.getItem(LEGACY_LAST_READ_KEY)).toBeNull();
    });

    // Крестик у перенесённой строки обязан её убрать, а не оставить
    // воскресать из старого ключа при следующем чтении.
    it('крестик у перенесённой строки не даёт ей воскреснуть', () => {
      localStorage.setItem(LEGACY_LAST_READ_KEY, JSON.stringify(entry(8, 55, 214)));
      forgetRead(8, 55);
      expect(readRecent()).toEqual([]);
    });

    it('при живом списке старую запись не смотрит', () => {
      localStorage.setItem(RECENT_KEY, JSON.stringify([entry(3, 10)]));
      localStorage.setItem(LEGACY_LAST_READ_KEY, JSON.stringify(entry(8, 55, 214)));
      expect(places()).toEqual([[3, 10]]);
    });

    it('испорченная старая запись не мешает', () => {
      localStorage.setItem(LEGACY_LAST_READ_KEY, 'не json');
      expect(readRecent()).toEqual([]);
    });
  });
});

// Видимость вкладки в jsdom всегда «visible»: тест подменяет её сам и
// сообщает о смене событием, как браузер.
let visibility: DocumentVisibilityState = 'visible';
function setVisibility(next: DocumentVisibilityState) {
  visibility = next;
  document.dispatchEvent(new Event('visibilitychange'));
}

describe('useRecordRead: порог в минуту', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useFakeTimers();
    visibility = 'visible';
    vi.spyOn(document, 'visibilityState', 'get').mockImplementation(() => visibility);
  });
  afterEach(() => {
    vi.useRealTimers();
    vi.restoreAllMocks();
  });

  const place = (workId: number, chapterId: number, pageNumber: number | null = 10): ReadPlace => {
    const { ts: _ts, ...rest } = entry(workId, chapterId, pageNumber);
    return rest;
  };

  const advance = (ms: number) =>
    act(() => {
      vi.advanceTimersByTime(ms);
    });

  it('новую главу не заводит раньше минуты', () => {
    renderHook(() => useRecordRead(place(3, 10)));
    advance(DWELL_MS - 1000);
    expect(readRecent()).toEqual([]);
  });

  it('через минуту заводит строку с текущей страницей', () => {
    const { rerender } = renderHook(({ p }) => useRecordRead(p), {
      initialProps: { p: place(3, 10, 100) },
    });
    advance(30_000);
    rerender({ p: place(3, 10, 104) });
    advance(DWELL_MS - 30_000);
    expect(places()).toEqual([[3, 10]]);
    expect(readRecent()[0]?.pageNumber).toBe(104);
  });

  // Глава, открытая в фоновой вкладке и забытая, чтением не считается.
  it('время скрытой вкладки не считает', () => {
    renderHook(() => useRecordRead(place(3, 10)));
    advance(30_000);
    setVisibility('hidden');
    advance(10 * DWELL_MS);
    expect(readRecent()).toEqual([]);
    setVisibility('visible');
    advance(DWELL_MS - 30_000 - 1000);
    expect(readRecent()).toEqual([]);
    advance(1000);
    expect(places()).toEqual([[3, 10]]);
  });

  it('открытая в скрытой вкладке глава ждёт, пока её покажут', () => {
    visibility = 'hidden';
    renderHook(() => useRecordRead(place(3, 10)));
    advance(2 * DWELL_MS);
    expect(readRecent()).toEqual([]);
    setVisibility('visible');
    advance(DWELL_MS);
    expect(places()).toEqual([[3, 10]]);
  });

  // Минута — за одно открытие главы: смена главы начинает счёт заново, и
  // накопленное прежней не достаётся новой.
  it('смена главы начинает счёт заново', () => {
    const { rerender } = renderHook(({ p }) => useRecordRead(p), {
      initialProps: { p: place(3, 10) },
    });
    advance(40_000);
    rerender({ p: place(3, 11) });
    advance(40_000);
    expect(readRecent()).toEqual([]);
    advance(DWELL_MS - 40_000);
    expect(places()).toEqual([[3, 11]]);
  });

  // Порог нужен только чтобы завести строку: у главы, которая уже в списке,
  // место обновляется сразу.
  it('уже известную главу поднимает сразу', () => {
    recordRead(entry(3, 10, 100));
    recordRead(entry(8, 55));
    renderHook(() => useRecordRead(place(3, 10, 120)));
    expect(places()).toEqual([
      [3, 10],
      [8, 55],
    ]);
    expect(readRecent()[0]?.pageNumber).toBe(120);
  });

  it('после минуты каждая новая страница пишется сразу', () => {
    const { rerender } = renderHook(({ p }) => useRecordRead(p), {
      initialProps: { p: place(3, 10, 100) },
    });
    advance(DWELL_MS);
    rerender({ p: place(3, 10, 101) });
    expect(readRecent()[0]?.pageNumber).toBe(101);
  });

  // Разрешение писать без порога принадлежит одной главе на одно открытие.
  // Снятая крестиком (в другой вкладке) глава, к которой вернулись, снова
  // ждёт минуту, а не возвращается в список первым же кадром.
  it('снятая и открытая заново глава снова ждёт минуту', () => {
    const { rerender } = renderHook(({ p }) => useRecordRead(p), {
      initialProps: { p: place(3, 10) },
    });
    advance(DWELL_MS);
    rerender({ p: place(3, 11) });
    forgetRead(3, 10);
    rerender({ p: place(3, 10) });
    expect(readRecent()).toEqual([]);
  });

  it('без места не пишет ничего', () => {
    renderHook(() => useRecordRead(null));
    advance(2 * DWELL_MS);
    expect(localStorage.getItem(RECENT_KEY)).toBeNull();
  });

  // Размонтирование до минуты (читатель ушёл со страницы) не должно
  // оставить таймер, который заведёт строку уже после ухода.
  it('ушедший до минуты не оставляет отложенной записи', () => {
    const { unmount } = renderHook(() => useRecordRead(place(3, 10)));
    advance(30_000);
    unmount();
    advance(2 * DWELL_MS);
    expect(readRecent()).toEqual([]);
  });
});

// Появился как заплатка от реального бага: чтение элемента подборки писало
// общую запись «последнее прочитанное» и ломало виджет «Продолжить» на
// главной, потому что chapterId у элемента подборки — id строки
// collection_items, а не id главы. Дефолт trackLastRead=true держится одним
// словом в деструктуризации хука — легко потерять при рефакторинге.
describe('useReadingProgress: trackLastRead', () => {
  beforeEach(() => {
    localStorage.clear();
    vi.useFakeTimers();
    // Хук ставит окно в начало главы; jsdom прокрутку не реализует и пишет об
    // этом в stderr на каждый вызов.
    window.scrollTo = vi.fn();
  });
  afterEach(() => vi.useRealTimers());

  // Хук смотрит на якорь адреса (им распоряжается useHashAnchor, а не он),
  // поэтому живёт внутри маршрутизатора — как и оба его потребителя.
  const wrapper = ({ children }: { children: ReactNode }) =>
    createElement(MemoryRouter, null, children);

  const options = {
    workId: 8,
    chapterId: 55,
    workTitle: 'Том 8',
    chapterTitle: 'Восемнадцатое брюмера',
    pageNumber: 214,
    ready: true,
  };

  it('trackLastRead=false не пишет список, но пишет позицию прокрутки по своему ключу', () => {
    renderHook(() => useReadingProgress({ ...options, trackLastRead: false }), { wrapper });
    // Запись позиции идёт из onScroll через requestAnimationFrame, список —
    // через минуту: подождать и то и другое.
    act(() => {
      vi.advanceTimersByTime(2 * DWELL_MS);
    });

    expect(readRecent()).toEqual([]);
    expect(localStorage.getItem(storageKey(8, 55))).not.toBeNull();
  });

  it('по умолчанию (trackLastRead не задан) глава попадает в список', () => {
    renderHook(() => useReadingProgress(options), { wrapper });
    act(() => {
      vi.advanceTimersByTime(DWELL_MS);
    });

    expect(places()).toEqual([[8, 55]]);
  });

  it('пока текст не готов, минута не идёт', () => {
    const { rerender } = renderHook(({ ready }) => useReadingProgress({ ...options, ready }), {
      wrapper,
      initialProps: { ready: false },
    });
    act(() => {
      vi.advanceTimersByTime(2 * DWELL_MS);
    });
    expect(readRecent()).toEqual([]);
    rerender({ ready: true });
    act(() => {
      vi.advanceTimersByTime(DWELL_MS);
    });
    expect(places()).toEqual([[8, 55]]);
  });
});
