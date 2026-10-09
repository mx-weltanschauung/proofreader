import { useCallback, useEffect, useState, useRef, useMemo } from 'react';
import { useParams, Link, useNavigate, useSearchParams } from 'react-router-dom';
import toast from 'react-hot-toast';
import { chaptersApi, worksApi, cacheApi } from '../services/api';
import { useAuth, canEdit } from '../hooks/useAuth';
import type { Chapter, ChapterPage, Work } from '../types';
import { ChapterTocDrawer } from '../components/ChapterTocDrawer';
import { ChapterBreadcrumb } from '../components/ChapterBreadcrumb';
import { ReadingSurface } from '../components/ReadingSurface';
import { ReadingProgressBar } from '../components/ProgressTicks';
import { DownloadMenu } from '../components/DownloadMenu';
import { AudioPanel } from '../components/AudioPanel';
import { AudioQueueControl } from '../components/AudioQueueControl';
import { RecordingManager } from '../components/RecordingManager';
import { useWorkAudio } from '../hooks/useWorkAudio';
import { recordingsForChapter, tracksForRange } from '../utils/audio';
import { AskAiButton } from '../components/AskAiButton';
import { ShareButton } from '../components/ShareButton';
import { askAiPrompt } from '../utils/askAi';
import {
  chapterInApparatus,
  chapterPath,
  findChapterById,
  siblingNeighbours,
} from '../utils/chapterTree';
import './ChapterView.css';
import {
  chapterPath as chapterAddress,
  pagePath,
  readPath,
  workPath,
  numericId,
} from '../utils/paths';
import { useReadingProgress } from '../hooks/useReadingProgress';
import { useCanonicalPath } from '../hooks/useCanonicalPath';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { useAnchorAboveFold } from '../hooks/useAnchorAboveFold';
import { chapterLevelsForPage } from '../hooks/useChaptersForPage';
import { citationPlace } from '../utils/citationPlace';
import { useSearchTerms } from '../hooks/useSearchTerms';
import { apiErrorMessage } from '../utils/apiError';
import { useScrollDockAction } from '../contexts/scrollDockContext';
import { CreditLinks } from '../components/CreditLinks';
import { articleKindLabel, authorsOf } from '../utils/credits';
import { chapterTypeLabel } from '../utils/chapterTypeLabel';
import { pageAnchorId } from '../utils/pageAnchor';
import { jumpToAnchor, markAddress } from '../utils/anchorNav';
import { plural } from '../utils/volumeLabel';
import { missingPageMessage, parsePrintedPage, resolvePageJump } from '../utils/pageJump';
import { printedFolio } from '../utils/folio';

