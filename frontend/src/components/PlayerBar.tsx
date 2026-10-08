import { useLayoutEffect, useRef } from 'react';
import { Link } from 'react-router-dom';
import { usePlayer } from '../audio/playerStore';
import { formatClock, formatDuration } from '../utils/audio';
import {
  CloseIcon,
  DownloadIcon,
  NextIcon,
  PauseIcon,
  PlayIcon,
  PrevIcon,
  SpinnerIcon,
} from './icons';
import { RateMenu } from './RateMenu';
import { TOP_FOCUS_ID } from './ScrollDock';
import './PlayerBar.css';

/** Полоса проигрывателя внизу экрана. Живёт в Layout — переходы по читальне
 *  её не снимают; видна, пока очередь не пуста. Свою высоту пишет в
 *  --player-bar-height: по нему текст получает отступ снизу, а «Наверх»
 *  поднимается над полосой. */
export function PlayerBar() {
  const item = usePlayer((s) => s.queue[s.index] ?? null);
  const status = usePlayer((s) => s.status);
  const position = usePlayer((s) => s.position);
  const duration = usePlayer((s) => s.duration);
  const hasPrev = usePlayer((s) => s.index > 0 || s.position > 0);
  const hasNext = usePlayer((s) => s.index < s.queue.length - 1);
  const toggle = usePlayer((s) => s.toggle);
  const seek = usePlayer((s) => s.seek);
  const next = usePlayer((s) => s.next);
  const prev = usePlayer((s) => s.prev);
  const close = usePlayer((s) => s.close);
  const retry = usePlayer((s) => s.retry);
  const ref = useRef<HTMLDivElement>(null);
  const playRef = useRef<HTMLButtonElement>(null);
  // ✕ убирает полосу вместе с кнопкой под фокусом, «Повторить» — саму себя:
  // без переноса фокус падал бы на <body>. Цель ✕ — <main>, как у «Наверх».
  const closeBar = () => {
    document.getElementById(TOP_FOCUS_ID)?.focus({ preventScroll: true });
    close();
  };
  const retryHere = () => {
    playRef.current?.focus();
    retry();
  };
  const visible = item !== null;

  useLayoutEffect(() => {
    const el = ref.current;
    if (!visible || !el) return;
    const root = document.documentElement;
    const apply = () => root.style.setProperty('--player-bar-height', `${el.offsetHeight}px`);
    apply();
    const ro = new ResizeObserver(apply);
    ro.observe(el);
    return () => {
      ro.disconnect();
      root.style.removeProperty('--player-bar-height');
    };
  }, [visible]);

  if (!item) return null;

  const busy = status === 'playing' || status === 'loading';
  const total = duration > 0 ? duration : item.durationMs / 1000;
  const at = Math.min(position, total);

  return (
    <div className="player-bar" ref={ref} role="region" aria-label="Проигрыватель">
      <input
        className="player-bar-seek"
        type="range"
        min={0}
        max={Math.max(total, 0)}
        step={1}
        value={at}
        aria-label="Перемотка"
        aria-valuetext={`${formatDuration(at * 1000)} из ${formatDuration(total * 1000)}`}
        onChange={(e) => seek(Number(e.target.value))}
      />
      <button
        type="button"
        className="player-bar-btn player-bar-prev"
        aria-label="Предыдущая"
        disabled={!hasPrev}
        onClick={prev}
      >
        <PrevIcon />
      </button>
      <button
        ref={playRef}
        type="button"
        className="player-bar-btn player-bar-play"
        aria-label={busy ? 'Пауза' : 'Слушать'}
        onClick={toggle}
      >
        {status === 'loading' ? (
          <SpinnerIcon size={22} />
        ) : busy ? (
          <PauseIcon size={22} />
        ) : (
          <PlayIcon size={22} />
        )}
      </button>
      <button
        type="button"
        className="player-bar-btn player-bar-next"
        aria-label="Следующая"
        disabled={!hasNext}
        onClick={next}
      >
        <NextIcon />
      </button>
      <div className="player-bar-info">
        <Link className="player-bar-title" to={item.href}>
          {item.title}
        </Link>
        {status === 'error' ? (
          <span className="player-bar-sub player-bar-error" role="alert">
            Не удалось загрузить{' '}
            <button type="button" className="player-bar-retry" onClick={retryHere}>
              Повторить
            </button>
          </span>
        ) : (
          <>
            <span className="player-bar-sub player-bar-subtitle">{item.subtitle}</span>
            <span className="player-bar-sub player-bar-clock">
              {formatClock(at)} / {formatClock(total)}
            </span>
          </>
        )}
      </div>
      <span className="player-bar-time player-bar-time-at">{formatClock(at)}</span>
      <span className="player-bar-time player-bar-time-total">{formatClock(total)}</span>
      <RateMenu />
      <a
        className="player-bar-btn player-bar-download"
        href={item.downloadUrl}
        download
        aria-label={`Скачать «${item.title}»`}
      >
        <DownloadIcon />
      </a>
      <button
        type="button"
        className="player-bar-btn player-bar-close"
        aria-label="Закрыть проигрыватель"
        onClick={closeBar}
      >
        <CloseIcon />
      </button>
    </div>
  );
}
