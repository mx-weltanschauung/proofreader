import { useEffect, useRef, useState } from 'react';
import { PLAYBACK_RATES, usePlayer } from '../audio/playerStore';
import { formatRate } from '../utils/audio';

/** Кнопка скорости в полосе: открывает столбик 2× … 0,75× над собой.
 *  Esc и клик мимо закрывают — тем же приёмом, что меню «Скачать». */
export function RateMenu() {
  const rate = usePlayer((s) => s.rate);
  const setRate = usePlayer((s) => s.setRate);
  const [open, setOpen] = useState(false);
  const root = useRef<HTMLDivElement>(null);
  const toggleRef = useRef<HTMLButtonElement>(null);
  // Пункт под фокусом уходит из DOM вместе со столбиком — без возврата фокус
  // упал бы на <body>. Клик мимо фокус не трогает: читатель ушёл туда сам.
  const closeToToggle = () => {
    setOpen(false);
    toggleRef.current?.focus();
  };

  useEffect(() => {
    if (!open) return;
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') {
        setOpen(false);
        toggleRef.current?.focus();
      }
    };
    const onDown = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };
    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onDown);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onDown);
    };
  }, [open]);

  return (
    <div className="rate-menu" ref={root}>
      <button
        ref={toggleRef}
        type="button"
        className="rate-menu-toggle"
        aria-haspopup="menu"
        aria-expanded={open}
        aria-label={`Скорость ${formatRate(rate)}`}
        onClick={() => setOpen((o) => !o)}
      >
        {formatRate(rate)}
      </button>
      {open && (
        <ul className="rate-menu-list" role="menu" aria-label="Скорость">
          {[...PLAYBACK_RATES].reverse().map((r) => (
            <li key={r} role="none">
              <button
                type="button"
                role="menuitemradio"
                aria-checked={r === rate}
                onClick={() => {
                  setRate(r);
                  closeToToggle();
                }}
              >
                {formatRate(r)}
              </button>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
}
