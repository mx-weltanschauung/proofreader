import { useCallback, useEffect, useRef, useState } from 'react';
import type React from 'react';
import type { VolumeSummary } from '../types';

/**
 * Карточка тома по наведению: задержка показа, перемер при прокрутке и
 * молчание под пальцем. Вынесена из полки (`VolumeShelf`), чтобы любой
 * другой вид тех же томов брал её отсюда, а не заводил вторую копию.
 */

/**
 * Задержка перед показом. Без неё проход мышью по сорока пяти книгам даёт
 * стробоскоп; снятие идёт без задержки, чтобы карточка не тянулась за
 * указателем.
 */
export const PEEK_DELAY_MS = 120;

export interface Peek {
  volume: VolumeSummary;
  /** Книга, за которую карточка держится: по ней её и перемеряют. */
  el: HTMLElement;
  /** Прямоугольник книги в координатах окна, снятый последним замером. */
  anchor: DOMRect;
}

function sameBox(a: DOMRect, b: DOMRect): boolean {
  return a.top === b.top && a.left === b.left && a.bottom === b.bottom && a.right === b.right;
}

export function useVolumePeek() {
  const [peek, setPeek] = useState<Peek | null>(null);
  const timer = useRef<number | null>(null);
  /**
   * Чем нажали последним. Медиазапросом `(hover: none)` этого не решить: он
   * про устройство, а гибрид — ноутбук с тач-экраном — обязан показывать
   * карточку под мышью и молчать под пальцем. Та же идиома, что в
   * `useFootnotePreview` и `useNoteXrefs`, где нижний лист заменяет панель
   * ровно по этому признаку.
   */
  const pointerKind = useRef('');

  const clearTimer = () => {
    if (timer.current !== null) {
      window.clearTimeout(timer.current);
      timer.current = null;
    }
  };

  // Таймер, доживший до размонтирования, дёрнул бы setState на снятом
  // компоненте.
  useEffect(() => clearTimer, []);

  const handlePeek = useCallback((volume: VolumeSummary, target: HTMLElement | null) => {
    clearTimer();
    if (!target) {
      setPeek(null);
      return;
    }
    // У пальца наведения нет, а эмулированные мышиные события за тапом есть:
    // без разбора указателя карточка мигала бы под уже уезжающей страницей.
    // На тач-экране её нет вовсе — на узком экране том называет сама книга.
    if (pointerKind.current === 'touch' || pointerKind.current === 'pen') {
      setPeek(null);
      return;
    }
    // Прямоугольник снимается в момент показа, а не наведения: за время
    // задержки страница может уехать под указателем, и карточка встала бы по
    // устаревшим координатам.
    timer.current = window.setTimeout(
      () => setPeek({ volume, el: target, anchor: target.getBoundingClientRect() }),
      PEEK_DELAY_MS,
    );
  }, []);

  // Карточка стоит `position: fixed`, то есть в координатах окна, — а книга
  // едет вместе со страницей. Указатель при этом с книги не уходит
  // (mouseleave не приходит), так что снятый однажды прямоугольник устаревает
  // ровно на величину прокрутки, и карточка отрывается от книги вверх или
  // вниз. Пока карточка показана, прямоугольник перемеряется на каждой
  // прокрутке и смене размера окна.
  const anchorEl = peek?.el ?? null;
  useEffect(() => {
    if (!anchorEl) return;

    let frame = 0;
    const remeasure = () => {
      frame = 0;
      const box = anchorEl.getBoundingClientRect();
      setPeek((prev) =>
        prev && prev.el === anchorEl && !sameBox(prev.anchor, box)
          ? { ...prev, anchor: box }
          : prev,
      );
    };
    // Прокрутка мельче кадра ничего не меняет на экране; без склейки же на
    // каждое событие приходилась бы своя перерисовка карточки.
    const track = () => {
      if (frame === 0) frame = window.requestAnimationFrame(remeasure);
    };

    // Перехват: штабель едет не только окном — прокрутиться может любой
    // прокручиваемый предок, а такие события всплывать не обязаны.
    window.addEventListener('scroll', track, true);
    window.addEventListener('resize', track);
    return () => {
      if (frame !== 0) window.cancelAnimationFrame(frame);
      window.removeEventListener('scroll', track, true);
      window.removeEventListener('resize', track);
    };
  }, [anchorEl]);

  const pointerHandlers = {
    onPointerDown: (e: React.PointerEvent) => {
      pointerKind.current = e.pointerType;
    },
    // Жест, перехваченный браузером под прокрутку, эмулированных мышиных
    // событий за собой не ведёт — и вердикт пальца не должен пережить его и
    // достаться тому, кто придёт следом с клавиатуры. Тап, дошедший до
    // конца, уводит со страницы вместе со всем штабелем, и сбрасывать там
    // нечего.
    onPointerCancel: () => {
      pointerKind.current = '';
    },
  };

  return { peek, handlePeek, pointerHandlers };
}
