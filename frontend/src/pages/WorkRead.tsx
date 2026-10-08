import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { Link, useNavigate, useParams, useSearchParams } from 'react-router-dom';

import { worksApi, chaptersApi } from '../services/api';
import type { Work, Chapter } from '../types';
import { ReadingChunk } from '../components/ReadingChunk';
import { ReadingStreamBar } from '../components/ReadingStreamBar';
import { useReadingStream } from '../hooks/useReadingStream';
import { useVisiblePage } from '../hooks/useVisiblePage';
import { useInfiniteSentinel } from '../hooks/useInfiniteSentinel';
import { chapterLevelsForPage } from '../hooks/useChaptersForPage';
import { useRecordRead } from '../hooks/useReadingProgress';
import { getActivePopover } from '../hooks/popoverPlacement';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { useSearchTerms } from '../hooks/useSearchTerms';
import { useReadingPrefs } from '../contexts/readingPrefsContext';
import { apiErrorMessage } from '../utils/apiError';
import { chapterPath, numericId, pagePath, readPath, workPath } from '../utils/paths';
import { missingPageMessage, parsePrintedPage } from '../utils/pageJump';
import { printedFolio } from '../utils/folio';
import { pageAnchorId } from '../utils/pageAnchor';
import { jumpToAnchor } from '../utils/anchorNav';
import './WorkRead.css';

/**
 * За сколько пикселей до низа просить следующее окно. Примерно два экрана:
 * окно должно приехать раньше, чем читатель до него доберётся, — момент
 * упора в конец текста он видеть не должен.
 */
const PREFETCH_MARGIN_PX = 1500;

/**
 * Потоковое чтение работы: текст приезжает окнами по мере прокрутки и не
 * обрывается на границе главы.
 *
 * Адресуется страница, а не глава: позиция в потоке — это страница, а глава
 * лишь точка входа. Вверх от неё поток не растёт.
 */