export const ChapterView: React.FC = () => {
  // workParam/chapterParam — сырые сегменты адреса (несут слаг); workId и
  // chapterId ниже — разобранные номера для сравнений и вызовов API.
  const { workId: workParam, chapterId: chapterParam } = useParams<{
    workId: string;
    chapterId: string;
  }>();
  const workId = numericId(workParam);
  const chapterId = numericId(chapterParam);
  const [searchParams] = useSearchParams();
  const highlightTerms = useSearchTerms(searchParams.get('q') ?? '');
  // Внешняя ссылка на место в тексте несёт пару якорей: ?quote= — начало
  // цитаты (он же ключ адреса), &to= — её конец. Подсвечивается всё между
  // ними; ReadingSurface сам сузит поиск по номеру полосы из хэша адреса.
  // Якоря конца нет у ссылок, розданных до этой работы, и у внешних адресов
  // вырезок указателя (там хранится только голова) — тогда подсветится начало.
  const quote = searchParams.get('quote') ?? '';
  const quoteTo = searchParams.get('to') ?? '';
  const { user } = useAuth();
  const [chapter, setChapter] = useState<Chapter | null>(null);
  const [work, setWork] = useState<Work | null>(null);
  const [allChapters, setAllChapters] = useState<Chapter[]>([]);
  const [pagesWithHtml, setPagesWithHtml] = useState<ChapterPage[]>([]);
  const [footnotesHtml, setFootnotesHtml] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  const [isDroppingCache, setIsDroppingCache] = useState(false);
  // Панель «Слушать» раскрыта у конкретной главы: ChapterView не размонтируется
  // при переходе на соседнюю, и булев флаг перевёз бы панель (пустой рамкой у
  // главы без звука, со строками заливки прежней главы — у сотрудника).
  const [listenFor, setListenFor] = useState<number | null>(null);
  const listenOpen = listenFor !== null && listenFor === chapterId;
  const { audio, failed: audioFailed, reload: reloadAudio } = useWorkAudio(workId);
  // Звук тома неизвестен вовсе (первое чтение упало) — в отличие от упавшего
  // перечитывания, при котором прежний звук остаётся на экране.
  const audioUnknown = audioFailed && audio === null;
  const [reloadingAudio, setReloadingAudio] = useState(false);
  const retryAudio = async () => {
    setReloadingAudio(true);
    try {
      await reloadAudio();
    } finally {
      setReloadingAudio(false);
    }
  };
  const retryAudioButton = (
    <button
      type="button"
      className="btn btn-secondary btn-sm"
      disabled={reloadingAudio}
      onClick={() => void retryAudio()}
    >
      {reloadingAudio ? 'Загружаю…' : 'Загрузить снова'}
    </button>
  );
  // Наблюдение за видимой страницей ведёт ReadingSurface — секции порождает
  // она. Сюда номер приходит обратным вызовом: по нему подсвечивается шторка
  // подглав и пишется запись «где читатель остановился».
  const [visiblePage, setVisiblePage] = useState<number | null>(null);
  const toolbarRef = useRef<HTMLDivElement>(null);

  // Порядок и разделитель — те же, что печатает internal/seo/render_text.go
  // (Chapter): название вперёд, автор следом, и без лишнего тире, если автора
  // нет вовсе.
  useDocumentTitle(
    chapter
      ? authorsOf(chapter.credits) || work?.author
        ? `${chapter.title} — ${authorsOf(chapter.credits) || work?.author}`
        : chapter.title
      : null,
  );

  // Заход по старой числовой ссылке тихо подменяется каноном, как только
  // работа и глава приехали и слаги стали известны.
  //
  // Сравнение с разобранными workId/chapterId обязательно: ChapterView не
  // размонтируется между главами (Route тот же, меняется только :chapterId),
  // и переход на соседнюю главу — свой же push через navigate() или <Link> —
  // на один тик опережает ответ сервера. Без сравнения канон строился бы по
  // ещё не смытому `chapter`/`work` прежней главы и совпадал бы с прежним
  // адресом, а не с новым — эффект тут же откатывал бы адрес обратно (see
  // `dockAction` выше: та же защита от рассинхронизации на тот же случай).
  useCanonicalPath(
    work && chapter && work.id === workId && chapter.id === chapterId
      ? chapterAddress(work, chapter)
      : null,
  );

  useEffect(() => {
    const loadData = async () => {
      // Битый сегмент работы или главы (numericId вернул null): грузить
      // нечего, но флаг загрузки обязан сброситься — иначе экран вечно висит
      // на «Загрузка главы…» вместо уже существующего экрана отказа ниже
      // (условие там уже проверяет workId === null / chapterId === null).
      // Сброс — внутри асинхронной функции, а не прямо в теле эффекта:
      // react-hooks/set-state-in-effect запрещает синхронный setState там.
      if (workId === null || chapterId === null) {
        setIsLoading(false);
        return;
      }

      try {
        const [chapterResponse, workResponse, pagesResponse, chaptersResponse] = await Promise.all([
          chaptersApi.get(workId, chapterId),
          worksApi.get(workId),
          chaptersApi.listPages(workId, chapterId),
          chaptersApi.list(workId),
        ]);

        setChapter(chapterResponse.data);
        setWork(workResponse.data);
        setPagesWithHtml(pagesResponse.data?.pages || []);
        setFootnotesHtml(pagesResponse.data?.footnotes_html || '');
        setAllChapters(chaptersResponse.data || []);
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить главу'));
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void loadData();
  }, [workId, chapterId]);

  // Кэш главы держится час и путей инвалидации не имеет — эта кнопка и есть
  // вторая половина того размена. Перезапрос обязателен: без него редактор
  // нажал бы, получил «готово» и продолжал смотреть прежний текст из
  // состояния компонента, то есть кнопка выглядела бы сломанной, будучи
  // исправной.
  const dropChapterCache = useCallback(async () => {
    // workId и chapterId здесь уже разобранные номера (см. numericId выше),
    // поэтому parseInt не нужен — и не был бы верен на сегменте со слагом.
    if (workId === null || chapterId === null) return;
    setIsDroppingCache(true);
    try {
      await cacheApi.purgeChapter(workId, chapterId);
      const pagesResponse = await chaptersApi.listPages(workId, chapterId);
      setPagesWithHtml(pagesResponse.data.pages || []);
      setFootnotesHtml(pagesResponse.data.footnotes_html || '');
      toast.success('Кэш главы сброшен');
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось сбросить кэш главы'));
    } finally {
      setIsDroppingCache(false);
    }
  }, [workId, chapterId]);

  // Соседи — только сёстры. Плоский обход всего тома выдавал за «следующую»
  // главу первого ребёнка открытой.
  const { prev: prevChapter, next: nextChapter } = useMemo(
    () => siblingNeighbours(allChapters, chapterId ?? -1),
    [allChapters, chapterId],
  );

  // Предки открытой главы: сама глава стоит заголовком ниже, поэтому хвост
  // цепочки отбрасывается. Каждый предок — отдельный уровень, так включается
  // встроенная свёртка середины у ChapterBreadcrumb: в томе 3 цепочка
  // доходит до одиннадцати уровней.
  const ancestors = useMemo(
    () => chapterPath(allChapters, chapterId ?? -1).slice(0, -1),
    [allChapters, chapterId],
  );
  const lastAncestor = ancestors[ancestors.length - 1];

  // ChapterBreadcrumb сбрасывает свою свёртку при каждом новом `levels`
  // (ChapterBreadcrumb.tsx:31-33) — без мемоизации сюда уходил бы новый
  // массив на каждый рендер ChapterView (а он перерисовывается на каждый
  // скролл через useReadingProgress), и раскрытая читателем цепочка
  // схлопывалась бы обратно при первой же прокрутке.
  const crumbLevels = useMemo(() => ancestors.map((c) => [c]), [ancestors]);

  const navigate = useNavigate();

  // Стабильный адрес маркера страницы: по нему мемоизирована склейка полос в
  // ReadingSurface, а ChapterView перерисовывается на каждый кадр прокрутки
  // (useReadingProgress) — новая стрелка означала бы разбор html всей главы
  // на каждый из них.
  // Маркер полосы — якорь на её начало в читаемой сейчас главе: голый
  // фрагмент, который и <Link>, и браузер разворачивают в полный адрес
  // текущей главы. Присланная такая ссылка открывает главу на нужной полосе
  // (прокрутку делает useHashAnchor), а не уводит на отдельный экран полосы —
  // он переехал в панель чтения.
  const pageHref = useCallback((pageNumber: number) => `#${pageAnchorId(pageNumber)}`, []);

  // Адрес подглавы для строки оглавления и плавающего дока — якорь на её
  // первую полосу. Точность до полосы: своего id у заголовка подглавы в
  // вёрстке нет, подглава может начинаться и в середине печатной полосы.
  const subChapterHref = useCallback(
    (target: Chapter) => `#${pageAnchorId(target.start_page)}`,
    [],
  );

  // Дети берутся из дерева тома, а не из ответа chaptersApi.get: список глав —
  // единственный источник, где вложенность заведомо заполнена.
  const subChapters = useMemo(
    () => findChapterById(allChapters, chapterId ?? -1)?.children ?? [],
    [allChapters, chapterId],
  );

  // Узел главы из дерева тома: у ответа chaptersApi.get детей нет, а записи
  // подглав принадлежат и главе (спека, «Ручные записи»).
  const chapterNode = useMemo(
    () => findChapterById(allChapters, chapterId ?? -1) ?? chapter,
    [allChapters, chapterId, chapter],
  );
  const chapterTracks = useMemo(
    () =>
      audio && chapter ? tracksForRange(audio.tracks, chapter.start_page, chapter.end_page) : [],
    [audio, chapter],
  );
  const chapterRecordings = useMemo(
    () => (audio && chapterNode ? recordingsForChapter(audio.recordings, chapterNode) : []),
    [audio, chapterNode],
  );
  const hasAudio = chapterTracks.length > 0 || chapterRecordings.length > 0;

  // Самая глубокая подглава, накрывающая видимую страницу.
  // chapterLevelsForPage уже умеет стыковые страницы, где совпадают сёстры:
  // берём первую из последнего уровня.
  const activeSubChapterId = useMemo(() => {
    if (visiblePage === null) return null;
    const levels = chapterLevelsForPage(subChapters, visiblePage);
    return levels[levels.length - 1]?.[0]?.id ?? null;
  }, [subChapters, visiblePage]);

  // Текст всех подглав уже отрендерен на этой странице, поэтому переход —
  // прокрутка. Точность до страницы: подглава может начинаться в середине
  // печатной полосы. Если якоря нет (битые диапазоны глав), уходим на
  // отдельный адрес подглавы, чтобы клик не пропал впустую.
  // Обёрнут в useCallback не ради самого вызова, а ради memo на шторке: новая
  // функция на каждом рендере страницы сбрасывала бы сравнение пропсов, и
  // список подглав реконсилился бы всё равно.
  const jumpToSubChapter = useCallback(
    (target: Chapter) => {
      const anchor = pageAnchorId(target.start_page);
      // jumpToAnchor переносит фокус на секцию вместе с прокруткой: шторка
      // размонтируется вместе с кнопкой-переключателем, на которой стоял
      // фокус, и без переноса клавиатурный читатель теряет позицию, а
      // скринридер не получает сигнала, что страница прокрутилась.
      if (!jumpToAnchor(anchor)) {
        // Выше раннего возврата: work ещё не сужен типом до не-null, поэтому
        // адрес строится из того, что уже известно — тот же приём, что и в
        // chapterCrumbs ниже.
        navigate(chapterAddress({ id: workId ?? 0, slug: work?.slug }, target));
        return;
      }
      // Место отмечается в адресе, но записи в истории не заводит: прыжок по
      // оглавлению — это перемещение внутри читаемой главы, и «назад» после
      // десятка таких прыжков не должно десять раз никуда не вести.
      markAddress(subChapterHref(target));
    },
    [navigate, workId, work?.slug, subChapterHref],
  );

  // Переход к странице из панели. Полоса этой главы — прокрутка с отметкой в
  // адресе, тем же приёмом, что у оглавления подглав: перемещение внутри
  // читаемого текста записей в истории не заводит. Полоса вне главы — уже
  // переход, с записью: «назад» вернёт к прежней главе. Не накрытая ни одной
  // главой (содержание, колофон) открывается отдельной полосой, и только
  // тогда нужна карта полос тома — отличить такую от несуществующей.
  const jumpToPage = useCallback(
    async (input: string): Promise<string | null> => {
      if (!work || workId === null) return missingPageMessage(input);
      const pageNumber = parsePrintedPage(input, work);
      if (pageNumber === null) return missingPageMessage(input);
      const target = resolvePageJump(pageNumber, allChapters, (n) =>
        pagesWithHtml.some((page) => page.page_number === n),
      );
      const anchor = pageAnchorId(pageNumber);
      if (target.kind === 'here') {
        jumpToAnchor(anchor);
        markAddress(`#${anchor}`);
        return null;
      }
      if (target.kind === 'chapter') {
        navigate(`${chapterAddress(work, target.chapter)}#${anchor}`);
        return null;
      }
      try {
        const map = await worksApi.pageMap(workId);
        if (!map.data.some((entry) => entry.page_number === pageNumber)) {
          return missingPageMessage(input);
        }
      } catch {
        // Карта не пришла — проверять нечем, но и отказывать не в чем: экран
        // полосы сам скажет, если её нет.
      }
      navigate(pagePath(work, pageNumber));
      return null;
    },
    [work, workId, allChapters, pagesWithHtml, navigate],
  );

  const activeSubChapter = useMemo(
    () => subChapters.find((c) => c.id === activeSubChapterId) ?? null,
    [subChapters, activeSubChapterId],
  );

  // Условие показа геометрическое, а не постраничное. Сравнение
  // `visiblePage > start_page` невыполнимо для подглавы в одну страницу — а
  // такова каждая седьмая листовая подглава в загруженных томах, в томе 6 —
  // больше половины: страница у неё одна, и стоит видимой странице стать
  // следующей, как активной становится уже следующая подглава.
  const activeAnchorId = useMemo(
    () => (activeSubChapter ? pageAnchorId(activeSubChapter.start_page) : null),
    [activeSubChapter],
  );
  const subChapterStartAboveFold = useAnchorAboveFold(activeAnchorId, toolbarRef);

  // Кнопка нужна, только когда есть куда прыгать: подглава известна и её
  // начало уже ушло за кромку читаемого текста. Пока начало на экране, кнопка
  // сдвинула бы текст на пару строк и сбила бы чтение.
  //
  // Первое условие — про окно загрузки. subChapters берутся из дерева тома по
  // :chapterId и меняются синхронно с адресом, а страницы приходят сетевым
  // ответом, и всё это время в DOM висит текст прежней главы (isLoading назад
  // в true не возвращается). Один круг до сервера подпись принадлежала бы
  // главе, в которую читатель только входит, при том что на экране ещё та,
  // которую он покидает, — а на спуске в потомка диапазоны заведомо
  // пересекаются, так что подпись находилась бы всегда. Врать о месте хуже,
  // чем молчать: пока загруженная глава не совпала с адресом, кнопки нет.
  const dockAction = useMemo(() => {
    if (!chapter || chapter.id !== chapterId) return null;
    if (!activeSubChapter || !subChapterStartAboveFold) return null;
    return {
      label: activeSubChapter.title,
      href: subChapterHref(activeSubChapter),
      onActivate: () => jumpToSubChapter(activeSubChapter),
    };
  }, [
    chapter,
    chapterId,
    activeSubChapter,
    subChapterStartAboveFold,
    jumpToSubChapter,
    subChapterHref,
  ]);

  useScrollDockAction(dockAction);

  const progress = useReadingProgress({
    workId: workId ?? undefined,
    chapterId: chapterId ?? undefined,
    workTitle: work?.title,
    chapterTitle: chapter?.title,
    pageNumber: visiblePage,
    // Признак «на экране именно та глава, что в адресе», а не просто «текст
    // есть»: ChapterView не размонтируется между главами, isLoading назад в
    // true не возвращается, и один круг до сервера в DOM висят полосы главы,
    // которую читатель покидает (та же защита, что у dockAction выше). Без
    // сверки позиция прокрутки прежней главы записалась бы под ключ новой, а
    // восстановление сохранённой позиции пришлось бы на чужой ещё текст — и
    // упёрлось бы в его высоту.
    ready: !isLoading && pagesWithHtml.length > 0 && chapter?.id === chapterId,
  });

  // Цепочка получает признак сжатия панели: сжатую полоску полная цепочка
  // растянула бы обратно в две строки, ради чего её и сжимают.
  const chapterCrumbs = useCallback(
    (condensed: boolean) => (
      <>
        <span aria-hidden="true">← </span>
        {condensed && ancestors.length > 0 ? (
          // Остаётся только ближайший предок.
          <Link
            to={chapterAddress({ id: workId ?? 0, slug: work?.slug }, lastAncestor)}
            className="back-link"
          >
            {lastAncestor.title}
          </Link>
        ) : (
          <>
            <Link to={workPath({ id: workId ?? 0, slug: work?.slug })} className="back-link">
              {work?.title}
            </Link>
            {ancestors.length > 0 && (
              <>
                <span className="chapter-view-crumb-sep" aria-hidden="true">
                  {' '}
                  /{' '}
                </span>
                <ChapterBreadcrumb
                  work={workId !== null ? { id: workId, slug: work?.slug } : undefined}
                  levels={crumbLevels}
                />
              </>
            )}
          </>
        )}
      </>
    ),
    [ancestors, lastAncestor, workId, work?.slug, work?.title, crumbLevels],
  );

  if (isLoading) {
    return <div className="loading-state">Загрузка главы…</div>;
  }

  // workId === null / chapterId === null сюда практически никогда не доходят
  // отдельно от !chapter (главу без разобранных номеров загрузчик и не
  // запускал), но добавлены в условие ради сужения типов: дальше по функции
  // workId и chapterId используются как обычные number, без запасных `?? `.
  if (error || !chapter || !work || workId === null || chapterId === null) {
    return (
      <div className="error-state">
        <div>{error || 'Глава не найдена'}</div>
        <Link
          to={workId !== null ? workPath({ id: workId, slug: work?.slug }) : '/'}
          className="back-link"
        >
          ← Назад
        </Link>
      </div>
    );
  }

  // Один узел на оба места: ReadingSurface рисует навигацию и над текстом, и
  // под ним.
  const chapterNav = (prevChapter || nextChapter) && (
    <nav className="chapter-nav" aria-label="Переход по главам">
      {prevChapter ? (
        <Link to={chapterAddress(work, prevChapter)} className="chapter-nav-prev">
          ← {prevChapter.title}
        </Link>
      ) : (
        <span />
      )}
      {nextChapter && (
        <Link to={chapterAddress(work, nextChapter)} className="chapter-nav-next">
          {nextChapter.title} →
        </Link>
      )}
    </nav>
  );

  return (
    <>
      <ReadingProgressBar percent={progress} />
      <ReadingSurface
        pages={pagesWithHtml}
        footnotesHtml={footnotesHtml}
        work={work}
        progressPercent={progress}
        pageJump={{
          currentLabel:
            visiblePage !== null ? (printedFolio(visiblePage, work) ?? String(visiblePage)) : null,
          onJump: jumpToPage,
          roman: work.numbering_style === 'roman',
        }}
        toolbarRef={toolbarRef}
        onVisiblePageChange={setVisiblePage}
        pageHref={pageHref}
        crumbs={chapterCrumbs}
        highlightTerms={highlightTerms}
        quote={{ start: quote, end: quoteTo }}
        citation={{
          work,
          // Цитата, снятая в главе, ведёт В ГЛАВУ — туда, где читатель был, —
          // с якорем полосы, на которой цитата началась. Якорь обязателен:
          // ReadingSurface сужает поиск цитаты по нему, и без него подсветки
          // не будет вовсе. Цена этой формы названа в спеке (решение 15):
          // перестройка глав меняет id, и присланная ссылка ответит 410, —
          // номер полосы в якоре остаётся единственным, что от неё уцелеет.
          quoteHref: (n, search) => `${chapterAddress(work, chapter)}${search}#${pageAnchorId(n)}`,
          // Произведение и автор подписи — один помощник на все поверхности
          // (citationPlace): у тома неаппаратная глава верхнего уровня,
          // накрывающая полосу; у номера журнала — самая глубокая статья,
          // сперва внутри читаемой главы (рубрика ведёт к своей статье, на
          // стыке двух статей побеждает читаемая).
          workTitleFor: (n) => citationPlace(work, allChapters, n, chapterNode).workTitle,
          authorFor: (n) => citationPlace(work, allChapters, n, chapterNode).author,
        }}
        toolbarExtra={
          <ChapterTocDrawer
            work={work}
            chapters={subChapters}
            onJump={jumpToSubChapter}
            hrefFor={subChapterHref}
            activeChapterId={activeSubChapterId}
          />
        }
        header={
          <header className="chapter-header">
            {chapter.credits && chapter.credits.length > 0 && (
              <p className="chapter-credits">
                <CreditLinks credits={chapter.credits} />
              </p>
            )}
            {(() => {
              // У номера журнала глава без вида статьи — рубрика
              // («Критика и библиография»), а не «глава» тома.
              const badge = chapter.article_kind
                ? articleKindLabel(chapter.article_kind)
                : work.journal_issue
                  ? 'рубрика'
                  : chapterTypeLabel(chapter.type);
              return badge ? <div className="chapter-type-badge">{badge}</div> : null;
            })()}
            <h1>{chapter.title}</h1>
            <div className="chapter-meta">
              <span>
                Страницы {chapter.start_page}—{chapter.end_page}
              </span>
              <span>{plural(pagesWithHtml.length, ['страница', 'страницы', 'страниц'])}</span>
            </div>
            <div className="chapter-header-actions">
              <Link to={readPath(work, chapter.start_page)} className="btn btn-secondary">
                Читать потоком
              </Link>
              {(hasAudio || canEdit(user)) && (
                <button
                  type="button"
                  className="btn btn-secondary"
                  aria-expanded={listenOpen}
                  onClick={() => setListenFor(listenOpen ? null : chapterId)}
                >
                  Слушать
                </button>
              )}
              {/* Бэкенд строго числовой (см. спеку) — адрес API строится из
                  разобранного workId, а не из сырого сегмента маршрута
                  (workParam несёт слаг: /works/49-lenin-t06/chapters/...). */}
              <DownloadMenu
                href={`/api/works/${workId}/chapters/${chapter.id}/download`}
                audio={hasAudio}
              />
              <AskAiButton prompt={() => askAiPrompt(work, chapter, window.location.origin)} />
              {/* Глава целиком, а не видимая полоса — ту отдаёт «Ссылка» в
                  панели чтения. Автор у томов корпуса пуст и стоит в самом
                  названии тома, поэтому подпись — глава и том. */}
              <ShareButton
                title={`${chapter.title} — ${work.title}`}
                path={chapterAddress(work, chapter)}
              />
              {canEdit(user) && (
                <>
                  <button
                    type="button"
                    className="btn btn-secondary"
                    onClick={dropChapterCache}
                    disabled={isDroppingCache}
                  >
                    {isDroppingCache ? 'Сбрасываю…' : 'Сбросить кэш главы'}
                  </button>
                  {/* Без звука тома дорожки главы неизвестны, и кнопка
                      звала бы озвучить заново уже озвученное. */}
                  {audioUnknown ? (
                    <span className="audio-queue-state">Звук тома не загрузился</span>
                  ) : (
                    <AudioQueueControl
                      workId={workId}
                      chapter={chapter}
                      apparatus={chapterInApparatus(allChapters, chapter)}
                      chapterTracks={chapterTracks}
                      onChanged={reloadAudio}
                    />
                  )}
                </>
              )}
            </div>
            {listenOpen && audioUnknown && canEdit(user) && (
              // Пустой список записей сотрудник прочёл бы как «нет записей» и
              // прикрепил бы повторно то, что уже лежит.
              <div className="audio-panel">
                <p role="alert" className="audio-panel-unsupported">
                  Звук не загрузился — какие записи у главы уже есть, сейчас не видно.{' '}
                  {retryAudioButton}
                </p>
              </div>
            )}
            {listenOpen && !audioUnknown && chapterNode && work && (
              <AudioPanel
                key={chapterNode.id}
                work={work}
                audio={audio ?? { tracks: [], recordings: [] }}
                chapter={chapterNode}
                playlistHref={`/api/works/${workId}/chapters/${chapter.id}/download?format=m3u`}
                allChapters={allChapters}
                tracks={chapterTracks}
                recordings={chapterRecordings}
                staffSlot={
                  canEdit(user) ? (
                    <>
                      {audioFailed && (
                        <p role="alert" className="audio-panel-unsupported">
                          Звук не перечитался — списки выше могут быть неполными. {retryAudioButton}
                        </p>
                      )}
                      <RecordingManager
                        workId={workId}
                        chapterId={chapter.id}
                        recordings={chapterRecordings.filter((r) => r.chapter_id === chapter.id)}
                        onChanged={reloadAudio}
                      />
                    </>
                  ) : undefined
                }
              />
            )}
          </header>
        }
        nav={chapterNav}
      />
    </>
  );
};
