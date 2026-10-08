import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useLocation } from 'react-router-dom';
import type { ChapterPage } from '../types';
import 'katex/dist/katex.min.css';
import './ReadingSurface.css';
import { useNoteXrefs } from '../hooks/useNoteXrefs';
import { useFootnotePreview } from '../hooks/useFootnotePreview';
import { readSiteHeaderHeightPx } from '../hooks/useSiteHeaderHeight';
import { getActivePopover } from '../hooks/popoverPlacement';
import { useVisiblePage } from '../hooks/useVisiblePage';
import { printedFolio } from '../utils/folio';
import { pageAnchorId, pageNumberFromHash } from '../utils/pageAnchor';
import { useKatex } from '../hooks/useKatex';
import { usePageSeams } from '../hooks/usePageSeams';
import { useSearchHighlight } from '../hooks/useSearchHighlight';
import { useQuoteHighlight } from '../hooks/useQuoteHighlight';
import type { QuoteSpan } from '../utils/quoteMatch';
import { useHashAnchor } from '../hooks/useHashAnchor';
import { jumpToAnchor, markAddress } from '../utils/anchorNav';
import { isPlainClick } from '../utils/plainClick';
import { stitchPages } from '../utils/stitchPages';
import { FeatureHint } from './FeatureHint';
import { QuoteNotice } from './QuoteNotice';
import { useReadingPrefs } from '../contexts/readingPrefsContext';
import { pagePath } from '../utils/paths';
import { CiteButton, type CitationContext } from './CiteButton';
import { PageJump, type PageJumpProps } from './PageJump';

/** Адреса без цитаты: якоря нет, и useQuoteHighlight в DOM не ходит вовсе. */
const EMPTY_QUOTE: QuoteSpan = { start: '' };

/** Минимум полей работы-источника, нужный читалке: только колонцифры и сноски. */
export interface ReadingWork {
  id: number;
  page_offset: number;
  numbering_style?: string;
  /** Хвост адреса тома — на ссылках «Скан страницы» и «Предложить исправление». */
  slug?: string;
}

export interface ReadingSurfaceProps {
  pages: ChapterPage[];
  footnotesHtml: string;
  /** Работа-источник: нужна для печатных колонцифр и указателя примечаний. */
  work: ReadingWork;
  /** Шапка: заголовок и метаданные — своя у каждого потребителя. */
  header: React.ReactNode;
  /** Кнопки рядом с «Номера страниц» и «Режим чтения» (например шторка подглав). */
  toolbarExtra?: React.ReactNode;
  /**
   * Хлебные крошки в левой части панели.
   *
   * Функция, а не готовый узел: панель сжимается при прокрутке, и сжатая
   * полоска показывает укороченную цепочку — без этого признака потребитель
   * не смог бы её сократить, а состояние сжатия живёт здесь.
   */
  crumbs?: (condensed: boolean) => React.ReactNode;
  /** Навигация «предыдущее/следующее» — рисуется сверху и снизу текста. */
  nav?: React.ReactNode;
  /**
   * Куда ведёт маркер страницы.
   *
   * Функцию надо держать стабильной (useCallback): по ней мемоизирована
   * склейка полос, а она разбирает и пересобирает html всей главы. Новая
   * стрелка на каждый рендер означала бы этот разбор на каждый кадр
   * прокрутки.
   */
  pageHref: (pageNumber: number) => string;
  /** Видимая страница — потребитель пишет по ней прогресс чтения. */
  onVisiblePageChange?: (pageNumber: number | null) => void;
  /**
   * Доля прочитанного, 0..100. Считает её потребитель — он же рисует полосу
   * прогресса поверх окна; панель называет ту же долю цифрой. Без значения
   * надписи нет вовсе: экран зовут и оттуда, где прогресса не считают.
   */
  progressPercent?: number;
  /** Панель сжимается при прокрутке; реф нужен потребителю для useAnchorAboveFold. */
  toolbarRef?: React.RefObject<HTMLDivElement>;
  /**
   * Текст пустого состояния — у главы и у элемента подборки он разный
   * («нет страниц» относится именно к диапазону главы). По умолчанию —
   * прежняя формулировка, чтобы ChapterView не пришлось трогать.
   */
  emptyState?: React.ReactNode;
  /** Леммы запроса (?q= в адресе) — подсветка найденного в тексте. */
  highlightTerms?: string[];
  /**
   * Точное место из адреса — пара якорей `?quote=` (начало) и `&to=` (конец).
   * В отличие от highlightTerms ищет дословно и, если не нашлось, говорит об
   * этом вслух (QuoteNotice) — молчаливый провал читатель не отличил бы от
   * неверной ссылки. Якоря конца нет у ссылок, розданных до этой работы, и у
   * внешних адресов вырезок указателя — тогда подсвечивается одно начало.
   */
  quote?: QuoteSpan;
  /**
   * Данные для кнопки «Цитировать»/«Ссылка» — необязателен: у элемента
   * подборки (CollectionRead) нет ни автора, ни названия работы, ни издания
   * (`entry.source` их не несёт), и подпись цитаты собрать не из чего. Экран
   * главы (ChapterView) эти данные передаёт.
   */
  citation?: CitationContext;
  /**
   * Переход к странице по номеру — встаёт на место доли прочитанного цифрой.
   * Нет его у чтения подборки: там полосы из разных томов, и номер ничего не
   * адресует, — там остаётся доля.
   */
  pageJump?: Omit<PageJumpProps, 'className'>;
}

