import { useCallback, useEffect, useMemo, useRef, useState } from 'react';
import { useParams, Link } from 'react-router-dom';
import { collectionsApi } from '../services/api';
import type { ChapterPage, Collection } from '../types';
import { ReadingSurface } from '../components/ReadingSurface';
import { ReadingProgressBar } from '../components/ProgressTicks';
import './CollectionRead.css';
import { useReadingProgress } from '../hooks/useReadingProgress';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { plural } from '../utils/volumeLabel';
import { sourceLabel } from '../utils/sourceLabel';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { pageAnchorId } from '../utils/pageAnchor';
import { collectionPath, collectionReadPath } from '../utils/paths';

export const CollectionRead: React.FC = () => {
  // nickname есть только у длинного (читательского) адреса — см. paths.ts.
  const { nickname, slug, itemId } = useParams<{
    nickname?: string;
    slug: string;
    itemId: string;
  }>();
  const [collection, setCollection] = useState<Collection | null>(null);
  const [pagesWithHtml, setPagesWithHtml] = useState<ChapterPage[]>([]);
  const [footnotesHtml, setFootnotesHtml] = useState('');
  // Какой записи принадлежат лежащие в состоянии полосы. Сам :itemId для
  // этого не годится: он меняется в тот же миг, что и адрес, а полосы
  // приезжают сетевым ответом кругом позже.
  const [loadedItemId, setLoadedItemId] = useState<number | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [error, setError] = useState('');
  // 410 — отдельный случай, не общая ошибка: источник элемента удалён из
  // читальни, а не «не получилось загрузить».
  const [sourceRemoved, setSourceRemoved] = useState(false);
  // Наблюдение за видимой страницей ведёт ReadingSurface. Сюда номер приходит
  // обратным вызовом: по нему пишется запись «где читатель остановился».
  const [visiblePage, setVisiblePage] = useState<number | null>(null);
  const toolbarRef = useRef<HTMLDivElement>(null);

  // Заголовок — у подборки, а не у читаемого пункта: сервер
  // (internal/seo/handler.go, маршрут collections/{slug}/read/{id}) отдаёт
  // краулеру тот же Doc, что и сама подборка (Collection), только noindex.
  // Взять здесь entry.title вместо collection.title означало бы разойтись с
  // тем, что видит поисковик, на индексируемой странице.
  useDocumentTitle(collection ? `${collection.title} — подборка` : null);

  useEffect(() => {
    if (!slug || !itemId) return;

    const load = async () => {
      try {
        const [collectionResponse, pagesResponse] = await Promise.all([
          collectionsApi.get(slug, nickname),
          collectionsApi.itemPages(slug, Number(itemId), nickname),
        ]);

        setCollection(collectionResponse.data);
        setPagesWithHtml(pagesResponse.data?.pages || []);
        setFootnotesHtml(pagesResponse.data?.footnotes_html || '');
        setLoadedItemId(Number(itemId));
      } catch (err: unknown) {
        if (apiErrorStatus(err) === 410) {
          setSourceRemoved(true);
        } else {
          setError(apiErrorMessage(err, 'Не удалось загрузить элемент подборки'));
        }
      } finally {
        setIsLoading(false);
      }
    };

    // Загрузчик забирает свои ошибки во внутреннем catch и не реджектится,
    // поэтому промис здесь честно отбрасывается, а не проглатывается.
    void load();
  }, [slug, itemId, nickname]);

  const items = collection?.items ?? [];
  const index = items.findIndex((i) => i.id === Number(itemId));
  const entry = index >= 0 ? items[index] : null;
  // Соседи — по позиции в составе подборки, а не по главам тома-источника:
  // «дальше» может увести в другую книгу другого автора, в этом и смысл
  // подборки.
  const prevEntry = index > 0 ? items[index - 1] : null;
  const nextEntry = index >= 0 && index < items.length - 1 ? items[index + 1] : null;

  // Работа-источник и адрес маркера страницы держатся стабильными: по ним
  // мемоизирована склейка полос в ReadingSurface, а она разбирает html всей
  // записи. Новый объект на каждый рендер означал бы этот разбор на каждый
  // кадр прокрутки. Ноль в id недостижим — до него компонент выходит по
  // раннему возврату ниже.
  const sourceWorkId = entry?.source?.work_id;
  const sourceWorkSlug = entry?.source?.work_slug;
  const readingWork = useMemo(
    () => ({ id: sourceWorkId ?? 0, slug: sourceWorkSlug, page_offset: 0 }),
    [sourceWorkId, sourceWorkSlug],
  );
  // Маркер полосы — якорь на её начало в читаемой сейчас записи подборки:
  // голый фрагмент, который и <Link>, и браузер разворачивают в полный адрес.
  // Отдельный экран полосы переехал в панель чтения (ReadingSurface).
  const pageHref = useCallback((pageNumber: number) => `#${pageAnchorId(pageNumber)}`, []);

  // Место чтения хранится по source.work_id и entry.id — отдельно от позиции
  // в самом томе (там ключ по chapterId тома), чтобы одно не затирало другое.
  const progress = useReadingProgress({
    workId: entry?.source?.work_id,
    chapterId: entry?.id,
    workTitle: collection?.title,
    chapterTitle: entry?.title,
    pageNumber: visiblePage,
    // Признак «на экране именно та запись, что в адресе», а не просто «текст
    // есть»: CollectionRead не размонтируется между записями, isLoading назад
    // в true не возвращается, и круг до сервера в DOM висят полосы записи,
    // которую читатель покидает. Без сверки её смещение записалось бы под
    // ключ новой, а восстановление сохранённой позиции пришлось бы на чужой
    // ещё текст.
    ready: !isLoading && pagesWithHtml.length > 0 && loadedItemId === entry?.id,
    // Запись «последнее прочитанное» адресует главу тома ссылкой
    // /works/{workId}/chapters/{chapterId} (Dashboard, VolumeMasthead).
    // entry.id — id строки состава подборки, а не id главы; попади он в
    // общую запись, «Продолжить» вело бы на несуществующую или чужую главу.
    // Позиция прокрутки внутри подборки всё равно сохраняется — не трогаем
    // только глобальный виджет.
    trackLastRead: false,
  });

  // Адрес назад к подборке не ждёт сети: nickname/slug уже есть в строке
  // адреса, а collectionPath строит по ним ту же ссылку, что и сервер
  // (paths.ts). Пустой slug (адрес ещё не разобран роутером) откатывается
  // на каталог.
  const backToCollection = slug
    ? collectionPath({ slug, author_nickname: nickname })
    : '/collections';

  const crumbs = useCallback(
    () => (
      <>
        <span aria-hidden="true">← </span>
        <Link to={backToCollection} className="back-link">
          {collection?.title}
        </Link>
      </>
    ),
    [backToCollection, collection?.title],
  );

  if (isLoading) {
    return <div className="loading-state">Загрузка…</div>;
  }

  if (sourceRemoved) {
    return (
      <div className="error-state">
        <div>Источник удалён из читальни — эту запись больше нечем читать.</div>
        <Link to={backToCollection} className="back-link">
          ← К подборке
        </Link>
      </div>
    );
  }

  if (error || !collection || !entry || !entry.source) {
    return (
      <div className="error-state">
        <div>{error || 'Элемент подборки не найден'}</div>
        <Link to={backToCollection} className="back-link">
          ← Назад
        </Link>
      </div>
    );
  }

  // Один узел на оба места: ReadingSurface рисует навигацию и над текстом, и
  // под ним.
  const collectionNav = (prevEntry || nextEntry) && (
    <nav className="collection-read-nav" aria-label="Переход по подборке">
      {prevEntry ? (
        <Link
          to={collectionReadPath(collection, prevEntry.id)}
          className="collection-read-nav-prev"
        >
          ← {prevEntry.title}
        </Link>
      ) : (
        <span />
      )}
      {nextEntry && (
        <Link
          to={collectionReadPath(collection, nextEntry.id)}
          className="collection-read-nav-next"
        >
          {nextEntry.title} →
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
        // page_offset нулевой намеренно: печатные номера уже посчитаны
        // сервером и лежат в source — второе применение смещения сдвинуло бы
        // колонцифры вдвое.
        work={readingWork}
        progressPercent={progress}
        toolbarRef={toolbarRef}
        onVisiblePageChange={setVisiblePage}
        pageHref={pageHref}
        crumbs={crumbs}
        emptyState={<p>Страницы этой записи ещё не выложены.</p>}
        header={
          <header className="collection-read-header">
            <h1>{entry.title}</h1>
            <div className="collection-read-meta">
              {entry.author && <span>{entry.author}</span>}
              <span>{sourceLabel(entry)}</span>
              <span>{plural(pagesWithHtml.length, ['страница', 'страницы', 'страниц'])}</span>
            </div>
          </header>
        }
        nav={collectionNav}
      />
    </>
  );
};
