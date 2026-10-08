import type { AudioQueueItem, AudioRecording, AudioTrack, Chapter } from '../types';
import { chapterPath, flattenChapters } from './chapterTree';

/** Контейнер синтеза: Ogg Opus (спека, «Совместимость opus»). */
export const OPUS_TYPE = 'audio/ogg; codecs=opus';

/** Воспроизводит ли браузер этот тип. Запись .opus (audio/opus) спрашивается
 *  как Ogg Opus: Chrome отвечает '' на 'audio/opus', хотя такой файл играет. */
export function canPlay(type: string): boolean {
  const asked = type === 'audio/opus' ? OPUS_TYPE : type;
  return document.createElement('audio').canPlayType(asked) !== '';
}

/** Дорожки, у которых есть общие полосы с [start, end], по началу. */
export function tracksForRange(tracks: AudioTrack[], start: number, end: number): AudioTrack[] {
  return tracks
    .filter((t) => t.start_page <= end && t.end_page >= start)
    .sort((a, b) => a.start_page - b.start_page || a.id - b.id);
}

/** Записи человека главы и её подглав. chapter — узел дерева тома (с children).
 *  Порядок — тот же, что у плейлиста: глава за главой в порядке чтения
 *  (родитель раньше детей), внутри — по position. */
export function recordingsForChapter(recs: AudioRecording[], chapter: Chapter): AudioRecording[] {
  const order = new Map<number, number>();
  flattenChapters([chapter]).forEach((c, i) => order.set(c.id, i));
  return recs
    .filter((r) => order.has(r.chapter_id))
    .sort(
      (a, b) =>
        (order.get(a.chapter_id) ?? 0) - (order.get(b.chapter_id) ?? 0) || a.position - b.position,
    );
}

/** Главы вне открытой, чьи полосы озвучены той же дорожкой («вместе с „…“»).
 *  Предок открытой главы не называется — спуск идёт к его детям; из прочих
 *  берётся верхняя пересекающая глава, без её подглав. Аппарат не звучит —
 *  не называется. */
export function companionTitles(track: AudioTrack, chapter: Chapter, all: Chapter[]): string[] {
  const own = new Set(flattenChapters([chapter]).map((c) => c.id));
  const ancestors = new Set(chapterPath(all, chapter.id).map((c) => c.id));
  const out: string[] = [];
  const walk = (nodes: Chapter[]) => {
    for (const c of nodes) {
      if (own.has(c.id) || c.is_apparatus) continue;
      if (c.start_page > track.end_page || c.end_page < track.start_page) continue;
      if (ancestors.has(c.id)) {
        walk(c.children ?? []);
        continue;
      }
      out.push(c.title);
    }
  };
  walk(all);
  return out;
}

/** Подпись дорожки в панели главы. Короткий кусок главы прилипает к следующей
 *  и берёт её заголовок (tools/tts/tracks.py, build) — дорожка, начатая в
 *  открытой главе, названа соседом, и панель читалась «Сосед / вместе с
 *  «Сосед»». Заголовок дорожки не меняем: он входит в ключ (title8), и смена
 *  переозвучила бы лежащий звук. Подпись — открытой главой, только если это
 *  именно склейка вперёд: дорожка начата в открытой главе, а названа соседом
 *  «вместе с», начавшимся позже неё. Иначе в панель попадает и чужая дорожка
 *  через общую граничную полосу — её подпись открытой главой была бы ложью.
 *  Номер части worker дописывает как «{title.rstrip('.')}. Часть …»
 *  (порядковое бывает составным: «двадцать первая») — он сохраняется. */
