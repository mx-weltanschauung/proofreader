import { useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import type { Work } from '../types';
import { readSelection, fullyInsideExcluded } from '../utils/quoteSelection';
import { normalizeQuote } from '../utils/quoteMatch';
import { quoteKey, quoteTail } from '../utils/quoteKey';
import {
  citationSignature,
  citationMarkdown,
  citationHtml,
  citationLinkHtml,
  citationLinkMarkdown,
  seamMarker,
} from '../utils/citation';
import { printedFolio } from '../utils/folio';
import './CiteButton.css';

export interface CitationContext {
  work: Work;
  /** Заголовок неаппаратной главы верхнего уровня, накрывающей полосу. */
  workTitleFor: (pageNumber: number) => string;
  /**
   * Адрес места в тексте — для ТОЙ поверхности, с которой снимают цитату:
   * путь с готовой строкой параметров (`?quote=…&to=…`) и, если поверхность
   * его требует, якорем полосы.
   *
   * Строит его поверхность, а не кнопка, и это решение, а не проводка ради
   * проводки. Кнопка раньше строила адрес полосы сама (`pagePath`) — и
   * читатель, процитировавший кусок В ГЛАВЕ, получал ссылку на отдельную
   * полосу: не то место, где он был, и без потока вокруг цитаты. Какой адрес
   * читатель узнаёт своим, знает только сам экран: глава ведёт на главу с
   * якорем полосы, страница со сканом — на полосу.
   *
   * Обязателен намеренно: умолчание «полоса» вернуло бы прежнюю ошибку молча
   * у любой новой поверхности, забывшей передать своё.
   */
  quoteHref: (pageNumber: number, search: string) => string;
}

export interface CiteButtonProps extends CitationContext {
  contentRef: React.RefObject<HTMLElement>;
  /** Полоса, на которую ведёт «Ссылка», когда выделения нет. */
  visiblePage: number | null;
  className?: string;
}

/**
 * Есть ли непустое выделение внутри root — дёшево, без полного разбора.
 *
 * Это единственное, что можно позволить себе на каждое `selectionchange`
 * (то есть на каждое движение мыши с зажатой кнопкой): readSelection и
 * flattenText делают полный проход по всему root (802 мс на главе в 742
 * полосы в jsdom — см. докблок `quoteSelection.ts`), и звать их здесь
 * означало бы заикание при обычном выделении текста. Проверка — только
 * isCollapsed и то, что диапазон лежит внутри контейнера, плюс два дешёвых
 * исключения — те же, что и в readSelection, но без его дорогого прохода:
 *
 * - fullyInsideExcluded(range) — то же самое native-Range исключение
 *   колонцифры/формулы, что использует readSelection. Без него двойной клик
 *   по печатной колонцифре (обычное читательское движение, не редкость) менял
 *   бы надпись на «Цитировать», а нажатие уносило бы в буфер не то — само
 *   readSelection эту же цитату отвергло бы (fix-раунд 1, важное 4).
 * - normalizeQuote(range.toString()) !== '' — range.toString() дешёвый
 *   (нативный, без обхода дерева), и голое выделение из одних пробелов не
 *   должно называться «Цитировать».
 *
 * Полный разбор выделения происходит один раз, в момент нажатия кнопки.
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

/**
 * Кнопка «Цитировать» / «Ссылка»: уносит выделенный кусок текста вместе с
 * выходными данными и рабочей ссылкой на точное место в буфер обмена.
 *
 * Слово меняется по наличию выделения: «Цитировать», когда есть что
 * процитировать, иначе «Ссылка» — тогда в буфер уходит подпись без цитаты, а
 * адрес ведёт на видимую сейчас полосу (`visiblePage`).
 */
export const CiteButton: React.FC<CiteButtonProps> = ({
  work,
  workTitleFor,
  quoteHref,
  contentRef,
  visiblePage,
  className,
}) => {
  const [hasSelection, setHasSelection] = useState(false);

  useEffect(() => {
    const update = () => setHasSelection(hasSelectionInside(contentRef.current));
    document.addEventListener('selectionchange', update);
    // Выделение могло появиться (или пропасть) ещё до монтирования кнопки —
    // например, при переходе между полосами потокового чтения.
    update();
    return () => document.removeEventListener('selectionchange', update);
  }, [contentRef]);

  const onClick = async () => {
    const root = contentRef.current;
    // Полный разбор выделения — только здесь, в момент нажатия, не раньше.
    const selected = root ? readSelection(root, (n) => seamMarker(printedFolio(n, work), n)) : null;

    // Адрес цитаты — полоса, где выделение НАЧАЛОСЬ (pages[0], решение
    // тикета 15). Последняя полоса переходов (pages[pages.length - 1]) или
    // конец pageSpan для адреса не годятся: на выделении, начавшемся в шве
    // ([6,5,6]), это увело бы адрес не туда, где читатель начал выделять.
    const pages = selected?.pages ?? (visiblePage !== null ? [visiblePage] : []);
    if (pages.length === 0) return;

    // Два якоря: ключ начала метит, откуда цитата, якорь конца — докуда.
    // Второй нужен ровно затем, чтобы на приёме подсветилось процитированное
    // предложение, а не ключ из двух слов (markQuote, QuoteSpan).
    const key = selected ? quoteKey(selected.head, selected.firstPageText) : '';
    const tail = selected ? quoteTail(selected.tail, selected.lastPageText) : '';
    // Якорь конца не едет, когда ключ начала и так накрывает всё выделение:
    // на цитате в одну полосу, уникальной с первого слова, `to` удлинял бы
    // адрес, ничего не добавляя подсветке. Через стык полос он нужен всегда —
    // хвост лежит на другой полосе, ключ до него не достаёт по определению.
    const keyCoversAll =
      selected !== null && selected.pageSpan.length === 1 && key === selected.head;
    const params = [
      key ? `quote=${encodeURIComponent(key)}` : '',
      tail && !keyCoversAll ? `to=${encodeURIComponent(tail)}` : '',
    ].filter(Boolean);
    const search = params.length > 0 ? `?${params.join('&')}` : '';
    const url = `${window.location.origin}${quoteHref(pages[0], search)}`;

    // Диапазон ДЛЯ ПОДПИСИ — концы pageSpan (отсортированный список задетых
    // полос), а не концы pages (последовательность переходов, с повторами):
    // на выделении, начавшемся в шве ([6,5,6]), концы pages напечатали бы
    // «с. 6—6» у цитаты, которая на самом деле тянется с 5 по 6. Расхождение
    // адреса (по началу выделения) с подписью (по краям диапазона) названо
    // вслух в решении 15 спеки и остаётся осознанно.
    const span = selected ? selected.pageSpan : pages;
    // Концы диапазона для подписи: одна полоса — один конец, иначе оба.
    // Раньше здесь всегда строился массив длины 2 из span[0] и
    // span[span.length-1] — на span.length===1 оба индекса совпадали и
    // folioLabel честно печатал «с. 5—5» вместо «с. 5» (fix-раунд 1,
    // важное 1: дефект мой).
    const ends = span.length > 1 ? [span[0], span[span.length - 1]] : [span[0]];
    const signature = citationSignature({
      author: work.author ?? '',
      workTitle: workTitleFor(pages[0]),
      editionTitle: work.edition_title ?? '',
      volumeTitle: work.title,
      volumeNumber: work.volume_number,
      volumePart: work.volume_part,
      folios: ends.map((n) => printedFolio(n, work)),
      pageNumbers: ends,
    });

    // Адрес приклеивают грани буфера, а не подпись: в html-грани он обязан
    // стать живой ссылкой <a href>. Текст цитаты у ветки «Ссылка» пуст —
    // уезжает одна подпись с адресом.
    const body = selected?.text ?? '';
    const markdown = body
      ? citationMarkdown(body, signature, url)
      : citationLinkMarkdown(signature, url);
    const html = body ? citationHtml(body, signature, url) : citationLinkHtml(signature, url);

    try {
      // Буфер пишется ОДНИМ ClipboardItem с двумя гранями: формат выбирает
      // не читатель, а то, куда он вставляет, — редактор возьмёт Markdown,
      // Word и Телеграм оформленную цитату. Меню форматов не нужно.
      //
      // Между нажатием (onPointerDown ниже) и этой строкой нет ни одного
      // await: запрос доступа к буферу в этом месте рвёт жест пользователя,
      // и браузер отказывает — поэтому серверного /citation нет вовсе,
      // строка собирается из того, что уже лежит на экране.
      await navigator.clipboard.write([
        new ClipboardItem({
          'text/plain': new Blob([markdown], { type: 'text/plain' }),
          'text/html': new Blob([html], { type: 'text/html' }),
        }),
      ]);
      toast.success(selected ? 'Цитата скопирована' : 'Ссылка скопирована');
    } catch {
      // Запасного пути при отказе буфера нет (решение 13 спеки) — сообщить
      // об этом читателю единственное, что остаётся.
      toast.error('Не удалось скопировать');
    }
  };

  // Без выделения и без видимой полосы цитировать/ссылаться не на что: та же
  // проверка, что уже стоит на onClick (pages.length === 0), только раньше —
  // на рендере, а не после бесполезного нажатия. Кнопка молча ничего не
  // делавшая (fix-раунд 1, важное 3) хуже отсутствующей: соседние органы
  // управления панели (скан/правка, ReadingSurface.tsx) в этом же состоянии
  // не рендерятся вовсе (`visiblePage !== null && (...)`), а не гасят себя.
  if (!hasSelection && visiblePage === null) return null;

  const label = hasSelection ? 'Цитировать' : 'Ссылка';

  return (
    <button
      type="button"
      className={className ? `cite-button ${className}` : 'cite-button'}
      // Гасится, иначе нажатие на кнопку снимает выделение системой раньше,
      // чем onClick успевает его прочитать: выделение читается В МОМЕНТ
      // нажатия и ни секундой раньше.
      onPointerDown={(e) => e.preventDefault()}
      onClick={() => void onClick()}
    >
      {label}
    </button>
  );
};
