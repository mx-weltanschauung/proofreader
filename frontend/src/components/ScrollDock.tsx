import React, { useCallback, useEffect, useRef, useState } from 'react';
import { useScrollDock } from '../contexts/scrollDockContext';
import { scrollBehavior } from '../utils/motion';
import { FeatureHint } from './FeatureHint';
import { isPlainClick } from '../utils/plainClick';
import './ScrollDock.css';

/**
 * Сколько экранов надо прокрутить, чтобы блок появился. Полтора: на одном
 * экране кнопка «наверх» бессмысленна, а мельтешение на коротких страницах
 * раздражает сильнее, чем отсутствие кнопки.
 */
export const SCROLL_THRESHOLD_SCREENS = 1.5;

/**
 * Куда уходит фокус после прыжка наверх. Кнопка исчезает вместе с прокруткой,
 * и без переноса фокус падает на `<body>`: следующий Tab начинает обход
 * документа заново, ничего об этом не сообщив. Цель — `<main>` из Layout: он
 * есть на любой странице и в режиме чтения тоже, в отличие от шапки сайта,
 * которая там скрыта насовсем.
 */
export const TOP_FOCUS_ID = 'main-content';

function jumpToTop(): void {
  // Сначала фокус, потом прокрутка — как в jumpToSubChapter: preventScroll
  // не даёт браузеру дёрнуть страницу к цели до плавной прокрутки.
  document.getElementById(TOP_FOCUS_ID)?.focus({ preventScroll: true });
  window.scrollTo({ top: 0, behavior: scrollBehavior() });
}

export const ScrollDock: React.FC = () => {
  const { action } = useScrollDock();
  const [visible, setVisible] = useState(false);
  // Элемент подглавы бывает и кнопкой, и ссылкой (см. ниже), поэтому реф
  // хранится через callback: один тип на оба варианта.
  const chapterRef = useRef<HTMLElement | null>(null);
  const setChapterRef = useCallback((el: HTMLElement | null) => {
    chapterRef.current = el;
  }, []);

  useEffect(() => {
    let frame = 0;
    const onScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        setVisible(window.scrollY > window.innerHeight * SCROLL_THRESHOLD_SCREENS);
      });
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => {
      window.removeEventListener('scroll', onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  if (!visible) return null;

  return (
    <div className="scroll-dock">
      {/* Подпись — только название подглавы, иначе она не влезет; доступное
          имя объясняет, куда ведёт. Ссылка вместо кнопки, когда у места есть
          адрес: читателю нужно уметь на него сослаться, а не только прыгнуть.
          Обычный клик всё равно уходит в onActivate — он умеет прокрутку и
          отступной путь; модификаторы оставлены браузеру. */}
      {action &&
        (action.href !== undefined ? (
          <a
            ref={setChapterRef}
            href={action.href}
            className="scroll-dock-button scroll-dock-chapter"
            aria-label={`В начало подглавы: ${action.label}`}
            onClick={(event) => {
              if (!isPlainClick(event)) return;
              event.preventDefault();
              action.onActivate();
            }}
          >
            <span aria-hidden="true" className="scroll-dock-arrow">
              ↑
            </span>
            <span className="scroll-dock-label">{action.label}</span>
          </a>
        ) : (
          <button
            ref={setChapterRef}
            type="button"
            className="scroll-dock-button scroll-dock-chapter"
            aria-label={`В начало подглавы: ${action.label}`}
            onClick={action.onActivate}
          >
            <span aria-hidden="true" className="scroll-dock-arrow">
              ↑
            </span>
            <span className="scroll-dock-label">{action.label}</span>
          </button>
        ))}
      {action && <FeatureHint id="scroll-dock-chapter" anchorRef={chapterRef} />}
      <button
        type="button"
        className="scroll-dock-button scroll-dock-top"
        // Явное имя, а не имя-из-содержимого: на узких экранах подпись именно
        // этой кнопки скрыта через display:none (подпись подглавы остаётся,
        // см. ScrollDock.css), а содержимое display:none-элемента не
        // участвует в accessible name — без aria-label кнопка осталась бы
        // безымянной для скринридера.
        aria-label="Наверх"
        onClick={jumpToTop}
      >
        <span aria-hidden="true" className="scroll-dock-arrow">
          ⇈
        </span>
        <span className="scroll-dock-label">Наверх</span>
      </button>
    </div>
  );
};
