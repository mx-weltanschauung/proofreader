import { useCallback, useEffect, useLayoutEffect, useRef, useState } from 'react';
import { createPortal } from 'react-dom';
import type { MarginNote } from '../types';
import { marginNotesApi } from '../services/api';
import { markQuotes, type MarkRequest } from '../utils/quoteMatch';
import { MarginNoteEditor } from './MarginNoteEditor';
import './MarginNotes.css';

export interface MarginNotesProps {
  /**
   * Контейнер полос: секции с data-page, текст полосы в .page-html-content.
   * Реф обязан быть стабильным (useRef): по нему слой перезапускается
   * целиком — с проходом по всему тексту главы и пересадкой карточек.
   */
  contentRef: React.RefObject<HTMLElement>;
  notes: MarginNote[];
  /** Полосы, нарисованные сейчас в контейнере: заметки остальных не рисуются. */
  pageNumbers: number[];
  /** Признак новой вёрстки полос — по его смене подсветка ставится заново. */
  deps: unknown[];
  onChange(n: MarginNote): void;
  onDelete(id: number): void;
}

/** Класс подсветки цитат заметок; его же снимает слой перед новым проходом. */
const HIT = 'margin-hit';
/** Узел, в который на узком экране встаёт карточка, — под абзацем с цитатой. */
const HOST = 'margin-note-host';

/** Поля сбоку — только на широком окне; уже — карточка под абзацем. */
const WIDE_QUERY = '(min-width: 1100px)';
/**
 * Самое узкое поле, в которое карточку ещё стоит ставить, px. Окно бывает
 * широким, а поля нет: мера «во всю ширину» растягивает колонку до краёв, —
 * тогда карточка уходит под абзац, как на телефоне.
 */
const MIN_SIDE = 160;
/** Зазор между колонкой и карточкой и от карточки до края, px. */
const SIDE_GAP = 24;
/** Ширина карточки на полях, px. */
const CARD_WIDTH = 288;
/** Зазор между соседними карточками, когда нижняя сдвинута, px. */
const STACK_GAP = 8;

type Mode = 'wide' | 'narrow';

/** Полоса заметки: живая, а у пропавшей — снимок номера. */
function pageOfNote(n: MarginNote): number | null {
  return n.start_page ?? n.snapshot_start_page;
}

/**
 * Снимает свою подсветку прежнего прохода. markQuotes старых меток не
 * видит и обернул бы текст второй раз — метка в метке, а плоский текст
 * поиска места поехал бы.
 */
function unwrapHits(root: HTMLElement): void {
  for (const mark of Array.from(root.querySelectorAll(`mark.${HIT}`))) {
    const parent = mark.parentNode;
    if (!parent) continue;
    while (mark.firstChild) parent.insertBefore(mark.firstChild, mark);
    parent.removeChild(mark);
    parent.normalize();
  }
}

/** Блоки, под которыми встаёт карточка; внутри списка — в конец пункта. */
const BLOCKS = 'p, li, dd, dt, h1, h2, h3, h4, h5, h6, pre, blockquote';
const INNER_BLOCKS = new Set(['LI', 'DD', 'DT']);

/** Куда встаёт хост карточки: после узла или первым ребёнком узла. */
interface Slot {
  el: Element;
  where: 'after' | 'start' | 'end';
}

/** Место карточки под абзацем, в котором лежит метка (или шов полосы). */
function slotUnder(anchor: Element): Slot | null {
  const content = anchor.closest('.page-html-content');
  if (!content) return null;
  let block = anchor.closest(BLOCKS);
  // Ячейка таблицы — не место для карточки: она встаёт под всей таблицей.
  if (!block || !content.contains(block) || block.closest('table')) {
    block = anchor;
    while (block.parentElement && block.parentElement !== content) block = block.parentElement;
    if (block.parentElement !== content) return null;
  }
  return { el: block, where: INNER_BLOCKS.has(block.tagName) ? 'end' : 'after' };
}

/** Начало полосы: её секция или, у полосы, начатой швом, сам шов. */
function pageStart(root: HTMLElement, page: number): Element | null {
  return root.querySelector(`[data-page="${page}"]`);
}

