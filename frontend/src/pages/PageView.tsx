import { useEffect, useState, useRef } from 'react';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { pagesApi, worksApi } from '../services/api';
import { useAuth, canEdit } from '../hooks/useAuth';
import { useCanonicalPath } from '../hooks/useCanonicalPath';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import type { Work } from '../types';
import { getPreviewUrl } from '../utils/url';
import 'katex/dist/katex.min.css';
import './PageView.css';
import { useNoteXrefs } from '../hooks/useNoteXrefs';
import { useFootnotePreview } from '../hooks/useFootnotePreview';
import { usePageByNumber } from '../hooks/usePageByNumber';
import { useChaptersForPage } from '../hooks/useChaptersForPage';
import { useKatex } from '../hooks/useKatex';
import { useSearchTerms } from '../hooks/useSearchTerms';
import { useSearchHighlight } from '../hooks/useSearchHighlight';
import { useQuoteHighlight } from '../hooks/useQuoteHighlight';
import { useWorkPageMap } from '../hooks/useWorkPageMap';
import { usePageKeyboardNav } from '../hooks/usePageKeyboardNav';
import { ChapterBreadcrumb } from '../components/ChapterBreadcrumb';
import { PageConcepts } from '../components/PageConcepts';
import { PagePager } from '../components/PagePager';
import { ScanViewer } from '../components/ScanViewer';
import { CiteButton } from '../components/CiteButton';
import { QuoteNotice } from '../components/QuoteNotice';
import { apiErrorMessage } from '../utils/apiError';
import { printedFolio } from '../utils/folio';
import { pageNeighbours } from '../utils/pageNeighbours';
import { pageStatusLabel } from '../utils/pageStatusLabel';
import { russianDate } from '../utils/russianDate';
import { numericId, pagePath, workPath } from '../utils/paths';

type PageContent =
  | { pageId: number; status: 'ok'; work: Work; html: string }
  | { pageId: number; status: 'error'; message: string };