export function trackCaption(track: AudioTrack, chapter: Chapter, all: Chapter[]): string {
  if (chapter.start_page > track.start_page) return track.title;
  const part = /\. (Часть [^.]+)$/.exec(track.title);
  const base = part ? track.title.slice(0, part.index) : track.title;
  const bare = (s: string) => s.trim().replace(/\.+$/, '');
  const named = (title: string) => bare(title) === bare(base);
  const gluedForward =
    companionTitles(track, chapter, all).some(named) &&
    flattenChapters(all).some(
      (c) => named(c.title) && c.start_page > track.start_page && c.start_page <= track.end_page,
    );
  if (!gluedForward) return track.title;
  return part ? `${bare(chapter.title)}. ${part[1]}` : chapter.title;
}

/** Адрес «скачать» у дорожки или записи: тот же редирект с ?download=1 —
 *  сервер подпишет ссылку с attachment. Атрибута download мало: звук лежит на
 *  чужом домене хранилища, и там браузер его игнорирует. */
export function downloadHref(url: string): string {
  return `${url}${url.includes('?') ? '&' : '?'}download=1`;
}

/** «45 с», «2 мин 30 с», «1 ч 02 мин». */
export function formatDuration(ms: number): string {
  const total = Math.round(ms / 1000);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = total % 60;
  if (h > 0) return `${h} ч ${String(m).padStart(2, '0')} мин`;
  if (m > 0) return `${m} мин ${String(s).padStart(2, '0')} с`;
  return `${s} с`;
}

/** Часы полосы: «4:51», «1:02:03». Не число (длительность до загрузки
 *  метаданных бывает NaN, у потока — Infinity) — «0:00». */
export function formatClock(seconds: number): string {
  if (!Number.isFinite(seconds) || seconds < 0) return '0:00';
  const total = Math.floor(seconds);
  const h = Math.floor(total / 3600);
  const m = Math.floor((total % 3600) / 60);
  const s = String(total % 60).padStart(2, '0');
  return h > 0 ? `${h}:${String(m).padStart(2, '0')}:${s}` : `${m}:${s}`;
}

/** «1,25×» — десятичная запятая, как в остальных текстах читальни. */
export function formatRate(rate: number): string {
  return `${String(rate).replace('.', ',')}×`;
}

/** Состояние озвучки главы для кнопки сотрудника. */
export type ChapterAudioState =
  | { kind: 'queued'; item: AudioQueueItem }
  | { kind: 'running'; item: AudioQueueItem }
  | { kind: 'failed'; item: AudioQueueItem }
  | { kind: 'stale' }
  | { kind: 'idle' };

/** items — ответ GET /audio/queue (по requested_at): открытая заявка главы
 *  важнее упавшей, упавшая — важнее устаревшего звука. Заявка на весь том
 *  (chapter_id === null) главе не принадлежит. */
export function chapterAudioState(
  items: AudioQueueItem[],
  chapterId: number,
  chapterTracks: AudioTrack[],
): ChapterAudioState {
  const mine = items.filter((i) => i.chapter_id === chapterId);
  const open = mine.find((i) => i.status === 'в_очереди' || i.status === 'синтезируется');
  if (open) return { kind: open.status === 'в_очереди' ? 'queued' : 'running', item: open };
  const failed = mine.filter((i) => i.status === 'ошибка');
  if (failed.length > 0) return { kind: 'failed', item: failed[failed.length - 1] };
  if (chapterTracks.some((t) => t.stale)) return { kind: 'stale' };
  return { kind: 'idle' };
}

/** Форматы записи человека — ровно models.RecordingContentTypes. По
 *  расширению: браузеры называют .m4a и .flac по-разному (audio/x-m4a…). */
const RECORDING_TYPES: Record<string, string> = {
  mp3: 'audio/mpeg',
  m4a: 'audio/mp4',
  ogg: 'audio/ogg',
  opus: 'audio/opus',
  flac: 'audio/flac',
};

export function recordingContentType(name: string): string | null {
  const dot = name.lastIndexOf('.');
  if (dot < 0) return null;
  return RECORDING_TYPES[name.slice(dot + 1).toLowerCase()] ?? null;
}
