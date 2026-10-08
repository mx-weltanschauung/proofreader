import { useCallback, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { audioApi } from '../services/api';
import type { AudioQueueItem, AudioTrack, Chapter } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { chapterAudioState } from '../utils/audio';
import './AudioQueueControl.css';

interface Props {
  workId: number;
  chapter: Chapter;
  /** Глава внутри аппарата — по пути от корня, а не по флагу самой главы:
   *  признак ставится руками на корень, детям не проставляется, а worker
   *  наследует его вниз и отклоняет заявку подглавы (chapterInApparatus). */
  apparatus: boolean;
  /** Дорожки, пересекающие главу (tracksForRange) — для «устарела». */
  chapterTracks: AudioTrack[];
  /** Перечитать звук главы после действия. */
  onChanged: () => Promise<void>;
}

/** Кнопка сотрудника у главы. Состояние — из GET /audio/queue (открытые и
 *  упавшие заявки) и признака stale у дорожек главы. Аппарат не озвучивается
 *  — ему кнопки нет вовсе. */
export const AudioQueueControl: React.FC<Props> = ({
  workId,
  chapter,
  apparatus,
  chapterTracks,
  onChanged,
}) => {
  const [items, setItems] = useState<AudioQueueItem[] | null>(null);
  const [busy, setBusy] = useState(false);

  const load = useCallback(async () => {
    if (apparatus) return;
    try {
      const response = await audioApi.queue();
      setItems(response.data.items);
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось прочитать очередь озвучки'));
    }
  }, [apparatus]);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  const act = async (run: () => Promise<unknown>, fallback: string) => {
    setBusy(true);
    try {
      await run();
      await load();
      await onChanged();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, fallback));
    } finally {
      setBusy(false);
    }
  };

  if (apparatus || items === null) return null;
  const state = chapterAudioState(items, chapter.id, chapterTracks);
  const enqueue = () =>
    act(() => audioApi.enqueue(workId, chapter.id), 'Не удалось поставить главу в озвучку');

  switch (state.kind) {
    case 'queued':
      return (
        <span className="audio-queue-state">
          В очереди на озвучку{' '}
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={busy}
            onClick={() => act(() => audioApi.cancel(state.item.id), 'Не удалось снять заявку')}
          >
            снять
          </button>
        </span>
      );
    case 'running':
      return <span className="audio-queue-state">Озвучивается</span>;
    case 'failed':
      // «снять» — упавшая заявка не уходит сама (сервер держит «ошибку»
      // бессрочно), а повтор постоянного отказа падает снова. Причина — не в
      // title: на сенсорном экране его не увидеть; <details> открывается
      // касанием, а свёрнут потому, что worker пишет до 4000 знаков.
      return (
        <span className="audio-queue-state">
          <button
            type="button"
            className="btn btn-secondary"
            disabled={busy}
            onClick={() => act(() => audioApi.retry(state.item.id), 'Не удалось повторить озвучку')}
          >
            Озвучка не удалась — повторить
          </button>{' '}
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={busy}
            onClick={() => act(() => audioApi.cancel(state.item.id), 'Не удалось снять заявку')}
          >
            снять
          </button>{' '}
          <details className="audio-queue-why">
            <summary>почему?</summary>
            <pre className="audio-queue-error">{state.item.error}</pre>
          </details>
        </span>
      );
    case 'stale':
      return (
        <button type="button" className="btn btn-secondary" disabled={busy} onClick={enqueue}>
          Озвучка устарела — поставить заново
        </button>
      );
    default:
      return (
        <button type="button" className="btn btn-secondary" disabled={busy} onClick={enqueue}>
          Поставить в озвучку
        </button>
      );
  }
};
