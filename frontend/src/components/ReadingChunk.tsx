import { useCallback, useMemo, useRef } from 'react';
import 'katex/dist/katex.min.css';
// Маршруты грузятся лениво, и Vite режет CSS по тем же чанкам: стили,
// объявленные в файле, который этот маршрут не импортирует, до него не
// доезжают. Разметка ниже держится на правилах ReadingSurface.css
// (.chapter-pages-content, .page-marker, пара подписей на кнопках панели) —
// поэтому импорт стоит здесь, а не подразумевается соседом. Сторож —
// readingStyles.guard.test.ts.
import './ReadingSurface.css';
import './ReadingChunk.css';

import type { ReadingPage } from '../types';
import { useKatex } from '../hooks/useKatex';
import { useNoteXrefs } from '../hooks/useNoteXrefs';
import { useFootnotePreview } from '../hooks/useFootnotePreview';
import { usePageSeams } from '../hooks/usePageSeams';
import { isPlainClick } from '../utils/plainClick';
import { useSearchHighlight } from '../hooks/useSearchHighlight';
import { stitchPages } from '../utils/stitchPages';
import { pageAnchorId } from '../utils/pageAnchor';
import { printedFolio, type FolioSource } from '../utils/folio';

export interface ReadingChunkProps {
  pages: ReadingPage[];
  /**
   * Работа-источник: id нужен указателю примечаний, остальное — колонцифре.
   * slug — тот же хвост адреса, что и у ссылок читальни; без него
   * перекрёстная ссылка на примечание выйдет голым номером (легально, но
   * хуже, если слаг под рукой).
   */
  work: FolioSource & { id: number; slug?: string };
  /** Куда ведёт маркер номера страницы. */
  pageHref: (pageNumber: number) => string;
  /** Показывать ли маркеры печатных номеров: тумблер живёт в панели потока. */
  showPageInfo: boolean;
  /** Леммы запроса для подсветки; первое совпадение прокручивается только у стартового окна. */
  highlightTerms?: string[];
  scrollToFirstHit?: boolean;
}

/**
 * Одно загруженное окно потока.
 *
 * Собственный компонент на окно — не стилистика, а необходимость. Хуки
 * читалки (KaTeX, перекрёстные ссылки, подсказки сносок) вешаются на
 * контейнер целиком; будь контейнер один на весь поток, каждое новое окно
 * перевешивало бы обработчики на всём накопленном тексте, и к семисотой
 * странице это стоило бы больше, чем сэкономила кусочная загрузка.
 *
 * Содержимое окна после монтирования не меняется, поэтому все эффекты идут с
 * пустыми зависимостями и отрабатывают ровно один раз.
 */
export const ReadingChunk: React.FC<ReadingChunkProps> = ({
  pages,
  work,
  pageHref,
  showPageInfo,
  highlightTerms = [],
  scrollToFirstHit = false,
}) => {
  const rootRef = useRef<HTMLDivElement>(null);

  useKatex(rootRef, []);
  useNoteXrefs(rootRef, work.id, work.slug, []);
  useFootnotePreview(rootRef, []);
  // Адрес маркера — адрес самого потока (/works/{id}/read/{N}), поэтому
  // переход по нему пересобрал бы поток с этой полосы и выбросил всё
  // загруженное. Читателю нужна ссылка, которую можно скопировать и
  // переслать, а не прыжок: адрес в строке правится на месте.
  const markPlace = useCallback((href: string) => {
    window.history.replaceState(null, '', href);
  }, []);

  usePageSeams(rootRef, [], markPlace);
  useSearchHighlight(rootRef, highlightTerms, scrollToFirstHit, []);

  // Печатный номер добавляется в подпись для скринридера только там, где счёт
  // расходится с адресным: у тома offset нулевой и дублировать нечего.
  const pageLabel = (pageNumber: number): string => {
    const folio = printedFolio(pageNumber, work);
    return folio && folio !== String(pageNumber)
      ? `Страница ${pageNumber}, печатная ${folio}`
      : `Страница ${pageNumber}`;
  };

  // Содержимое окна после монтирования не меняется — склейка считается один
  // раз, вместе с ним. Стык между двумя соседними окнами не склеивается:
  // окна живут в разных контейнерах, а это один стык на полсотни страниц.
  const stitched = useMemo(
    () =>
      stitchPages(
        pages.map(({ page_number, html }) => ({ pageNumber: page_number, html })),
        { href: pageHref, label: pageLabel },
      ),
    // eslint-disable-next-line react-hooks/exhaustive-deps
    [pages],
  );

  return (
    // .chapter-pages-content переиспользует типографику читалки
    // (шрифт/кегль/отбивка из настроек, показ .page-marker по
    // .show-page-info) из ReadingSurface.css — заводить её копию здесь
    // означало бы держать сорок строк CSS в двух местах.
    <div
      ref={rootRef}
      className={`reading-chunk chapter-pages-content ${showPageInfo ? 'show-page-info' : ''}`}
      lang="ru"
    >
      {pages.map((page, i) => {
        const { html, seamed } = stitched[i];
        // Полоса уехала в предыдущую целиком, и своих сносок у неё нет:
        // рисовать нечего, якорь и маркер уже стоят на шве.
        if (seamed && html === '' && !page.notes_html) return null;
        return (
          <div
            key={page.page_number}
            // У склеенной полосы якорь на шве: второй такой же id увёл бы
            // прокрутку на абзац ниже настоящего начала страницы.
            id={seamed ? undefined : pageAnchorId(page.page_number)}
            className={`chapter-page-section ${seamed ? 'is-seamed' : ''}`}
            // data-page ставится и на склеенной секции: её остаток текста
            // принадлежит этой же полосе, а id у неё уже занят швом. По
            // этому атрибуту выделение цитаты узнаёт полосу — то же, что
            // уже делает шов.
            data-page={page.page_number}
            tabIndex={-1}
          >
            {/* Пустая полоса не рисует ничего, и её секция схлопывается в
              нулевую высоту: маркер сел бы ровно на номер следующей
              страницы. Поэтому пустые остаются без номера, а не двигают
              текст. */}
            {!page.blank && !seamed && (
              // Не <Link>: переход остался бы переходом. Настоящая ссылка с
              // адресом нужна ради контекстного меню («копировать адрес
              // ссылки») и модификаторов — их обычный клик и пропускает.
              <a
                href={pageHref(page.page_number)}
                className="page-marker"
                aria-label={pageLabel(page.page_number)}
                onClick={(event) => {
                  if (!isPlainClick(event)) return;
                  event.preventDefault();
                  markPlace(pageHref(page.page_number));
                }}
              >
                {page.page_number}
              </a>
            )}
            <div className="page-html-content" dangerouslySetInnerHTML={{ __html: html }} />
            {page.notes_html && (
              // chapter-footnotes — тот же класс, что у блока сносок внизу
              // главы: всё их оформление (кегль заголовка, список без
              // буллетов, отбивка обратной ссылки, scroll-margin-top под
              // липкой панелью) написано в ReadingSurface.css от него.
              // Своё имя остаётся только ради положения блока.
              <div
                className="reading-chunk-notes chapter-footnotes"
                dangerouslySetInnerHTML={{ __html: page.notes_html }}
              />
            )}
          </div>
        );
      })}
    </div>
  );
};
