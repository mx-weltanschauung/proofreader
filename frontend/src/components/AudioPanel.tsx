import { useMemo } from 'react';
import { Link } from 'react-router-dom';
import { recordingQueue, synthQueue } from '../audio/queue';
import { API_BASE_URL } from '../services/api';
import type { AudioRecording, AudioTrack, Chapter, Work, WorkAudio } from '../types';
import { OPUS_TYPE, canPlay, companionTitles, downloadHref, trackCaption } from '../utils/audio';
import { AudioRow } from './AudioRow';
import './AudioPanel.css';

interface Props {
  work: Pick<Work, 'id' | 'slug' | 'title'>;
  /** Звук всего тома: ▶ ставит в очередь дорожки тома, а не одной главы. */
  audio: WorkAudio;
  /** Узел дерева тома (с children). */
  chapter: Chapter;
  allChapters: Chapter[];
  /** Уже отобранные для главы: tracksForRange / recordingsForChapter. */
  tracks: AudioTrack[];
  recordings: AudioRecording[];
  /** Кнопки сотрудника (очередь, записи). */
  staffSlot?: React.ReactNode;
  /** Путь плейлиста главы (…/download?format=m3u) — ссылкой под отказом. */
  playlistHref: string;
}

export const UNSUPPORTED_TEXT =
  'Ваш браузер не воспроизводит этот формат. Скачайте плейлист и откройте его в VLC или другом плеере.';

export const AudioPanel: React.FC<Props> = ({
  work,
  audio,
  chapter,
  allChapters,
  tracks,
  recordings,
  staffSlot,
  playlistHref,
}) => {
  const opus = canPlay(OPUS_TYPE);
  const synth = useMemo(
    () => synthQueue(work, audio.tracks, allChapters),
    [work, audio.tracks, allChapters],
  );
  const human = useMemo(
    () => recordingQueue(work, audio.recordings, allChapters),
    [work, audio.recordings, allChapters],
  );
  // Отказ без ссылки заставлял бы искать плейлист в меню «Скачать».
  const unsupported = (
    <p className="audio-panel-unsupported">
      {UNSUPPORTED_TEXT}{' '}
      <a href={`${API_BASE_URL}${playlistHref}`} download>
        Плейлист .m3u
      </a>
    </p>
  );
  return (
    <div className="audio-panel">
      {recordings.length > 0 && (
        <section className="audio-panel-block" aria-labelledby="audio-human-title">
          <h3 id="audio-human-title" className="audio-panel-title">
            Запись человека
          </h3>
          {recordings.some((r) => !canPlay(r.content_type)) && unsupported}
          <ol className="audio-panel-list">
            {recordings.map((r) => {
              const where = r.chapter_id !== chapter.id ? `${r.chapter_title}. ` : '';
              const who = r.reader ? `Читает: ${r.reader}` : `Запись ${r.position}`;
              return (
                <AudioRow
                  key={r.id}
                  itemKey={`rec:${r.id}`}
                  queue={human}
                  label={`${where}${who}`}
                  playLabel={`запись ${r.position} «${r.chapter_title}»`}
                  durationMs={r.duration_ms}
                  downloadUrl={`${API_BASE_URL}${downloadHref(r.url)}`}
                  downloadLabel={`Скачать запись ${r.position} «${r.chapter_title}»`}
                />
              );
            })}
          </ol>
        </section>
      )}

      {tracks.length > 0 && (
        <section
          className="audio-panel-block"
          aria-label={recordings.length > 0 ? undefined : 'Синтезированная версия'}
          aria-labelledby={recordings.length > 0 ? 'audio-synth-title' : undefined}
        >
          {recordings.length > 0 && (
            <h3 id="audio-synth-title" className="audio-panel-title">
              Синтезированная версия
            </h3>
          )}
          {!opus && unsupported}
          <ol className="audio-panel-list">
            {tracks.map((t) => {
              const companions = companionTitles(t, chapter, allChapters);
              const caption = trackCaption(t, chapter, allChapters);
              return (
                <AudioRow
                  key={t.id}
                  itemKey={`track:${t.id}`}
                  queue={synth}
                  label={caption}
                  playLabel={caption}
                  durationMs={t.duration_ms}
                  downloadUrl={`${API_BASE_URL}${downloadHref(t.url)}`}
                  downloadLabel={`Скачать «${caption}»`}
                >
                  {companions.length > 0 && (
                    <p className="audio-panel-note">
                      вместе с {companions.map((c) => `«${c}»`).join(', ')}
                    </p>
                  )}
                  {t.stale && (
                    <p className="audio-panel-stale">
                      Текст главы поправлен после озвучки, запись может расходиться с ним
                    </p>
                  )}
                </AudioRow>
              );
            })}
          </ol>
          <p className="audio-panel-note">
            Озвучен основной текст; подстрочные и редакционные примечания в запись не вошли. Синтез
            речи — Silero. <Link to="/help#audio">Подробнее</Link>
          </p>
        </section>
      )}

      {staffSlot}
    </div>
  );
};