function slotAtStart(anchor: Element): Slot | null {
  const own = anchor.querySelector(':scope > .page-html-content');
  if (own) return { el: own, where: 'start' };
  // Шов — строчный узел внутри абзаца предыдущей полосы: карточка встаёт
  // под этим абзацем.
  return slotUnder(anchor);
}

/** Хосты при одном блоке: в начале, после и в конце него. */
type HostSet = Partial<Record<Slot['where'], HTMLElement>>;

/** Ключ портала — по хосту, а не по первой карточке в нём. */
let hostSeq = 0;

interface Placed {
  note: MarginNote;
  found: boolean;
  anchor: Element;
  slot: Slot | null;
}

/** Порядок по месту в документе — сверху вниз, как читатель их встречает. */
function byDocument(a: Placed, b: Placed): number {
  if (a.anchor === b.anchor) return 0;
  return a.anchor.compareDocumentPosition(b.anchor) & Node.DOCUMENT_POSITION_FOLLOWING ? -1 : 1;
}

function wideAvailable(layer: HTMLElement | null, root: HTMLElement | null): boolean {
  if (!layer || !root || !window.matchMedia(WIDE_QUERY).matches) return false;
  return layer.getBoundingClientRect().right - textRight(root) - 2 * SIDE_GAP >= MIN_SIDE;
}

/** Правый край ТЕКСТА колонки: поле номеров полос — пустой отступ, не текст. */
function textRight(root: HTMLElement): number {
  const padding = Number.parseFloat(getComputedStyle(root).paddingRight) || 0;
  return root.getBoundingClientRect().right - padding;
}

/**
 * Слой полей над контейнером полос: подсветка цитат и карточки заметок.
 *
 * Рисуется узлом нулевой высоты прямо перед контейнером полос: на широком
 * окне карточки стоят в нём абсолютно, в правом поле, на высоте своей
 * цитаты; на узком — каждая карточка порталом встаёт в хост-узел под
 * абзацем с цитатой, и текст ниже честно сдвигается.
 *
 * Хост живёт внутри текста полосы, поэтому всё, что читает текст полосы
 * (поиск места цитаты, выделение, подсветка поиска), его отсеивает по классу
 * `margin-note-host` — иначе слова заметки стали бы словами полосы.
 */