/**
 * Общий экран чтения: панель, колонка текста по секциям-страницам и сноски.
 *
 * Только отрисовка. Что читается, откуда взялись страницы, куда ведут соседи и
 * где читатель остановился — дело потребителя: главы тома и элемента подборки
 * читаются одним и тем же экраном, но собираются из разных источников.
 */
export const ReadingSurface: React.FC<ReadingSurfaceProps> = ({
  pages,
  footnotesHtml,
  work,
  header,
  toolbarExtra,
  progressPercent,
  crumbs,
  nav,
  pageHref,
  onVisiblePageChange,
  toolbarRef,
  emptyState = <p>В диапазоне этой главы нет страниц.</p>,
  highlightTerms = [],
  quote = EMPTY_QUOTE,
  citation,
  pageJump,
}) => {
  // Номера полос — настройка чтения, а не состояние экрана: своё состояние
  // забывалось при каждом переходе на соседнюю главу, и выключивший номера
  // читатель получал их обратно.
  const { prefs, setPref } = useReadingPrefs();
  const showPageInfo = prefs.pageNumbers;
  const [immersive, setImmersive] = useState(false);
  const [condensed, setCondensed] = useState(false);
  const contentRef = useRef<HTMLDivElement>(null);
  const footnotesRef = useRef<HTMLDivElement>(null);
  const sentinelRef = useRef<HTMLDivElement>(null);
  const pageInfoRef = useRef<HTMLButtonElement>(null);
  // Панельный реф отдаётся наружу, когда потребителю есть что по нему мерить
  // (кромка читаемого текста для useAnchorAboveFold). Своего достаточно, когда
  // не отдаётся: сама панель на нём не завязана.
  const ownToolbarRef = useRef<HTMLDivElement>(null);
  const panelRef = toolbarRef ?? ownToolbarRef;

  // Наблюдение включено всегда: помимо шторки подглав (которой нет без
  // подглав) видимая страница нужна ещё и для записи «где читатель
  // остановился». Секции порождаются страницами, поэтому pages — признак их
  // смены для наблюдателя.
  const visiblePage = useVisiblePage(true, pages);

  // Видимая страница считается здесь, а нужна снаружи: по ней потребитель
  // подсвечивает оглавление и пишет прогресс чтения.
  useEffect(() => {
    onVisiblePageChange?.(visiblePage);
  }, [visiblePage, onVisiblePageChange]);

  useEffect(() => {
    document.body.classList.toggle('reading-immersive', immersive);
    // An open note popover gets the first Escape - it should close before
    // reading mode does, so a keyboard user doesn't lose both at once.
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !getActivePopover()) setImmersive(false);
    };
    window.addEventListener('keydown', onKey);
    return () => {
      window.removeEventListener('keydown', onKey);
      document.body.classList.remove('reading-immersive');
    };
  }, [immersive]);

  // The toolbar shrinks once the page is scrolled. The sentinel sits above the
  // toolbar, so the header height has to be discounted: without it the
  // sentinel still counts as visible while it is hidden behind the sticky
  // header, and the toolbar condenses that many pixels of scrolling late.
  // Reading mode zeroes the header offset in CSS only (`body.reading-immersive
  // { --site-header-height: 0px }`), so JS never sees it change and toggling
  // immersive is not a resize either - both are covered by reading `immersive`
  // directly and listing it as a dependency, so the observer is rebuilt with
  // the right margin the moment reading mode flips.
  //
  // Раньше в зависимостях стоял ещё и признак загрузки: сентинель появлялся
  // только после неё. Теперь его нет — читалка монтируется уже с готовым
  // текстом, а до того потребитель показывает своё состояние загрузки.
  useEffect(() => {
    const el = sentinelRef.current;
    if (!el) return;

    let observer: IntersectionObserver | null = null;
    const observe = () => {
      observer?.disconnect();
      const headerHeight = immersive ? 0 : readSiteHeaderHeightPx();
      observer = new IntersectionObserver(([entry]) => setCondensed(!entry.isIntersecting), {
        rootMargin: `-${headerHeight}px 0px 0px 0px`,
      });
      observer.observe(el);
    };
    observe();

    // The header height changes with the viewport, and rootMargin cannot be
    // updated in place - the observer has to be rebuilt.
    let frame = 0;
    const onResize = () => {
      cancelAnimationFrame(frame);
      frame = requestAnimationFrame(observe);
    };
    window.addEventListener('resize', onResize);

    return () => {
      cancelAnimationFrame(frame);
      window.removeEventListener('resize', onResize);
      observer?.disconnect();
    };
  }, [immersive]);

  // Формулы встречаются и в тексте страниц, и в сносках: два контейнера —
  // два прохода, но по одной и той же таблице разделителей.
  useKatex(contentRef, [pages, footnotesHtml]);
  useKatex(footnotesRef, [pages, footnotesHtml]);

  useNoteXrefs(footnotesRef, work.id, work.slug, [footnotesHtml]);
  useNoteXrefs(contentRef, work.id, work.slug, [pages, footnotesHtml]);
  useFootnotePreview(contentRef, [pages, footnotesHtml]);
  // Маркер полосы уводит к её началу и отмечает место в адресе. Своя
  // обработка, а не переход по хэшу, — ради повторного нажатия: хэш при нём
  // тот же самый, и сменой адреса второго прыжка не сделать.
  const markPlace = useCallback((href: string) => {
    jumpToAnchor(href.replace(/^#/, ''));
    markAddress(href);
  }, []);

  usePageSeams(contentRef, [pages], markPlace);
  useSearchHighlight(contentRef, highlightTerms, true, [pages, footnotesHtml]);

  // Ставится после хуков, наполняющих текст: якорь из присланной ссылки
  // ищется в уже отрисованных секциях.
  useHashAnchor(pages);

  // Номер полосы для ?quote= — из хэша адреса (#chapter-page-N), а не из
  // видимой сейчас страницы: сужение должно указывать место, названное самой
  // ссылкой, а не то, что читатель успел прокрутить. Прыжок к этому хэшу уже
  // делает useHashAnchor выше — здесь хэш только читается, второй раз не
  // прыгаем. Нет якоря в адресе — undefined, и useQuoteHighlight поиск не
  // начинает вовсе: адрес без места ничего не подтверждает, а подсветка
  // первого совпадения в главе до 742 полос завела бы читателя не туда молча.
  const { hash } = useLocation();
  const quotePageNumber = pageNumberFromHash(hash);
  const quoteMatch = useQuoteHighlight(contentRef, quote, quotePageNumber, [pages, footnotesHtml]);

  // У тома offset нулевой, колонцифра совпадает с номером — дублировать её
  // незачем. Печатный номер добавляется в подпись для скринридера только там,
  // где счёт расходится с адресным.
  const pageLabel = useCallback(
    (pageNumber: number): string => {
      const folio = printedFolio(pageNumber, work);
      return folio && folio !== String(pageNumber)
        ? `Страница ${pageNumber}, печатная ${folio}`
        : `Страница ${pageNumber}`;
    },
    [work],
  );

  // Склейка полос разбирает и пересобирает html всей главы, поэтому считается
  // мемоизированно. Отсюда же требование к pageHref (см. проп): нестабильная
  // функция означала бы пересборку на каждый кадр прокрутки, потому что
  // ChapterView перерисовывается на каждый её кадр (useReadingProgress).
  const stitched = useMemo(
    () =>
      stitchPages(
        pages.map(({ page_number, html }) => ({ pageNumber: page_number, html })),
        { href: pageHref, label: pageLabel },
      ),
    [pages, pageHref, pageLabel],
  );

  return (
    <div className="chapter-view-container">
      <div ref={sentinelRef} className="chapter-view-sentinel" aria-hidden="true" />
      <div ref={panelRef} className={`chapter-view-header ${condensed ? 'is-condensed' : ''}`}>
        <div className="chapter-view-crumbs">{crumbs?.(condensed)}</div>
        <div className="chapter-view-actions">
          {/* Первой в правой группе, вплотную под полосой прогресса: номер
              видимой полосы (он же переход к другой), а без перехода — доля
              прочитанного цифрой. Долю на глаз показывает сама полоса с
              засечками через десять процентов. */}
          {pageJump ? (
            <PageJump {...pageJump} />
          ) : (
            progressPercent !== undefined && (
              <span className="reading-percent">{progressPercent} %</span>
            )
          )}
          {toolbarExtra}
          {/* Опечатку замечают в потоке чтения, а не на отдельной полосе:
              ссылка ведёт на правку видимой сейчас страницы, а не на
              просмотр (для него уже есть pageHref на маркере полосы).
              Класс .btn — тот же, что у соседних кнопок панели: только он
              подхватывает сжатие `.is-condensed .chapter-view-actions .btn`
              при прокрутке, без него ссылка осталась бы полноразмерной
              рядом с ужавшимися соседями. */}
          {/* Маркер полосы стал якорем на её начало, и прежний переход на
              отдельный экран полосы (скан рядом с текстом) переехал сюда —
              к той же видимой полосе, что и ссылка на правку.
              Скан открывается отдельным окном: он не продолжение чтения, а
              сверка с оригиналом, и уход в том же окне стоил читателю места
              в главе. Стрелка предупреждает об этом до нажатия; у соседней
              ссылки на правку её нет — там работа продолжается на месте. */}
          {visiblePage !== null && (
            <Link
              to={pagePath(work, visiblePage)}
              target="_blank"
              rel="noopener noreferrer"
              className="btn btn-secondary reading-scan-link"
              aria-label="Скан страницы"
            >
              <span className="toolbar-label-full">Скан страницы</span>
              <span className="toolbar-label-short">Скан</span>
              <span className="toolbar-label-external" aria-hidden="true">
                ↗
              </span>
            </Link>
          )}
          {visiblePage !== null && (
            <Link
              to={`${pagePath(work, visiblePage)}/suggest`}
              className="btn btn-secondary reading-suggest-link"
              aria-label="Предложить исправление"
            >
              <span className="toolbar-label-full">Предложить исправление</span>
              <span className="toolbar-label-short">Исправить</span>
            </Link>
          )}
          {citation && (
            <CiteButton
              contentRef={contentRef}
              visiblePage={visiblePage}
              className="btn btn-secondary"
              {...citation}
            />
          )}
          {/* Вторая кнопка номеров страниц в читальне: значок «№» в панели
              потокового чтения делает то же самое (ReadingStreamBar). Выноска
              нужна обеим — глава и чтение подборки идут через этот экран, и
              без неё возможность объяснена только в потоке. Один id на две
              разметки законен: право показаться выдаётся видимому экземпляру. */}
          {/* Подпись на телефоне короче, а доступное имя остаётся полным:
              скрытый display: none текст в имя кнопки не входит, поэтому
              полное имя задаётся aria-label (см. .toolbar-label-* в
              ReadingSurface.css). */}
          <button
            ref={pageInfoRef}
            onClick={() => setPref('pageNumbers', !showPageInfo)}
            className="btn btn-secondary"
            aria-pressed={showPageInfo}
            aria-label="Номера страниц"
          >
            <span className="toolbar-label-full">Номера страниц</span>
            <span className="toolbar-label-short">Номера</span>
          </button>
          <FeatureHint id="page-numbers" anchorRef={pageInfoRef} />
          <button
            onClick={() => setImmersive((v) => !v)}
            className="btn btn-secondary"
            aria-pressed={immersive}
            aria-label={immersive ? 'Выйти из чтения' : 'Режим чтения'}
          >
            <span className="toolbar-label-full">
              {immersive ? 'Выйти из чтения' : 'Режим чтения'}
            </span>
            <span className="toolbar-label-short">{immersive ? 'Выйти' : 'Чтение'}</span>
          </button>
        </div>
      </div>

      <article className="chapter-content">
        {header}

        {nav}

        <QuoteNotice match={quoteMatch} />

        <div
          ref={contentRef}
          className={`chapter-pages-content ${showPageInfo ? 'show-page-info' : ''}`}
          lang="ru"
        >
          {pages.length === 0 ? (
            <div className="empty-state">{emptyState}</div>
          ) : (
            pages.map((page, i) => {
              const { html, seamed } = stitched[i];
              // Полоса уехала в предыдущую целиком: рисовать нечего, а якорь и
              // маркер уже стоят на шве.
              if (seamed && html === '') return null;
              return (
                <div
                  key={page.page_number}
                  // У склеенной полосы якорь на шве — второй такой же id в
                  // документе увёл бы прокрутку оглавления на абзац ниже
                  // настоящего начала страницы.
                  id={seamed ? undefined : pageAnchorId(page.page_number)}
                  className={`chapter-page-section ${seamed ? 'is-seamed' : ''}`}
                  // data-page ставится и на склеенной секции: её остаток
                  // текста принадлежит этой же полосе, а id у неё уже занят
                  // швом. По этому атрибуту выделение цитаты узнаёт полосу —
                  // то же, что уже делает шов.
                  data-page={page.page_number}
                  tabIndex={-1}
                >
                  {/* A blank page renders nothing, which makes its section a
                    self-collapsing box: zero height, margins collapsed away,
                    top edge coinciding with the next section's. Its marker
                    would land exactly on top of the next page's number, so
                    blank pages go unnumbered rather than move the text. */}
                  {!seamed && !page.blank && (
                    // Не <Link>: маркер — метка места, а не переход. Настоящая
                    // ссылка с адресом нужна ради контекстного меню
                    // («копировать адрес ссылки») и модификаторов — их
                    // обычный клик и пропускает, см. isPlainClick.
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
                </div>
              );
            })
          )}
        </div>

        {footnotesHtml && (
          <div
            ref={footnotesRef}
            className="chapter-footnotes"
            dangerouslySetInnerHTML={{ __html: footnotesHtml }}
          />
        )}

        {/* Тот же узел навигации, что и над текстом. Обёртка — ради нижнего
            отступа: копии различимы только снаружи, а прежняя разметка отбивала
            нижний блок от сносок сильнее верхнего. */}
        {nav && <div className="chapter-nav-bottom">{nav}</div>}
      </article>
    </div>
  );
};