export const WorkRead: React.FC = () => {
  const { workId, pageNumber } = useParams<{ workId: string; pageNumber: string }>();
  const parsedWorkId = numericId(workId);
  const startPage = Number(pageNumber);
  const valid = parsedWorkId !== null && Number.isInteger(startPage) && startPage > 0;
  // Гарантированно число: используется только под `valid`, где parsedWorkId
  // уже не null — запасное значение ниже никогда фактически не читается.
  const numericWorkId = parsedWorkId ?? 0;

  const [work, setWork] = useState<Work | null>(null);
  const [contextError, setContextError] = useState('');
  const [chapters, setChapters] = useState<Chapter[]>([]);
  // Номера идут на весь поток сразу: окна монтируются по мере чтения, и
  // состояние в каждом из них расходилось бы — выключив номера на сотой
  // странице, читатель по-прежнему видел бы их на уже прочитанных. Хранятся
  // они в настройках чтения, общих с экраном главы: своё состояние экрана
  // забывалось при каждом переходе.
  const { prefs, setPref } = useReadingPrefs();
  const showPageNumbers = prefs.pageNumbers;

  // Печатный номер — как на бумаге и как считает internal/seo/render_text.go
  // (ReadPage): page_number + page_offset. Голый номер маршрута совпадает с
  // ним только у тома с нулевым сдвигом; у передних листов разошёлся бы.
  useDocumentTitle(work ? `${work.title}, с. ${startPage + work.page_offset}` : null);

  const stream = useReadingStream(valid ? numericWorkId : undefined, valid ? startPage : undefined);
  const [searchParams] = useSearchParams();
  const highlightTerms = useSearchTerms(searchParams.get('q') ?? '');

  // Работа нужна ради колонцифры (page_offset, numbering_style) и заголовка.
  // Запрос уходит один раз, параллельно с первым окном.
  useEffect(() => {
    if (!valid) return;
    let cancelled = false;

    worksApi
      .get(numericWorkId)
      .then((res) => {
        if (cancelled) return;
        setWork(res.data);
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setContextError(apiErrorMessage(err, 'Не удалось загрузить том'));
      });

    return () => {
      cancelled = true;
    };
  }, [valid, numericWorkId]);

  // Дерево глав нужно бегущему заголовку. Один запрос на весь сеанс чтения:
  // на томе 47 это 42 КБ, и на границе главы в сеть ходить уже не надо.
  useEffect(() => {
    if (!valid) return;
    let cancelled = false;

    chaptersApi
      .list(numericWorkId)
      .then((res) => {
        if (!cancelled) setChapters(res.data || []);
      })
      .catch(() => {
        // Заголовок главы — удобство. Без дерева поток читается, просто
        // панель остаётся без подписи; ронять ради этого экран нечего.
      });

    return () => {
      cancelled = true;
    };
  }, [valid, numericWorkId]);

  // Секции порождают окна, поэтому признак их смены для наблюдателя — сам
  // массив окон. Смена здесь всегда 'append': окна только дописываются в
  // конец, вверх поток не растёт. Отсюда два следствия — приезд окна не
  // обнуляет видимую страницу (иначе полоса прогресса, бегущий заголовок и
  // ссылка выхода на кадр теряли бы своё значение) и не заставляет
  // наблюдателя переподписываться на все накопленные секции.
  const visiblePage = useVisiblePage(true, stream.windows, 'append');

  // Опечатку замечают в потоке тома — он и есть основной способ читать, —
  // а не на отдельной полосе. Ссылка ведёт на правку той страницы, что
  // сейчас видима; пока она не определена, вести некуда.
  const suggestHref =
    visiblePage !== null
      ? `${pagePath({ id: numericWorkId, slug: work?.slug }, visiblePage)}/suggest`
      : null;

  // Скан видимой полосы: маркер, который уводил туда прежде, стал якорем
  // самого потока.
  const scanHref =
    visiblePage !== null ? pagePath({ id: numericWorkId, slug: work?.slug }, visiblePage) : null;

  // Самая глубокая глава, накрывающая видимую страницу. Дерево уже загружено,
  // функция чистая — на границе главы в сеть не ходим.
  const runningChapter = useMemo(() => {
    if (visiblePage === null) return null;
    const levels = chapterLevelsForPage(chapters, visiblePage);
    return levels[levels.length - 1]?.[0] ?? null;
  }, [chapters, visiblePage]);

  // Экран чтения прячет шапку сайта тем же классом, что и режим чтения главы:
  // стили для него уже написаны.
  useEffect(() => {
    document.body.classList.add('reading-immersive');
    return () => document.body.classList.remove('reading-immersive');
  }, []);

  // Адрес следует за читателем, чтобы перезагрузка и «скопировать ссылку»
  // попадали туда, где он стоит. replaceState, а не navigate: каждая
  // прокрученная страница в истории браузера сделала бы кнопку «назад»
  // бесполезной.
  //
  // Строка запроса (?q= с Task 11 и всё, что к ней добавится) переписывается
  // вместе с номером страницы, а не отбрасывается: иначе «попадали туда, где
  // он стоит» переставало выполняться для ровно того захода, ради которого
  // ?q= и существует, — прямой ссылки с подсветкой. Источник — searchParams
  // из useSearchParams(), а не window.location.search: этот же эффект пишет в
  // window.location в обход react-router, поэтому после первого его срабатывания
  // «живой» window.location.search — это уже результат ЕГО СОБСТВЕННОЙ прошлой
  // записи, а не независимая правда. searchParams же — снимок с исходной
  // навигации (react-router её видел, значит, синхронизировал и window.location
  // тогда), и этот эффект его больше не трогает, так что порча исключена.
  //
  // От этого же выбора (replaceState, а не navigate) зависит координатор
  // подсказок (HintsProvider): он сбрасывает «право экрана» по смене
  // useLocation().pathname, а history.replaceState меняет адресную строку в
  // обход react-router — его useLocation() эту смену не видит вовсе. Поэтому
  // пролистывание страниц тома не считается новым «экраном» для подсказок
  // (что и нужно: одна и та же выноска не обязана перевзводиться на каждой
  // странице), но если это когда-нибудь заменят на navigate()/useNavigate,
  // поведение подсказок здесь изменится вместе с этим. Дописанная строка
  // запроса этого выбора не меняет: путь по-прежнему пишется мимо
  // react-router, тем же вызовом.
  useEffect(() => {
    if (visiblePage === null || !valid) return;
    const query = searchParams.toString();
    const path = readPath({ id: numericWorkId, slug: work?.slug }, visiblePage);
    window.history.replaceState(null, '', `${path}${query ? `?${query}` : ''}`);
  }, [visiblePage, valid, numericWorkId, work?.slug, searchParams]);

  // Строка списка недавно читанного адресует главу — её читают главная и
  // шапка тома, строя ссылку /works/{workId}/chapters/{chapterId}. Поэтому
  // пишем только когда бегущая глава известна: без неё запись увела бы
  // «Продолжить» в никуда. В потоке бегущая глава меняется прокруткой, и
  // минута порога у каждой своя — пролистанная мимо в список не попадёт.
  useRecordRead(
    work && runningChapter && visiblePage !== null
      ? {
          workId: numericWorkId,
          chapterId: runningChapter.id,
          workTitle: work.title,
          chapterTitle: runningChapter.title,
          pageNumber: visiblePage,
        }
      : null,
  );

  // Выход из чтения: в бегущую главу, а не в ту, через которую вошли, — за
  // сотню страниц потока это давно разные главы.
  const backHref = runningChapter
    ? chapterPath({ id: numericWorkId, slug: work?.slug }, runningChapter)
    : workPath({ id: numericWorkId, slug: work?.slug });

  const navigate = useNavigate();

  // Escape выходит из чтения. Открытая подсказка сноски забирает Escape себе
  // (useNoteXrefs останавливает всплытие), поэтому здесь проверяется, не
  // занят ли единственный слот поповера: иначе один нажатый Escape закрыл бы
  // и подсказку, и весь экран.
  useEffect(() => {
    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape' && !getActivePopover()) navigate(backHref);
    };
    window.addEventListener('keydown', onKey);
    return () => window.removeEventListener('keydown', onKey);
  }, [navigate, backHref]);

  // Доля прочитанного считается по работе, а не по прокрутке: scrollHeight в
  // растущем потоке всегда «почти конец».
  const progressPercent =
    stream.totalPages > 0 && visiblePage !== null
      ? Math.min(100, Math.round((visiblePage / stream.totalPages) * 100))
      : 0;

  const sentinelRef = useRef<HTMLDivElement>(null);
  // Выделение двух окон потока лежит внутри этого узла целиком — по нему
  // CiteButton находит и проверяет наличие выделения, и по нему же читает его
  // в момент нажатия.
  const contentRef = useRef<HTMLElement>(null);
  const { hasMore, loadMore } = stream;

  useInfiniteSentinel(sentinelRef, hasMore, loadMore, `0px 0px ${PREFETCH_MARGIN_PX}px 0px`);

  // Якорь полосы в потоке — сам адрес потока: хэш для пересылки не годится,
  // окна догружаются от полосы из маршрута, и у получателя ссылки нужной
  // секции в документе нет. Стабильная ссылка через useCallback — ReadingChunk
  // мемоизирует разбор своих полос по ней же.
  const pageHref = useCallback(
    (n: number) => readPath({ id: numericWorkId, slug: work?.slug }, n),
    [numericWorkId, work?.slug],
  );

  // Данные для «Цитировать»/«Ссылка»: дерево глав уже загружено (chapters, для
  // бегущего заголовка), поэтому второго запроса ради подписи не нужно.
  // Произведение подписи — неаппаратная глава ВЕРХНЕГО уровня, накрывающая
  // полосу, тем же правилом, что и у ChapterView.
  const citation = useMemo(
    () =>
      work
        ? {
            work,
            workTitleFor: (n: number) =>
              chapterLevelsForPage(chapters, n)[0]?.find((c) => !c.is_apparatus)?.title ?? '',
            // Из потока чтения цитата ведёт на ПОЛОСУ, а не на адрес потока
            // (`/read/{n}`), и это названная граница, а не недосмотр: поток
            // пары якорей не разбирает вовсе — ни ?quote=, ни &to= он не
            // читает, подсветки в нём нет, — и ссылка на него привела бы
            // читателя в нужное место молча без подсветки цитаты. Полоса
            // подсвечивает и говорит вслух, если текст правили.
            quoteHref: (n: number, search: string) =>
              `${pagePath({ id: numericWorkId, slug: work.slug }, n)}${search}`,
          }
        : undefined,
    [work, chapters, numericWorkId],
  );

  // Переход к странице из панели. Подгруженная полоса — прокрутка; адрес
  // поправит сам поток, он следует за видимой полосой. Остальная —
  // перезапуск потока с неё: вверх поток не растёт, а дотягивать окна до
  // полосы за сотни страниц значило бы грузить всё, что между. Полосы вне
  // глав здесь законны (поток идёт по тому целиком), поэтому граница одна —
  // число полос тома.
  const jumpToPage = useCallback(
    async (input: string): Promise<string | null> => {
      const pageNumber = work ? parsePrintedPage(input, work) : null;
      if (pageNumber === null || pageNumber > stream.totalPages) {
        return missingPageMessage(input);
      }
      const loaded = stream.windows.some((w) =>
        w.pages.some((page) => page.page_number === pageNumber),
      );
      if (loaded && jumpToAnchor(pageAnchorId(pageNumber))) return null;
      window.scrollTo(0, 0);
      navigate(readPath({ id: numericWorkId, slug: work?.slug }, pageNumber));
      return null;
    },
    [work, stream.totalPages, stream.windows, navigate, numericWorkId],
  );

  if (!valid) {
    return (
      <div className="error-state">
        <div>Неверный адрес чтения</div>
        <Link to="/" className="back-link">
          ← На главную
        </Link>
      </div>
    );
  }

  if (contextError) {
    return (
      <div className="error-state">
        <div>{contextError}</div>
        <Link to={workPath({ id: numericWorkId, slug: work?.slug })} className="back-link">
          ← К тому
        </Link>
      </div>
    );
  }

  // Первое окно ещё летит: показывать нечего. Ошибка на первом окне — это
  // отказ экрана, а не полоска внизу: текста, который она бы дополняла, нет.
  if (stream.isLoading || !work) {
    return <div className="loading-state">Загрузка…</div>;
  }

  if (stream.error && stream.windows.length === 0) {
    return (
      <div className="error-state">
        <div>{stream.error}</div>
        <Link to={workPath(work)} className="back-link">
          ← К тому
        </Link>
      </div>
    );
  }

  return (
    <div className="work-read">
      <ReadingStreamBar
        chapterTitle={runningChapter?.title ?? null}
        progressPercent={progressPercent}
        backHref={backHref}
        showPageNumbers={showPageNumbers}
        onTogglePageNumbers={() => setPref('pageNumbers', !showPageNumbers)}
        suggestHref={suggestHref}
        scanHref={scanHref}
        citation={citation}
        contentRef={contentRef}
        visiblePage={visiblePage}
        pageJump={{
          currentLabel:
            visiblePage !== null ? (printedFolio(visiblePage, work) ?? String(visiblePage)) : null,
          onJump: jumpToPage,
          roman: work.numbering_style === 'roman',
        }}
      />
      <article className="work-read-content" ref={contentRef}>
        {stream.windows.map((loaded) => (
          <ReadingChunk
            key={loaded.from}
            pages={loaded.pages}
            work={work}
            pageHref={pageHref}
            showPageInfo={showPageNumbers}
            highlightTerms={highlightTerms}
            scrollToFirstHit={loaded.from === startPage}
          />
        ))}
      </article>

      {/* Сентинель стоит под текстом; его появление в кадре с запасом в два
          экрана тянет следующее окно. */}
      <div ref={sentinelRef} className="work-read-sentinel" aria-hidden="true" />

      {stream.isLoadingMore && (
        <div className="work-read-more" role="status">
          Загрузка…
        </div>
      )}

      {stream.error && stream.windows.length > 0 && (
        <div className="work-read-error" role="alert">
          <span>{stream.error}</span>
          <button type="button" className="btn btn-secondary" onClick={stream.retry}>
            Повторить
          </button>
        </div>
      )}

      {!stream.hasMore && !stream.error && (
        <div className="work-read-end">
          <span>Конец работы</span>
          <Link to={workPath(work)} className="back-link">
            К оглавлению тома
          </Link>
        </div>
      )}
    </div>
  );
};
