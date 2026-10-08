import { describe, it, expect } from 'vitest';
import type { AudioQueueItem, AudioRecording, AudioTrack, Chapter } from '../types';
import {
  chapterAudioState,
  companionTitles,
  downloadHref,
  formatClock,
  formatRate,
  formatDuration,
  recordingContentType,
  recordingsForChapter,
  trackCaption,
  tracksForRange,
} from './audio';

function track(
  id: number,
  start: number,
  end: number,
  extra: Partial<AudioTrack> = {},
): AudioTrack {
  return {
    id,
    work_id: 4,
    title: `дорожка ${id}`,
    start_page: start,
    end_page: end,
    duration_ms: 1000,
    bytes: 10,
    recipe_sha256: 'r',
    pages_sha256: 'p',
    stale: false,
    created_at: '',
    url: `/api/audio/${id}.opus`,
    ...extra,
  };
}

function chapter(
  id: number,
  start: number,
  end: number,
  children: Chapter[] = [],
  extra: Partial<Chapter> = {},
): Chapter {
  return {
    id,
    work_id: 4,
    title: `Глава ${id}`,
    type: 'chapter',
    order_number: id,
    start_page: start,
    end_page: end,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    children,
    ...extra,
  };
}

function rec(id: number, chapterId: number, position: number): AudioRecording {
  return {
    id,
    work_id: 4,
    chapter_id: chapterId,
    chapter_title: `Глава ${chapterId}`,
    position,
    reader: '',
    content_type: 'audio/mpeg',
    bytes: 10,
    duration_ms: 1000,
    created_at: '',
    url: `/api/audio/rec/${id}`,
  };
}

function item(
  id: number,
  chapterId: number | null,
  status: AudioQueueItem['status'],
): AudioQueueItem {
  return {
    id,
    work_id: 4,
    work_title: 'Том',
    chapter_id: chapterId,
    chapter_title: '',
    status,
    error: status === 'ошибка' ? 'упало' : '',
    status_counts: {},
    requested_by: '',
    requested_at: '',
    claimed_at: null,
    finished_at: null,
  };
}

describe('tracksForRange', () => {
  it('берёт пересекающие диапазон, по началу', () => {
    const got = tracksForRange([track(3, 30, 40), track(1, 1, 12), track(2, 13, 29)], 10, 30);
    expect(got.map((t) => t.id)).toEqual([1, 2, 3]);
  });

  it('касание краем — пересечение, соседняя без общих полос — нет', () => {
    expect(
      tracksForRange([track(1, 1, 9), track(2, 10, 10), track(3, 11, 20)], 10, 10).map((t) => t.id),
    ).toEqual([2]);
  });
});

describe('recordingsForChapter', () => {
  // Спека: запись попадает в главу, если её глава — сама глава или подглава;
  // порядок — родитель раньше детей (как плейлист, тикет 03), внутри — position.
  it('берёт главу и подглавы, родитель первым, внутри по position', () => {
    const sub = chapter(5, 1, 5);
    const outer = chapter(9, 1, 10, [sub]);
    const recs = [rec(1, 5, 1), rec(2, 9, 2), rec(3, 7, 1), rec(4, 9, 1), rec(5, 5, 2)];
    expect(recordingsForChapter(recs, outer).map((r) => r.id)).toEqual([4, 2, 1, 5]);
  });
});

describe('companionTitles', () => {
  it('называет соседей, чьи полосы озвучены той же дорожкой', () => {
    const sub = chapter(3, 5, 6);
    const own = chapter(2, 5, 8, [sub]);
    const prev = chapter(1, 1, 4);
    const next = chapter(4, 9, 12);
    const all = [prev, own, next];
    expect(companionTitles(track(1, 3, 10), own, all)).toEqual(['Глава 1', 'Глава 4']);
    expect(companionTitles(track(1, 5, 8), own, all)).toEqual([]);
  });

  it('предка не называет, а спускается к его детям; аппарат молчит', () => {
    const own = chapter(2, 1, 5);
    const sister = chapter(3, 6, 9);
    const parent = chapter(1, 1, 9, [own, sister]);
    const notes = chapter(4, 9, 12, [], { is_apparatus: true });
    expect(companionTitles(track(1, 1, 12), own, [parent, notes])).toEqual(['Глава 3']);
  });
});

