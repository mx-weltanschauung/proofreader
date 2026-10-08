import { usePlayer } from '../audio/playerStore';
import { savedPosition } from '../audio/positions';
import type { QueueItem } from '../audio/queue';
import { formatClock, formatDuration } from '../utils/audio';
import { DownloadIcon, PauseIcon, PlayIcon, SpinnerIcon } from './icons';
import './AudioRow.css';

interface Props {
  /** Ключ элемента очереди: 'track:3' | 'rec:1001'. */
  itemKey: string;
  /** Очередь, которую ставит ▶ этой строки. Строки в ней нет (браузер не
   *  сыграет формат) — кнопки ▶ нет, стрелка «скачать» остаётся. */
  queue: QueueItem[];
  label: React.ReactNode;
  /** Что назвать диктору после «Слушать:» / «Пауза:». */
  playLabel: string;
  durationMs: number;
  downloadUrl: string;
  downloadLabel: string;
  /** Пометки под названием («вместе с …», «текст поправлен…»). */
  children?: React.ReactNode;
}

/** Строка дорожки или записи: ▶, название, время, стрелка «скачать». Играет не
 *  сама — ставит очередь общему проигрывателю (полоса внизу экрана). */
export function AudioRow({
  itemKey,
  queue,
  label,
  playLabel,
  durationMs,
  downloadUrl,
  downloadLabel,
  children,
}: Props) {
  const index = queue.findIndex((q) => q.key === itemKey);
  // Примитивы, а не объект: строка, которая не играет, не перерисовывается на
  // каждом timeupdate.
  const isCurrent = usePlayer((s) => s.queue[s.index]?.key === itemKey);
  const status = usePlayer((s) => (s.queue[s.index]?.key === itemKey ? s.status : 'idle'));
  const position = usePlayer((s) => (s.queue[s.index]?.key === itemKey ? s.position : 0));
  const duration = usePlayer((s) => (s.queue[s.index]?.key === itemKey ? s.duration : 0));
  const playQueue = usePlayer((s) => s.playQueue);
  const toggle = usePlayer((s) => s.toggle);

  const total = isCurrent && duration > 0 ? duration : durationMs / 1000;
  const at = isCurrent ? position : (savedPosition(itemKey) ?? 0);
  const busy = status === 'playing' || status === 'loading';

  return (
    <li className={isCurrent ? 'audio-row audio-row--current' : 'audio-row'}>
      {index >= 0 ? (
        <button
          type="button"
          className="audio-row-play"
          aria-label={`${busy ? 'Пауза' : 'Слушать'}: ${playLabel}`}
          onClick={() => (isCurrent ? toggle() : playQueue(queue, index))}
        >
          {status === 'loading' ? (
            <SpinnerIcon size={16} />
          ) : busy ? (
            <PauseIcon size={16} />
          ) : (
            <PlayIcon size={16} />
          )}
        </button>
      ) : (
        <span className="audio-row-play-gap" aria-hidden="true" />
      )}
      <div className="audio-row-body">
        <div className="audio-row-label">{label}</div>
        {children}
        {at > 0 && (
          <div className="audio-row-progress" aria-hidden="true">
            <span style={{ width: `${Math.min(100, (at / total) * 100)}%` }} />
          </div>
        )}
      </div>
      <span className="audio-row-duration">
        {at > 0 ? `${formatClock(at)} / ${formatClock(total)}` : formatDuration(durationMs)}
      </span>
      <a className="audio-row-download" href={downloadUrl} download aria-label={downloadLabel}>
        <DownloadIcon size={18} />
      </a>
    </li>
  );
}
