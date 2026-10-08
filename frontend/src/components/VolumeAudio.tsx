import { useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import toast from 'react-hot-toast';
import { recordingQueue, synthQueue } from '../audio/queue';
import { API_BASE_URL, audioApi } from '../services/api';
import type { Chapter, Work, WorkAudio } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { OPUS_TYPE, canPlay, downloadHref } from '../utils/audio';
import { plural } from '../utils/volumeLabel';
import { UNSUPPORTED_TEXT } from './AudioPanel';
import { AudioRow } from './AudioRow';
import './AudioPanel.css';
import './VolumeAudio.css';

interface Props {
  work: Work;
  audio: WorkAudio;
  /** Дерево глав тома: по нему строка очереди находит свою главу. */
  chapters: Chapter[];
  editable: boolean;
  onChanged: () => Promise<void>;
}

/** Раздел «Аудио» тома: все дорожки и записи человека, плейлист, кнопки
 *  сотрудника. Нет звука и не сотрудник — раздела нет (решает WorkDetail). */
export const VolumeAudio: React.FC<Props> = ({ work, audio, chapters, editable, onChanged }) => {
  const [busy, setBusy] = useState(false);
  const staleCount = audio.tracks.filter((t) => t.stale).length;
  const opus = canPlay(OPUS_TYPE);
  const synth = useMemo(
    () => synthQueue(work, audio.tracks, chapters),
    [work, audio.tracks, chapters],
  );
  const human = useMemo(
    () => recordingQueue(work, audio.recordings, chapters),
    [work, audio.recordings, chapters],
  );

  const act = async (run: () => Promise<unknown>, done: string, fallback: string) => {
    setBusy(true);
    try {
      await run();
      toast.success(done);
      await onChanged();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, fallback));
    } finally {
      setBusy(false);
    }
  };

  return (
    <section className="audio-volume" aria-labelledby="audio-volume-title">
      <h2 id="audio-volume-title" className="audio-volume-title">
        Аудио
      </h2>

      {audio.recordings.length > 0 && (
        <ol className="audio-panel-list audio-volume-list">
          {audio.recordings.map((r) => (
            <AudioRow
              key={r.id}
              itemKey={`rec:${r.id}`}
              queue={human}
              label={
                <>
                  <span className="audio-volume-badge">запись человека</span> {r.chapter_title}
                  {r.reader && ` — читает ${r.reader}`}
                </>
              }
              playLabel={`запись ${r.position} «${r.chapter_title}»`}
              durationMs={r.duration_ms}
              downloadUrl={`${API_BASE_URL}${downloadHref(r.url)}`}
              downloadLabel={`Скачать запись ${r.position} «${r.chapter_title}»`}
            />
          ))}
        </ol>
      )}

      {audio.tracks.length > 0 && (
        <>
          {!opus && <p className="audio-volume-note">{UNSUPPORTED_TEXT}</p>}
          <ol className="audio-panel-list audio-volume-list">
            {audio.tracks.map((t) => (
              <AudioRow
                key={t.id}
                itemKey={`track:${t.id}`}
                queue={synth}
                label={t.title}
                playLabel={t.title}
                durationMs={t.duration_ms}
                downloadUrl={`${API_BASE_URL}${downloadHref(t.url)}`}
                downloadLabel={`Скачать «${t.title}»`}
              >
                {t.stale && (
                  <p className="audio-volume-stale">
                    Текст главы поправлен после озвучки, запись может расходиться с ним
                  </p>
                )}
              </AudioRow>
            ))}
          </ol>
        </>
      )}

      {(audio.tracks.length > 0 || audio.recordings.length > 0) && (
        <p className="audio-volume-note">
          <a href={`${API_BASE_URL}/api/works/${work.id}/download?format=m3u`} download>
            Плейлист .m3u
          </a>{' '}
          — для VLC и других плееров.
        </p>
      )}

      {audio.tracks.length > 0 && (
        <p className="audio-volume-note">
          Озвучен основной текст; подстрочные и редакционные примечания в запись не вошли. Синтез
          речи — Silero. <Link to="/help#audio">Подробнее</Link>
        </p>
      )}

      {editable && (
        <div className="audio-volume-actions">
          <button
            type="button"
            className="btn btn-secondary"
            disabled={busy}
            onClick={() =>
              act(
                () => audioApi.enqueue(work.id),
                'Том поставлен в озвучку',
                'Не удалось поставить том',
              )
            }
          >
            Поставить весь том
          </button>
          {staleCount > 0 && (
            <button
              type="button"
              className="btn btn-secondary"
              disabled={busy}
              onClick={() =>
                act(
                  () => audioApi.requeueStale(work.id),
                  'Устаревшее поставлено заново',
                  'Не удалось поставить заново',
                )
              }
            >
              Устарело {plural(staleCount, ['дорожка', 'дорожки', 'дорожек'])} — поставить заново
            </button>
          )}
        </div>
      )}
    </section>
  );
};