export const PageView: React.FC = () => {
  // workId остаётся сырым сегментом адреса — usePageByNumber/useChaptersForPage
  // сами делают Number(workId) внутри себя (frontend/src/hooks/usePageByNumber.ts,
  // useChaptersForPage.ts) — на слагованном сегменте это дало бы NaN, поэтому
  // туда передаётся уже очищенная строка с разобранным номером. Ссылки этого
  // экрана строятся билдерами из utils/paths ниже.
  const { workId, pageNumber } = useParams<{ workId: string; pageNumber: string }>();
  const parsedWorkId = numericId(workId);
  const cleanWorkId = parsedWorkId !== null ? String(parsedWorkId) : undefined;
  const { user } = useAuth();
  const editable = canEdit(user);
  const {
    page,
    isLoading: isPageLoading,
    notFound,
    error: pageError,
  } = usePageByNumber(cleanWorkId, pageNumber);
  const { levels: chapterLevels } = useChaptersForPage(cleanWorkId, page?.page_number);
  const [content, setContent] = useState<PageContent | null>(null);
  const contentRef = useRef<HTMLDivElement>(null);

  // Содержимое годится, только если оно от текущей страницы: при переходе на
  // соседнюю прошлый текст не должен мелькать до прихода нового.
  const current = page && content?.pageId === page.id ? content : null;
  const isLoading = isPageLoading || (page !== null && current === null);
  const work = current?.status === 'ok' ? current.work : null;
  const htmlContent = current?.status === 'ok' ? current.html : '';
  const error = current?.status === 'error' ? current.message : '';

  // Печатный номер — как на бумаге и как считает internal/seo/render_text.go
  // (ReadPage): page_number + page_offset, а не голый номер из адреса.
  useDocumentTitle(
    content?.status === 'ok'
      ? `${content.work.title}, с. ${Number(pageNumber) + content.work.page_offset}`
      : null,
  );

  // Заход по старой числовой ссылке тихо подменяется каноном, как только
  // работа приехала и слаг стал известен.
  useCanonicalPath(work && page ? pagePath(work, page.page_number) : null);

  // Рендер требует ID страницы, поэтому ждёт резолва номера; загрузка работы
  // могла бы идти параллельно резолву, но экономия — один запрос из трёх,
  // а так вся дозагрузка живёт в одном эффекте.
  useEffect(() => {
    if (isPageLoading || !page) return;

    const pageId = page.id;
    let cancelled = false;

    Promise.all([worksApi.get(page.work_id), pagesApi.render(page.work_id, page.id)])
      .then(([workResponse, renderResponse]) => {
        if (cancelled) return;
        setContent({
          pageId,
          status: 'ok',
          work: workResponse.data,
          html: renderResponse.data.html,
        });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setContent({
          pageId,
          status: 'error',
          message: apiErrorMessage(err, 'Не удалось загрузить страницу'),
        });
      });

    return () => {
      cancelled = true;
    };
  }, [page, isPageLoading]);

  // Полоса, открытая из выдачи поиска, несёт ?q= и подсвечивает найденное —
  // тем же хуком, что режим чтения и глава. Без q не делает ничего: ни запроса
  // за леммами, ни обхода DOM.
  const [searchParams] = useSearchParams();
  const highlightTerms = useSearchTerms(searchParams.get('q') ?? '');
  // Внешняя ссылка на место в тексте несёт пару якорей — ?quote= (начало) и
  // &to= (конец): та же полоса, что и у q=, но точная цитата вместо лемм, и с
  // озвученным промахом при несовпадении. Без &to= подсветится одно начало —
  // так приходят ссылки, розданные до этой работы, и адреса вырезок указателя.
  const quote = searchParams.get('quote') ?? '';
  const quoteTo = searchParams.get('to') ?? '';

  useKatex(contentRef, [htmlContent]);
  useNoteXrefs(contentRef, parsedWorkId ?? undefined, work?.slug, [htmlContent]);
  useFootnotePreview(contentRef, [htmlContent]);
  useSearchHighlight(contentRef, highlightTerms, true, [htmlContent]);
  // page ещё может быть null на первом рендере (хуки идут раньше ранних
  // return-ов ниже) — сужение по номеру полосы тут не декорация, а условие
  // правильности (см. useQuoteHighlight), поэтому передаём его как есть.
  const quoteMatch = useQuoteHighlight(
    contentRef,
    { start: quote, end: quoteTo },
    page?.page_number,
    [htmlContent],
  );

  const { numbers } = useWorkPageMap(page?.work_id);
  const neighbours = pageNeighbours(numbers, page?.page_number ?? 0);
  // Пока открыт полноэкранный просмотр скана, стрелками возят увеличенную
  // картинку в ScanViewer, а не листают страницы работы под ним — иначе
  // навигация может размонтировать оверлей вместе со сканом (если у соседней
  // страницы нет превью), в обход его собственного close().
  const [scanOpen, setScanOpen] = useState(false);
  usePageKeyboardNav(
    neighbours.prev !== null
      ? pagePath({ id: parsedWorkId ?? 0, slug: work?.slug }, neighbours.prev)
      : null,
    neighbours.next !== null
      ? pagePath({ id: parsedWorkId ?? 0, slug: work?.slug }, neighbours.next)
      : null,
    !scanOpen,
  );

  if (isLoading) {
    return <div className="loading-state">Загрузка страницы…</div>;
  }

  if (notFound) {
    return (
      <div className="error-state">
        <div>Страница {pageNumber} не найдена в этой работе</div>
        <Link
          to={parsedWorkId !== null ? workPath({ id: parsedWorkId, slug: work?.slug }) : '/'}
          className="back-link"
        >
          ← К работе
        </Link>
      </div>
    );
  }

  if (error || pageError || !page || !work) {
    return (
      <div className="error-state">
        <div>{error || pageError || 'Страница не найдена'}</div>
        <Link
          to={parsedWorkId !== null ? workPath({ id: parsedWorkId, slug: work?.slug }) : '/'}
          className="back-link"
        >
          ← Назад
        </Link>
      </div>
    );
  }

  // Prefer the API-provided presigned URL; fall back to the legacy path helper.
  const previewUrl = page.preview_url ?? getPreviewUrl(page.preview_path) ?? undefined;

  // У тома offset нулевой, колонцифра совпадает с номером — дублировать её
  // незачем. Показываем только там, где печатный счёт расходится с адресным,
  // либо там, где колонцифры нет вовсе (обложка вне счёта — «б/н»).
  const folio = printedFolio(page.page_number, work);
  const folioLabel: string | null =
    folio === null ? 'б/н' : folio !== String(page.page_number) ? `стр. ${folio}` : null;

  return (
    <div className="page-view-container">
      <div className="page-view-header">
        <Link to={workPath(work)} className="back-link">
          ← {work.title}
        </Link>
        <div className="page-view-header-actions">
          <PagePager work={work} neighbours={neighbours} variant="top" />
          {/* Неаппаратная глава верхнего уровня, накрывающая полосу, — тот же
              выбор подписи, что и в потоковом чтении (ChapterView/WorkRead). */}
          <CiteButton
            contentRef={contentRef}
            work={work}
            workTitleFor={() => chapterLevels[0]?.find((c) => !c.is_apparatus)?.title ?? ''}
            // Со страницы со сканом цитата ведёт на полосу: сюда цитирующий
            // и приходит сверить текст со снимком печатной страницы, а скан
            // лежит только здесь (решение 15 спеки).
            quoteHref={(n, search) => `${pagePath(work, n)}${search}`}
            visiblePage={page.page_number}
            className="btn btn-secondary btn-sm"
          />
          <Link to={`${pagePath(work, page.page_number)}/suggest`} className="suggest-link">
            Предложить исправление
          </Link>
          {editable && (
            <Link
              to={`${pagePath(work, page.page_number)}/edit`}
              className="btn btn-primary btn-sm"
            >
              Править
            </Link>
          )}
        </div>
      </div>

      <div className="page-info-card">
        <h1>
          Страница {page.page_number}
          {folioLabel !== null && <span className="page-folio"> · {folioLabel}</span>}
        </h1>
        <div className="page-meta">
          <div className="meta-item">
            <span className="meta-label">Работа:</span>
            <Link to={workPath(work)} className="meta-link">
              {work.title}
            </Link>
          </div>
          {work.author && (
            <div className="meta-item">
              <span className="meta-label">Автор:</span>
              <span className="meta-value">{work.author}</span>
            </div>
          )}
          {work.volume_number !== undefined && work.volume_number !== null && (
            <div className="meta-item">
              <span className="meta-label">В томе:</span>
              <span className="meta-value">
                печатная стр. {page.page_number + (work.page_offset ?? 0)}
              </span>
            </div>
          )}
          {chapterLevels.length > 0 && (
            <div className="meta-item">
              <span className="meta-label">Главы:</span>
              <ChapterBreadcrumb work={work} levels={chapterLevels} />
            </div>
          )}
          <div className="meta-item">
            <span className="meta-label">Статус:</span>
            <span className={`page-status status-${page.status}`}>
              {pageStatusLabel(page.status)}
            </span>
          </div>
          {page.text_edited_at && (
            <div className="meta-item">
              <span className="meta-label">Правлено:</span>
              <span className="meta-value">{russianDate(page.text_edited_at)}</span>
            </div>
          )}
        </div>
      </div>

      <div className="page-view-two-columns">
        <div className="page-view-left">
          <div className="content-section">
            <h2>Содержимое</h2>
            <QuoteNotice match={quoteMatch} editedAt={page.text_edited_at} />
            <div
              ref={contentRef}
              className="page-html-content"
              data-page={page.page_number}
              dangerouslySetInnerHTML={{ __html: htmlContent }}
            />
          </div>
          {/* Сервер и без этой проверки отвечает [] работе без обеих координат —
              не отправляем этот запрос впустую на самом частом экране проекта:
              сегодня это большинство работ. */}
          {work.volume_number != null && work.edition_id != null && (
            <PageConcepts workId={page.work_id} pageId={page.id} />
          )}
        </div>

        <div className="page-view-right">
          {previewUrl && (
            <div className="preview-section">
              <h2>Скан страницы</h2>
              <ScanViewer
                src={previewUrl}
                alt={`Страница ${page.page_number}`}
                onOpenChange={setScanOpen}
              />
            </div>
          )}
        </div>
      </div>

      <PagePager work={work} neighbours={neighbours} variant="bottom" />
    </div>
  );
};