// Тикет 07, п. 8: короткий кусок главы прилипает к следующей и берёт её
// заголовок (tools/tts/tracks.py, build). Ключ дорожки несёт title8, поэтому
// заголовок не трогаем — в панели главы подписываем её саму, а соседа
// оставляем в «вместе с».
describe('trackCaption', () => {
  // Склейка вперёд: короткий кусок главы 2 (5—8) прилип к главе 4 (9—12).
  const own = chapter(2, 5, 8);
  const next = chapter(4, 9, 12);
  const all = [own, next];

  it('дорожка названа по соседу — подпись по открытой главе', () => {
    expect(trackCaption(track(1, 5, 12, { title: 'Глава 4' }), own, all)).toBe('Глава 2');
  });

  it('дорожка названа не по соседу — подпись её собственная', () => {
    expect(trackCaption(track(1, 5, 12, { title: 'Отдел' }), own, all)).toBe('Отдел');
  });

  it('часть склеенной дорожки — номер части сохраняется', () => {
    // worker пишет «{title.rstrip('.')}. Часть …»; у главы точка на конце бывает.
    const dottedNext = { ...next, title: 'Глава 4.' };
    expect(
      trackCaption(track(1, 5, 12, { title: 'Глава 4. Часть первая' }), own, [own, dottedNext]),
    ).toBe('Глава 2. Часть первая');
    const dotted = { ...own, title: 'Глава 2.' };
    expect(
      trackCaption(track(1, 5, 12, { title: 'Глава 4. Часть вторая' }), dotted, [dotted, next]),
    ).toBe('Глава 2. Часть вторая');
  });

  it('составное порядковое части — тоже часть', () => {
    // num2words(21, to='ordinal', gender='f') — «двадцать первая».
    expect(
      trackCaption(track(1, 5, 12, { title: 'Глава 4. Часть двадцать первая' }), own, all),
    ).toBe('Глава 2. Часть двадцать первая');
  });

  // Рецензия: соседняя глава, кончающаяся на первой полосе открытой, попадает
  // в её панель своей дорожкой — подписать её открытой главой было бы ложью.
  it('дорожка соседа через общую граничную полосу — подпись не меняется', () => {
    const prev = { ...chapter(1, 1, 10), title: 'P' };
    const open = { ...chapter(2, 10, 20), title: 'O' };
    expect(trackCaption(track(1, 1, 10, { title: 'P' }), open, [prev, open])).toBe('P');
    // Сосед из одной общей полосы: дорожка начинается там же, где открытая глава.
    const single = { ...chapter(1, 10, 10), title: 'P' };
    expect(trackCaption(track(1, 10, 10, { title: 'P' }), open, [single, open])).toBe('P');
  });

  it('шмуцтитул в панели самого отдела — подпись не меняется', () => {
    // Головная полоса отдела прилипла к первой подглаве; подглава — своё
    // поддерево отдела, «вместе с» её нет, подменять нечем.
    const sub = { ...chapter(2, 2, 10), title: 'Глава' };
    const section = { ...chapter(1, 1, 10, [sub]), title: 'Отдел' };
    expect(trackCaption(track(1, 1, 10, { title: 'Глава' }), section, [section])).toBe('Глава');
  });

  it('однофамилец дальше по тому не делает чужую дорожку склейкой', () => {
    // «Письмо» из одной общей полосы с открытой главой и ещё одно «Письмо»
    // позже — второе дорожку не задевает и свидетелем склейки не служит.
    const letter = { ...chapter(1, 10, 10), title: 'Письмо' };
    const open = { ...chapter(2, 10, 20), title: 'O' };
    const later = { ...chapter(3, 30, 31), title: 'Письмо' };
    expect(trackCaption(track(1, 10, 10, { title: 'Письмо' }), open, [letter, open, later])).toBe(
      'Письмо',
    );
  });

  it('дорожка начата в главе раньше открытой — подпись не меняется', () => {
    // Два коротких куска (1, 2) прилипли к третьей главе; в панели второй
    // главы первым звучит не она.
    const a = chapter(1, 1, 1);
    const b = chapter(2, 2, 2);
    const c = chapter(3, 3, 10);
    expect(trackCaption(track(1, 1, 10, { title: 'Глава 3' }), b, [a, b, c])).toBe('Глава 3');
  });
});