export const MarginNotes: React.FC<MarginNotesProps> = ({
  contentRef,
  notes,
  pageNumbers,
  deps,
  onChange,
  onDelete,
}) => {
  const layerRef = useRef<HTMLDivElement>(null);
  const [mode, setMode] = useState<Mode>('narrow');
  const [placed, setPlaced] = useState<Placed[]>([]);
  const [hosts, setHosts] = useState<Map<number, HTMLElement>>(new Map());
  const [editing, setEditing] = useState<number | null>(null);
  // Счётчик повторного прохода: окно перешло порог полей, и хосты под
  // абзацами надо поставить или снять.
  const [rerun, setRerun] = useState(0);
  const cards = useRef(new Map<number, HTMLElement>());
  // Хосты прежнего прохода — по месту: тот же абзац получает тот же хост, и
  // открытое поле правки не пересоздаётся на догрузке соседних полос.
  const hostPool = useRef(new Map<Element, HostSet>());

  const pagesKey = pageNumbers.join(',');

  useEffect(() => {
    // Проход откладывается до конца текущей отрисовки: эффекты потребителя
    // (формулы KaTeX, подсветка поиска и цитаты из адреса) объявлены у
    // родителя и идут ПОСЛЕ эффектов этого слоя. Подсветка, поставленная
    // раньше формул, разрезала бы исходник `$…$`, и формула не отрисовалась бы.
    let cancelled = false;
    queueMicrotask(() => {
      if (cancelled) return;
      const root = contentRef.current;
      if (!root) return;
      unwrapHits(root);

      const visible = new Set(pageNumbers);
      const own = notes.filter((n) => {
        const page = pageOfNote(n);
        return n.target === 'quote' && page !== null && visible.has(page);
      });
      const reqs: MarkRequest[] = own
        .filter((n) => !n.page_missing && n.start_page !== null && n.quote_key !== '')
        .map((n) => ({
          id: n.id,
          span: { start: n.quote_key, end: n.quote_tail || undefined },
          pageNumber: n.start_page!,
        }));
      // Подсветка — украшение чтения, а не его условие: если вёрстка полосы
      // окажется такой, что обернуть кусок нельзя, заметки встают в начало
      // своих полос, а текст главы остаётся читаемым.
      let found = new Set<number>();
      try {
        found = markQuotes(root, reqs, HIT);
      } catch (err) {
        console.error('margin notes: подсветка не удалась', err);
        unwrapHits(root);
      }

      const firstMark = new Map<number, HTMLElement>();
      for (const m of Array.from(root.querySelectorAll<HTMLElement>(`mark.${HIT}`))) {
        for (const id of (m.dataset.noteIds ?? '').split(' ')) {
          const n = Number(id);
          if (!firstMark.has(n)) firstMark.set(n, m);
        }
      }

      const next: Placed[] = [];
      for (const n of own) {
        const mark = found.has(n.id) ? firstMark.get(n.id) : undefined;
        if (mark) {
          next.push({ note: n, found: true, anchor: mark, slot: slotUnder(mark) });
          continue;
        }
        const start = pageStart(root, pageOfNote(n)!);
        if (start) next.push({ note: n, found: false, anchor: start, slot: slotAtStart(start) });
      }
      next.sort(byDocument);

      const wide = wideAvailable(layerRef.current, root);
      const nextHosts = new Map<number, HTMLElement>();
      const pool = new Map<Element, HostSet>();
      if (!wide) {
        for (const p of next) {
          if (!p.slot) continue;
          const { el, where } = p.slot;
          const entry = pool.get(el) ?? {};
          let host = entry[where] ?? hostPool.current.get(el)?.[where];
          if (!host || !host.isConnected) {
            host = document.createElement('div');
            host.className = HOST;
            host.dataset.hostKey = String(++hostSeq);
            if (where === 'after') el.after(host);
            else if (where === 'start') el.prepend(host);
            else el.append(host);
          }
          entry[where] = host;
          pool.set(el, entry);
          nextHosts.set(p.note.id, host);
        }
      }
      // Хосты, которым больше нечего держать, уходят из текста.
      const kept = new Set(nextHosts.values());
      for (const entry of hostPool.current.values()) {
        for (const h of [entry.start, entry.after, entry.end]) if (h && !kept.has(h)) h.remove();
      }
      hostPool.current = pool;

      setMode(wide ? 'wide' : 'narrow');
      setPlaced(next);
      setHosts(nextHosts);
    });
    return () => {
      cancelled = true;
    };
    // deps потребителя — признак новой вёрстки, разворачивается в список
    // (тот же приём, что у useQuoteHighlight).
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [contentRef, notes, pagesKey, rerun, ...deps]);

  // Хосты снимаются вместе со слоем: иначе карточки остались бы в тексте
  // полосы после ухода с экрана, где слой больше не рисуется.
  useEffect(() => {
    const pool = hostPool;
    const root = contentRef.current;
    return () => {
      for (const entry of pool.current.values()) {
        for (const h of [entry.start, entry.after, entry.end]) h?.remove();
      }
      pool.current = new Map();
      if (root) unwrapHits(root);
    };
  }, [contentRef]);

  /** Раскладка на полях: высота цитаты, наезжающие карточки — вниз. */
  const layout = useCallback(() => {
    if (mode !== 'wide') return;
    const layer = layerRef.current;
    const root = contentRef.current;
    if (!layer || !root) return;
    const box = layer.getBoundingClientRect();
    const right = textRight(root);
    const left = right - box.left + SIDE_GAP;
    const width = Math.min(CARD_WIDTH, box.right - right - 2 * SIDE_GAP);
    let bottom = -Infinity;
    for (const p of placed) {
      const card = cards.current.get(p.note.id);
      if (!card) continue;
      card.style.left = `${left}px`;
      card.style.width = `${width}px`;
      const want = p.anchor.getBoundingClientRect().top - box.top;
      const top = Math.max(want, bottom + STACK_GAP);
      card.style.top = `${top}px`;
      bottom = top + card.offsetHeight;
    }
  }, [mode, placed, contentRef]);

  useLayoutEffect(() => {
    layout();
  }, [layout, editing]);

  // Пересчёт на смену окна: перешли порог полей — проход заново (хосты
  // ставятся или снимаются), остались по ту же сторону — только раскладка.
  // Колонка меняет высоту и без смены окна (кегль, мера, картинки
  // догрузились) — это ловит наблюдатель размера.
  useEffect(() => {
    let frame = 0;
    const onResize = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(() => {
        const wide = wideAvailable(layerRef.current, contentRef.current);
        if (wide !== (mode === 'wide')) setRerun((n) => n + 1);
        else layout();
      });
    };
    window.addEventListener('resize', onResize);
    const observer = new ResizeObserver(onResize);
    if (contentRef.current) observer.observe(contentRef.current);
    for (const card of cards.current.values()) observer.observe(card);
    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', onResize);
      observer.disconnect();
    };
  }, [mode, layout, contentRef, placed]);

  // Нажатие на подсветку раскрывает заметку. Выделение текста мышью тоже
  // кончается щелчком — его не трогаем, читатель выделял, а не открывал.
  useEffect(() => {
    const root = contentRef.current;
    if (!root) return;
    const onClick = (e: MouseEvent) => {
      const mark = (e.target as Element | null)?.closest?.(`mark.${HIT}`);
      if (!(mark instanceof HTMLElement) || !root.contains(mark)) return;
      if (window.getSelection()?.isCollapsed === false) return;
      const id = Number((mark.dataset.noteIds ?? '').split(' ')[0]);
      if (!Number.isInteger(id)) return;
      setEditing(id);
      cards.current.get(id)?.scrollIntoView({ block: 'nearest' });
    };
    root.addEventListener('click', onClick);
    return () => root.removeEventListener('click', onClick);
  }, [contentRef]);

  const card = (p: Placed) => {
    const { note } = p;
    const open = editing === note.id;
    return (
      <div
        key={note.id}
        ref={(el) => {
          if (el) cards.current.set(note.id, el);
          else cards.current.delete(note.id);
        }}
        className={`margin-note-card${open ? ' is-open' : ''}${p.found ? '' : ' is-lost'}`}
        data-note-id={note.id}
      >
        {!p.found && (
          <>
            <p className="margin-note-missing">Место в тексте не найдено</p>
            {note.quote_text && <p className="margin-note-snapshot">«{note.quote_text}»</p>}
          </>
        )}
        {open ? (
          <MarginNoteEditor
            initial={note.body}
            save={(body) => marginNotesApi.update(note.id, body).then((r) => r.data)}
            onSaved={(n) => {
              setEditing(null);
              onChange(n);
            }}
            onCancel={() => {
              setEditing(null);
            }}
            remove={() => marginNotesApi.remove(note.id)}
            onRemoved={() => {
              setEditing(null);
              onDelete(note.id);
            }}
          />
        ) : (
          <button
            type="button"
            className="margin-note-open"
            title="Изменить заметку"
            onClick={() => setEditing(note.id)}
          >
            {note.body}
          </button>
        )}
      </div>
    );
  };

  // Карточки одного хоста идут одним порталом, в порядке документа.
  const byHost = new Map<HTMLElement, Placed[]>();
  if (mode === 'narrow') {
    for (const p of placed) {
      const host = hosts.get(p.note.id);
      if (!host) continue;
      const list = byHost.get(host);
      if (list) list.push(p);
      else byHost.set(host, [p]);
    }
  }

  return (
    <div ref={layerRef} className="margin-note-layer">
      {mode === 'wide' && placed.map(card)}
      {Array.from(byHost, ([host, list]) =>
        createPortal(list.map(card), host, host.dataset.hostKey),
      )}
    </div>
  );
};
