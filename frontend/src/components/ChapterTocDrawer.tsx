import { memo, useEffect, useId, useMemo, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import { Link } from 'react-router-dom';
import type { Chapter } from '../types';
import { useDrawerChrome } from './useDrawerChrome';
import { useDrawerOverlap } from './useDrawerOverlap';
import { FeatureHint } from './FeatureHint';
import { scrollBehavior } from '../utils/motion';
import { isPlainClick } from '../utils/plainClick';
import { chapterPath } from '../utils/paths';
import './ChapterTocDrawer.css';

interface ChapterTocDrawerProps {
  /** Минимум для построения адреса — не обязательно полный Work. */
  work: { id: number; slug?: string };
  /** Поддерево открытой главы: её дети со своими детьми. */
  chapters: Chapter[];
  /** Прокрутка к подглаве внутри уже отрендеренной страницы главы. */
  onJump: (chapter: Chapter) => void;
  /**
   * Адрес подглавы для строки списка — якорь на её начало.
   *
   * Строка остаётся настоящей ссылкой ради контекстного меню («копировать
   * адрес ссылки») и модификаторов, но обычный клик обрабатывается своим
   * `onJump`: хэш при повторном нажатии тот же самый, и одной сменой адреса
   * второго прыжка к началу подглавы не сделать.
   */
  hrefFor: (chapter: Chapter) => string;
  /** Подглава, чей текст сейчас на экране; null — читатель вне подглав. */
  activeChapterId?: number | null;
}

interface Row {
  chapter: Chapter;
  depth: number;
}

/** Поддерево в порядке чтения, с глубиной для отступа. */
function rows(chapters: Chapter[], depth = 0): Row[] {
  return chapters.flatMap((chapter) => [
    { chapter, depth },
    ...rows(chapter.children ?? [], depth + 1),
  ]);
}

const ChapterTocDrawerInner: React.FC<ChapterTocDrawerProps> = ({
  work,
  chapters,
  onJump,
  hrefFor,
  activeChapterId,
}) => {
  const [open, setOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const panelRef = useRef<HTMLDivElement>(null);
  const activeRowRef = useRef<HTMLAnchorElement>(null);
  const titleId = useId();

  // Список подглав может опустеть, пока шторка была открыта (например, смена
  // маршрута кнопкой «Назад» браузера). Панель размонтируется через ранний
  // return ниже, но хуки выше него всё равно выполняются — без isOpen они
  // держали бы document-подписку с open=true, и первый Escape доставался бы
  // панели, которой уже нет, а не режиму чтения.
  const isOpen = open && chapters.length > 0;

  useDrawerOverlap(isOpen);

  const close = () => {
    setOpen(false);
    toggleRef.current?.focus();
  };

  useDrawerChrome({
    open: isOpen,
    panelRef,
    toggleRef,
    initialFocusRef: activeRowRef,
    onEscape: close,
    onOutsideClick: () => setOpen(false),
  });

  // Список открывается на том месте, где читатель находится. Только в момент
  // открытия: пока панель открыта, подсветка едет за прокруткой страницы, но
  // сам список прыгать под курсором не должен.
  useEffect(() => {
    if (!isOpen) return;
    const row = activeRowRef.current;
    if (!row) return;
    row.scrollIntoView({ behavior: scrollBehavior(), block: 'center' });
  }, [isOpen]);

  const tocRows = useMemo(() => rows(chapters), [chapters]);

  // У листовой главы оглавлению нечего показывать — кнопки тоже быть не должно.
  if (chapters.length === 0) return null;

  const jump = (chapter: Chapter, event: React.MouseEvent) => {
    if (!isPlainClick(event)) return;
    event.preventDefault();
    setOpen(false);
    onJump(chapter);
  };

  return (
    <div className="chapter-toc-drawer">
      <button
        ref={toggleRef}
        type="button"
        className="btn btn-secondary"
        aria-expanded={isOpen}
        onClick={() => setOpen((o) => !o)}
        aria-label="Оглавление главы"
      >
        {/* Короткая подпись на телефоне; полное имя держит aria-label — см.
            .toolbar-label-* в ReadingSurface.css. Заголовок самой шторки
            ниже остаётся полным: там места хватает. */}
        <span className="toolbar-label-full">Оглавление главы</span>
        <span className="toolbar-label-short">Оглавление</span>
      </button>
      <FeatureHint id="toc-drawer" anchorRef={toggleRef} />

      {/* В портал на body по той же причине, что и настройки чтения: липкая
          шапка создаёт свой контекст наложения, и панель внутри неё оказалась
          бы под полосой прогресса чтения. */}
      {isOpen &&
        createPortal(
          <>
            <div className="ctoc-scrim" aria-hidden="true" />
            <div
              ref={panelRef}
              className="ctoc-panel"
              role="dialog"
              aria-modal="true"
              aria-labelledby={titleId}
            >
              <div className="ctoc-head">
                <h2 className="ctoc-title" id={titleId}>
                  Оглавление главы
                </h2>
                <button type="button" className="ctoc-close" aria-label="Закрыть" onClick={close}>
                  <span aria-hidden="true">✕</span>
                </button>
              </div>

              <ul className="ctoc-list">
                {tocRows.map(({ chapter, depth }) => {
                  const isActive = chapter.id === activeChapterId;
                  return (
                    <li
                      key={chapter.id}
                      className={`ctoc-row${isActive ? ' is-active' : ''}`}
                      // Ограничение на четырёх уровнях: у одиннадцатиуровневой
                      // цепочки (том 3) отступ без предела съел бы 140px из
                      // 320px ширины панели.
                      style={{ paddingLeft: `${Math.min(depth, 4) * 14}px` }}
                    >
                      <a
                        href={hrefFor(chapter)}
                        className="ctoc-jump"
                        ref={isActive ? activeRowRef : undefined}
                        aria-current={isActive ? 'true' : undefined}
                        onClick={(event) => jump(chapter, event)}
                      >
                        <span className="ctoc-row-title">{chapter.title}</span>
                        <span className="ctoc-row-pages">
                          {chapter.start_page}–{chapter.end_page}
                        </span>
                      </a>
                      <Link
                        to={chapterPath(work, chapter)}
                        className="ctoc-open"
                        aria-label={`Открыть отдельно: ${chapter.title}`}
                        onClick={() => setOpen(false)}
                      >
                        <span aria-hidden="true">↗</span>
                      </Link>
                    </li>
                  );
                })}
              </ul>
            </div>
          </>,
          document.body,
        )}
    </div>
  );
};

ChapterTocDrawerInner.displayName = 'ChapterTocDrawer';

// Страница главы перерисовывается по ходу чтения: прогресс и номер видимой
// страницы меняются при прокрутке. Открытая шторка при этом реконсилила бы
// весь список подглав — в глубоких томах это сотни строк на каждое такое
// изменение. Все её пропсы стабильны по ссылке (work — состояние ChapterView,
// chapters и activeChapterId мемоизированы там же, onJump обёрнут в
// useCallback), поэтому memo действительно отсекает работу, а не сравнивает
// пропсы впустую.
export const ChapterTocDrawer = memo(ChapterTocDrawerInner);