describe('formatClock', () => {
  it('минуты и часы с ведущими нулями', () => {
    expect(formatClock(0)).toBe('0:00');
    expect(formatClock(291.7)).toBe('4:51');
    expect(formatClock(3723)).toBe('1:02:03');
  });

  // До метаданных длительность — NaN, у потока — Infinity.
  it('не число — 0:00', () => {
    expect(formatClock(Number.NaN)).toBe('0:00');
    expect(formatClock(Number.POSITIVE_INFINITY)).toBe('0:00');
    expect(formatClock(-3)).toBe('0:00');
  });
});

describe('formatRate', () => {
  it('десятичная запятая', () => {
    expect(formatRate(1.25)).toBe('1,25×');
    expect(formatRate(2)).toBe('2×');
  });
});

describe('downloadHref', () => {
  it('дописывает download=1 к адресу без параметров и с ними', () => {
    expect(downloadHref('/api/audio/3.opus')).toBe('/api/audio/3.opus?download=1');
    expect(downloadHref('/api/audio/rec/7?v=2')).toBe('/api/audio/rec/7?v=2&download=1');
  });
});

describe('formatDuration', () => {
  it('секунды, минуты, часы', () => {
    expect(formatDuration(45_400)).toBe('45 с');
    expect(formatDuration(149_902)).toBe('2 мин 30 с');
    expect(formatDuration(3_725_000)).toBe('1 ч 02 мин');
  });
});

describe('chapterAudioState', () => {
  it('открытая заявка важнее упавшей и устаревшего звука', () => {
    const items = [item(1, 5, 'ошибка'), item(2, 5, 'в_очереди')];
    expect(chapterAudioState(items, 5, [track(1, 1, 2, { stale: true })])).toMatchObject({
      kind: 'queued',
      item: { id: 2 },
    });
    expect(chapterAudioState([item(3, 5, 'синтезируется')], 5, [])).toMatchObject({
      kind: 'running',
    });
  });

  it('упавшая — последняя из упавших этой главы', () => {
    const items = [item(1, 5, 'ошибка'), item(4, 6, 'ошибка'), item(7, 5, 'ошибка')];
    expect(chapterAudioState(items, 5, [])).toMatchObject({ kind: 'failed', item: { id: 7 } });
  });

  it('заявка на том и чужая глава не в счёт; устаревшая дорожка — stale; иначе idle', () => {
    const items = [item(1, null, 'в_очереди'), item(2, 6, 'в_очереди')];
    expect(chapterAudioState(items, 5, [track(1, 1, 2, { stale: true })])).toEqual({
      kind: 'stale',
    });
    expect(chapterAudioState(items, 5, [track(1, 1, 2)])).toEqual({ kind: 'idle' });
  });
});

describe('recordingContentType', () => {
  // Chrome отдаёт .m4a как audio/x-m4a, .flac как audio/x-flac — поэтому по
  // расширению, в закрытый список сервера (models.RecordingContentTypes).
  it('по расширению, без учёта регистра; чужое — null', () => {
    expect(recordingContentType('Глава 1.MP3')).toBe('audio/mpeg');
    expect(recordingContentType('a.m4a')).toBe('audio/mp4');
    expect(recordingContentType('a.ogg')).toBe('audio/ogg');
    expect(recordingContentType('a.opus')).toBe('audio/opus');
    expect(recordingContentType('a.flac')).toBe('audio/flac');
    expect(recordingContentType('a.wav')).toBeNull();
    expect(recordingContentType('mp3')).toBeNull();
  });
});
