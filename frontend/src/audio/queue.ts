import { API_BASE_URL } from '../services/api';
import type { AudioRecording, AudioTrack, Chapter, Work } from '../types';
import { OPUS_TYPE, canPlay, downloadHref, trackCaption } from '../utils/audio';
import { chapterInApparatus, flattenChapters } from '../utils/chapterTree';
import { chapterPath as chapterUrl, workPath } from '../utils/paths';

/** Элемент очереди проигрывателя — уже готовое для показа: полосе внизу не
 *  нужно знать про главы и тома. */
export interface QueueItem {
  /** 'track:3' | 'rec:1001' — ключ строки и памяти позиций. */
  key: string;
  url: string;
  downloadUrl: string;
  title: string;
  /** «Капитал, т. 1 · синтез» | «… · читает Иванов». */
  subtitle: string;
  /** Адрес главы, где звук начинается: название в полосе ведёт к тексту. */
  href: string;
  durationMs: number;
}

type QueueWork = Pick<Work, 'id' | 'slug' | 'title'>;

/** Глава, где начинается звук с этой полосы: из накрывающих полосу
 *  не-аппаратных — начатая позже всех (на стыковой полосе дорожка начинает
 *  главу, которая с неё начинается, какой бы короткой ни была кончающаяся),
 *  затем самая узкая, затем последняя в порядке чтения (подглава того же
 *  диапазона глубже родителя). */
export function chapterForPage(chapters: Chapter[], page: number): Chapter | null {
  let best: Chapter | null = null;
  for (const c of flattenChapters(chapters)) {
    if (c.start_page > page || c.end_page < page) continue;
    if (chapterInApparatus(chapters, c)) continue;
    if (
      !best ||
      c.start_page > best.start_page ||
      (c.start_page === best.start_page &&
        c.end_page - c.start_page <= best.end_page - best.start_page)
    ) {
      best = c;
    }
  }
  return best;
}

/** Все дорожки синтеза тома по порядку полос. Браузер без Ogg Opus — пусто:
 *  ставить в очередь то, что он не сыграет, нельзя. */
export function synthQueue(
  work: QueueWork,
  tracks: AudioTrack[],
  chapters: Chapter[],
): QueueItem[] {
  if (!canPlay(OPUS_TYPE)) return [];
  return [...tracks]
    .sort((a, b) => a.start_page - b.start_page || a.id - b.id)
    .map((t) => {
      const ch = chapterForPage(chapters, t.start_page);
      return {
        key: `track:${t.id}`,
        url: `${API_BASE_URL}${t.url}`,
        downloadUrl: `${API_BASE_URL}${downloadHref(t.url)}`,
        title: ch ? trackCaption(t, ch, chapters) : t.title,
        subtitle: `${work.title} · синтез`,
        href: ch ? chapterUrl(work, ch) : workPath(work),
        durationMs: t.duration_ms,
      };
    });
}

/** Записи человека тома в порядке плейлиста: глава за главой в порядке чтения,
 *  внутри — по position. Неиграемые этим браузером отсеиваются. */
export function recordingQueue(
  work: QueueWork,
  recordings: AudioRecording[],
  chapters: Chapter[],
): QueueItem[] {
  const flat = flattenChapters(chapters);
  const order = new Map(flat.map((c, i) => [c.id, i]));
  const byId = new Map(flat.map((c) => [c.id, c]));
  const at = (r: AudioRecording) => order.get(r.chapter_id) ?? Number.MAX_SAFE_INTEGER;
  return recordings
    .filter((r) => canPlay(r.content_type))
    .sort((a, b) => at(a) - at(b) || a.position - b.position)
    .map((r) => {
      const ch = byId.get(r.chapter_id);
      return {
        key: `rec:${r.id}`,
        url: `${API_BASE_URL}${r.url}`,
        downloadUrl: `${API_BASE_URL}${downloadHref(r.url)}`,
        title: r.chapter_title,
        subtitle: r.reader
          ? `${work.title} · читает ${r.reader}`
          : `${work.title} · запись человека`,
        href: ch ? chapterUrl(work, ch) : workPath(work),
        durationMs: r.duration_ms,
      };
    });
}
