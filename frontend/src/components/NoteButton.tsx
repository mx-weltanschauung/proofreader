import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { CreateMarginNote, MarginNote } from '../types';
import { marginNotesApi } from '../services/api';
import { useAuth } from '../hooks/useAuth';
import { readSelection, fullyInsideExcluded } from '../utils/quoteSelection';
import { normalizeQuote } from '../utils/quoteMatch';
import { quoteKey, quoteTail } from '../utils/quoteKey';
import { MarginNoteEditor } from './MarginNoteEditor';
import './MarginNotes.css';

export interface NoteButtonProps {
  contentRef: React.RefObject<HTMLElement>;
  workId: number;
  onCreated(n: MarginNote): void;
  className?: string;
}

/** Снимок цитаты в заметке — для человека, не для поиска места; длиннее не нужен. */
const QUOTE_TEXT_LIMIT = 1000;

/** Отступ всплывающего редактора от выделения и от краёв окна, px. */
const POPOVER_GAP = 8;

type Quote = Omit<Extract<CreateMarginNote, { target: 'quote' }>, 'body'>;

/**
 * Есть ли непустое выделение внутри root. Та же дешёвая проверка, что у
 * кнопки «Цитировать» (CiteButton.tsx, hasSelectionInside): полный разбор
 * выделения на каждое `selectionchange` заикал бы чтение длинной главы.
 */
function hasSelectionInside(root: HTMLElement | null): boolean {
  if (!root) return false;
  const selection = window.getSelection();
  if (!selection || selection.rangeCount === 0 || selection.isCollapsed) return false;
  const range = selection.getRangeAt(0);
  if (!root.contains(range.startContainer) || !root.contains(range.endContainer)) return false;
  if (fullyInsideExcluded(range)) return false;
  return normalizeQuote(range.toString()) !== '';
}

/** Обрезка по кодовым точкам: суррогатная пара пополам — битый знак в снимке. */
function clip(text: string): string {
  const chars = Array.from(text);
  if (chars.length <= QUOTE_TEXT_LIMIT) return text;
  return chars.slice(0, QUOTE_TEXT_LIMIT - 1).join('') + '…';
}

/** Якоря цитаты — тем же правилом, что у кнопки «Цитировать» (CiteButton.tsx). */
function quoteOfSelection(root: HTMLElement): Quote | null {
  // Маркер стыка полос в тексте заметки не нужен: снимок — для человека, а
  // номер полосы у заметки и так свой.
  const selected = readSelection(root, () => '');
  if (!selected) return null;
  const { pages, pageSpan } = selected;
  return {
    target: 'quote',
    // Полоса, где выделение НАЧАЛОСЬ, — та же, что у адреса цитаты.
    start_page: pages[0],
    end_page: pageSpan[pageSpan.length - 1],
    quote_key: quoteKey(selected.head, selected.firstPageText),
    quote_tail: quoteTail(selected.tail, selected.lastPageText),
    quote_text: clip(selected.text),
  };
}

interface Opened {
  quote: Quote;
  /**
   * Том, с которого снята цитата. Берётся в момент нажатия, а не из пропа
   * при записи: в чтении подборки соседний элемент — другой том, и поле,
   * открытое на одном, записало бы полосы и ключ его цитаты в другой.
   */
  workId: number;
  /** Прямоугольник выделения в координатах документа. */
  top: number;
  left: number;
}

/**
 * Кнопка «Заметка» в панели чтения: записать мысль к выделенной цитате.
 *
 * Видна только вошедшему — заметка личная, анониму записать её некуда.
 * Нажимается, пока внутри текста есть выделение; по нажатию выделение
 * разбирается так же, как у «Цитировать», и у выделения открывается поле
 * заметки. Якоря цитаты снимаются в момент нажатия: дальше фокус уходит в
 * поле, и выделения на странице уже нет.
 */
export const NoteButton: React.FC<NoteButtonProps> = ({
  contentRef,
  workId,
  onCreated,
  className,
}) => {
  const signedIn = useAuth((s) => s.user !== null);
  const [hasSelection, setHasSelection] = useState(false);
  const [opened, setOpened] = useState<Opened | null>(null);
  const popoverRef = useRef<HTMLDivElement>(null);

  useEffect(() => {
    const update = () => setHasSelection(hasSelectionInside(contentRef.current));
    document.addEventListener('selectionchange', update);
    update();
    return () => document.removeEventListener('selectionchange', update);
  }, [contentRef]);

  // Поле не должно вылезать за край окна: левый край подбирается по
  // настоящей ширине всплывающего окна, известной только после отрисовки.
  // Верх ставится сразу разметкой (style ниже), а не здесь: поле получает
  // фокус раньше этого эффекта, и окно, стоявшее в эту минуту в начале
  // документа, утянуло бы страницу наверх — замерено живым Chrome.
  useLayoutEffect(() => {
    const pop = popoverRef.current;
    if (!pop || !opened) return;
    const width = pop.offsetWidth;
    const min = window.scrollX + POPOVER_GAP;
    const max = window.scrollX + window.innerWidth - width - POPOVER_GAP;
    pop.style.left = `${Math.max(min, Math.min(opened.left, max))}px`;
  }, [opened]);

  if (!signedIn) return null;

  const onClick = () => {
    const root = contentRef.current;
    if (!root) return;
    const quote = quoteOfSelection(root);
    if (!quote) return;
    // У jsdom у Range нет геометрии; в браузере она есть всегда.
    const range = window.getSelection()?.getRangeAt(0);
    const rect =
      typeof range?.getBoundingClientRect === 'function' ? range.getBoundingClientRect() : null;
    setOpened({
      quote,
      workId,
      top: (rect?.bottom ?? 0) + window.scrollY + POPOVER_GAP,
      left: (rect?.left ?? 0) + window.scrollX,
    });
  };

  return (
    <>
      <button
        type="button"
        className={className ? `margin-note-button ${className}` : 'margin-note-button'}
        disabled={!hasSelection}
        aria-label="Заметка"
        // Гасится по той же причине, что у «Цитировать»: иначе нажатие снимает
        // выделение раньше, чем onClick успевает его прочитать.
        onPointerDown={(e) => e.preventDefault()}
        onClick={onClick}
      >
        <span className="toolbar-label-full">Заметка</span>
        <span className="toolbar-label-short">Заметка</span>
      </button>
      {opened &&
        createPortal(
          <div
            ref={popoverRef}
            className="margin-note-popover"
            style={{ top: opened.top, left: opened.left }}
            role="dialog"
            aria-label="Новая заметка"
          >
            <div className="margin-note-popover-quote">{opened.quote.quote_text}</div>
            <MarginNoteEditor
              save={(body) =>
                marginNotesApi.create(opened.workId, { ...opened.quote, body }).then((r) => r.data)
              }
              onSaved={(n) => {
                setOpened(null);
                window.getSelection()?.removeAllRanges();
                onCreated(n);
              }}
              onCancel={() => setOpened(null)}
            />
          </div>,
          document.body,
        )}
    </>
  );
};
