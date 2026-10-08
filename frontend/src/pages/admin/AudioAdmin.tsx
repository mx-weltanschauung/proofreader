import { useCallback, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { audioApi } from '../../services/api';
import type { AudioQueue, AudioQueueItem } from '../../types';
import { apiErrorMessage, apiErrorStatus } from '../../utils/apiError';
import './AudioAdmin.css';

const TIME = new Intl.DateTimeFormat('ru-RU', {
  day: 'numeric',
  month: 'short',
  hour: '2-digit',
  minute: '2-digit',
});

function when(iso: string | null): string {
  if (!iso) return '';
  const d = new Date(iso);
  return Number.isNaN(d.getTime()) ? iso : TIME.format(d);
}

const isOpen = (i: AudioQueueItem) => i.status === 'в_очереди' || i.status === 'синтезируется';

/** У тома или главы упавшей заявки уже есть открытая — повтор упрётся в
 *  уникальный индекс audio_queue_open_key (work_id, coalesce(chapter_id, 0)),
 *  и сервер ответит тем же 409, что и на заявку, ушедшую из «ошибки». */
function hasOpenTwin(items: AudioQueueItem[], failed: AudioQueueItem): boolean {
  return items.some(
    (i) => isOpen(i) && i.work_id === failed.work_id && i.chapter_id === failed.chapter_id,
  );
}

function where(i: AudioQueueItem): string {
  return `${i.work_title} — ${i.chapter_id === null ? 'весь том' : i.chapter_title}`;
}

/** Очередь озвучки (спека, «/admin/audio»): ждущие и синтезируемые, ошибки
 *  текстом целиком, устаревшее по томам. Завершённые не показываются. */
export const AudioAdmin: React.FC = () => {
  const [queue, setQueue] = useState<AudioQueue | null>(null);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const response = await audioApi.queue();
      setQueue(response.data);
      setError('');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось прочитать очередь озвучки'));
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  const act = async (run: () => Promise<unknown>, fallback: string) => {
    try {
      await run();
      await load();
    } catch (err: unknown) {
      // 409 — список устарел: перечитанный сам покажет, что изменилось
      // (в том числе открытую заявку той же главы у упавшей).
      if (apiErrorStatus(err) === 409) {
        await load();
        toast.error('Заявка уже изменилась — список обновлён');
        return;
      }
      toast.error(apiErrorMessage(err, fallback));
    }
  };

  if (error) return <div className="error-state">{error}</div>;
  if (!queue) return <div className="loading-state">Загрузка…</div>;

  const open = queue.items.filter(isOpen);
  const failed = queue.items.filter((i) => i.status === 'ошибка');

  return (
    <div className="audio-admin">
      <h1>Озвучка</h1>

      <section aria-labelledby="audio-admin-queue">
        <h2 id="audio-admin-queue">Очередь</h2>
        {open.length === 0 ? (
          <p className="audio-admin-empty">Очередь пуста</p>
        ) : (
          <ul className="audio-admin-list">
            {open.map((i) => (
              <li key={i.id} className="audio-admin-row">
                <span>{where(i)}</span>
                <span className="audio-admin-meta">
                  {i.status === 'в_очереди'
                    ? `ждёт с ${when(i.requested_at)}`
                    : `синтезируется с ${when(i.claimed_at)}`}
                  {i.requested_by && `, поставил ${i.requested_by}`}
                </span>
                {i.status === 'в_очереди' && (
                  <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    onClick={() => act(() => audioApi.cancel(i.id), 'Не удалось снять заявку')}
                  >
                    снять
                  </button>
                )}
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-labelledby="audio-admin-errors">
        <h2 id="audio-admin-errors">Ошибки</h2>
        {failed.length === 0 ? (
          <p className="audio-admin-empty">Ошибок нет</p>
        ) : (
          <ul className="audio-admin-list">
            {failed.map((i) => (
              <li key={i.id} className="audio-admin-row">
                <span>{where(i)}</span>
                <pre className="audio-admin-error">{i.error}</pre>
                {hasOpenTwin(queue.items, i) ? (
                  <span className="audio-admin-meta">
                    У {i.chapter_id === null ? 'тома' : 'главы'} уже стоит открытая заявка —
                    повторять не нужно, эту можно снять
                  </span>
                ) : (
                  <button
                    type="button"
                    className="btn btn-secondary btn-sm"
                    onClick={() => act(() => audioApi.retry(i.id), 'Не удалось повторить')}
                  >
                    повторить
                  </button>
                )}
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => act(() => audioApi.cancel(i.id), 'Не удалось снять заявку')}
                >
                  снять
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>

      <section aria-labelledby="audio-admin-stale">
        <h2 id="audio-admin-stale">Устаревшее</h2>
        {queue.stale.length === 0 ? (
          <p className="audio-admin-empty">Устаревших дорожек нет</p>
        ) : (
          <ul className="audio-admin-list">
            {queue.stale.map((s) => (
              <li key={s.work_id} className="audio-admin-row">
                <span>
                  {s.work_title}: {s.stale}
                </span>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() =>
                    act(() => audioApi.requeueStale(s.work_id), 'Не удалось поставить заново')
                  }
                >
                  поставить заново
                </button>
              </li>
            ))}
          </ul>
        )}
      </section>
    </div>
  );
};
