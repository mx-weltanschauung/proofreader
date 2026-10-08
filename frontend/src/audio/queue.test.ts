import { describe, it, expect, vi, afterEach } from 'vitest';
import type { AudioRecording, AudioTrack, Chapter } from '../types';
import { chapterForPage, recordingQueue, synthQueue } from './queue';

function ch(
  id: number,
  title: string,
  start: number,
  end: number,
  extra: Partial<Chapter> = {},
): Chapter {
  return {
    id,
    work_id: 4,
    title,
    slug: `g${id}`,
    type: 'chapter',
    order_number: id,
    start_page: start,
    end_page: end,
    is_apparatus: false,
    created_at: '',
    updated_at: '',
    children: [],
    ...extra,
  };
}

// Глава 1 (1—3) с подглавой 11 того же диапазона, глава 2 (4—6), аппарат (7—9).
const SUB = ch(11, 'Два фактора товара', 1, 3);
const CH1 = ch(1, 'Товар', 1, 3, { children: [SUB] });
const CH2 = ch(2, 'Меновая стоимость', 4, 6);
const APP = ch(3, 'Примечания', 7, 9, { is_apparatus: true });
const TREE = [CH1, CH2, APP];
const WORK = { id: 4, slug: 'kapital-t1', title: 'Капитал, т. 1' };

function track(id: number, title: string, start: number, end: number): AudioTrack {
  return {
    id,
    work_id: 4,
    title,
    start_page: start,
    end_page: end,
    duration_ms: 1000 * id,
    bytes: 1,
    recipe_sha256: 'r',
    pages_sha256: 'p',
    stale: false,
    created_at: '',
    url: `/api/audio/${id}.opus`,
  };
}

function rec(
  id: number,
  chapterId: number,
  position: number,
  reader = '',
  type = 'audio/mpeg',
): AudioRecording {
  return {
    id,
    work_id: 4,
    chapter_id: chapterId,
    chapter_title: chapterId === 2 ? 'Меновая стоимость' : 'Товар',
    position,
    reader,
    content_type: type,
    bytes: 1,
    duration_ms: 500,
    created_at: '',
    url: `/api/audio/rec/${id}`,
  };
}

afterEach(() => vi.restoreAllMocks());

describe('chapterForPage', () => {
  it('самая узкая глава; при равной ширине — подглава', () => {
    expect(chapterForPage(TREE, 2)?.id).toBe(11);
    expect(chapterForPage(TREE, 5)?.id).toBe(2);
  });

  it('аппарат не годится, полоса вне глав — null', () => {
    expect(chapterForPage(TREE, 8)).toBeNull();
    expect(chapterForPage(TREE, 40)).toBeNull();
  });

  it('стыковая полоса — глава, которая с неё начинается', () => {
    const a = ch(5, 'А', 1, 4);
    const b = ch(6, 'Б', 4, 7);
    expect(chapterForPage([a, b], 4)?.id).toBe(6);
  });

  // Рецензия ветки: короткая статья кончается на полосе, где начинается
  // длинная, — «самая узкая» отдавала короткую, и ссылка в полосе вела назад.
  it('стыковая полоса при разной ширине — всё равно глава, которая с неё начинается', () => {
    const shortOne = ch(5, 'Короткая', 1, 4);
    const longOne = ch(6, 'Длинная', 4, 20);
    expect(chapterForPage([shortOne, longOne], 4)?.id).toBe(6);
  });
});

describe('synthQueue', () => {
  it('дорожки тома по полосам: ключ, адреса, глава начала, подзаголовок', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const q = synthQueue(
      WORK,
      [track(7, 'Меновая стоимость', 4, 6), track(3, 'Товар', 1, 3)],
      TREE,
    );
    expect(q.map((i) => i.key)).toEqual(['track:3', 'track:7']);
    expect(q[0]).toEqual({
      key: 'track:3',
      url: '/api/audio/3.opus',
      downloadUrl: '/api/audio/3.opus?download=1',
      title: 'Товар',
      subtitle: 'Капитал, т. 1 · синтез',
      href: '/works/4-kapital-t1/chapters/11-g11',
      durationMs: 3000,
    });
    expect(q[1].href).toBe('/works/4-kapital-t1/chapters/2-g2');
  });

  // Короткий кусок прилипает к следующей главе и берёт её заголовок
  // (tools/tts/tracks.py) — в полосе он подписан главой, где начинается.
  it('склеенная вперёд дорожка подписана главой начала', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const [item] = synthQueue(WORK, [track(9, 'Меновая стоимость', 1, 5)], TREE);
    expect(item.title).toBe('Два фактора товара');
  });

  it('полоса вне глав — адрес тома и заголовок дорожки', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const [item] = synthQueue(WORK, [track(5, 'Колофон', 40, 41)], TREE);
    expect(item.href).toBe('/works/4-kapital-t1');
    expect(item.title).toBe('Колофон');
  });

  it('без Ogg Opus очередь пуста', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('');
    expect(synthQueue(WORK, [track(3, 'Товар', 1, 3)], TREE)).toEqual([]);
  });
});

describe('recordingQueue', () => {
  it('глава за главой в порядке чтения, внутри по position; неиграемое отсеяно', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockImplementation((t: string) =>
      t === 'audio/flac' ? '' : 'probably',
    );
    const q = recordingQueue(
      WORK,
      [
        rec(1, 2, 1),
        rec(2, 1, 2, 'Петров'),
        rec(3, 1, 1, 'Иванов'),
        rec(4, 1, 3, '', 'audio/flac'),
      ],
      TREE,
    );
    expect(q.map((i) => i.key)).toEqual(['rec:3', 'rec:2', 'rec:1']);
    expect(q[0]).toEqual({
      key: 'rec:3',
      url: '/api/audio/rec/3',
      downloadUrl: '/api/audio/rec/3?download=1',
      title: 'Товар',
      subtitle: 'Капитал, т. 1 · читает Иванов',
      href: '/works/4-kapital-t1/chapters/1-g1',
      durationMs: 500,
    });
    expect(q[2].subtitle).toBe('Капитал, т. 1 · запись человека');
  });

  it('глава записи пропала из дерева — в конец, адрес тома', () => {
    vi.spyOn(HTMLMediaElement.prototype, 'canPlayType').mockReturnValue('probably');
    const q = recordingQueue(WORK, [rec(8, 99, 1), rec(3, 1, 1)], TREE);
    expect(q.map((i) => i.key)).toEqual(['rec:3', 'rec:8']);
    expect(q[1].href).toBe('/works/4-kapital-t1');
  });
});
