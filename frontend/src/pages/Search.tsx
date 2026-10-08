import { Fragment, useCallback, useEffect, useState } from 'react';
import { Link, useNavigate, useSearchParams } from 'react-router-dom';
import { searchApi, worksApi } from '../services/api';
import type {
  SearchResponse,
  SearchPage,
  SearchVolume,
  SearchChapterFacet,
  Work,
  Shelf,
} from '../types';
import { SearchTrigger } from '../components/SearchTrigger';
import { ChapterFacet } from '../components/ChapterFacet';
import {
  searchPath,
  parseScope,
  singleWorkOf,
  isEmptyScope,
  type SearchScope,
} from '../utils/searchScope';
import { loadShelfOnce } from '../utils/shelfCache';
import { splitSnippet } from '../utils/snippet';
import { printedFolio } from '../utils/folio';
import { orderVolumes } from '../utils/searchVolumes';
import { plural } from '../utils/volumeLabel';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { apiErrorMessage } from '../utils/apiError';
import { isTooShortQuery, TOO_SHORT_MESSAGE } from '../utils/searchQuery';
import { chapterPath, pagePath } from '../utils/paths';
import './Search.css';

export const PAGE_SIZE = 50;

/**
 * Ссылка на найденную полосу. Ведёт на саму полосу, а не в окно потока
 * (/read/): читатель пришёл за этой полосой, а поток открыл бы её посреди
 * соседей и увёл бы адрес на ту, до которой он долистает. q едет дальше —
 * по нему полоса подсвечивает найденное.
 */
function pageHref(work: { id: number; slug?: string }, page: number, q: string): string {
  return `${pagePath(work, page)}?${new URLSearchParams({ q }).toString()}`;
}

/**
 * Колонцифра полосы в списке томов. Работы под рукой нет и быть не может —
 * томов в выдаче бывает больше сотни, — но офсет сервер уже применил
 * (`printed_number`), так что печатной форме остаются римские цифры и «б/н»
 * ниже единицы. Та же printedFolio, что на карточке полосы и в обрезе тома.
 */
function volumeFolioLabel(p: SearchPage, v: SearchVolume): string {
  const folio = printedFolio(p.printed_number, {
    page_offset: 0,
    numbering_style: v.numbering_style,
  });
  return folio === null ? 'б/н' : `с. ${folio}`;
}

/**
 * Область строки тома в выдаче: сам том, а внутри собрания — ещё и собрание
 * (то же двойное сужение, что раньше собирал третий параметр старого
 * searchPath — withinEdition).
 */
function volumeScope(workId: number, editionId: number | null): SearchScope {
  return { editions: editionId !== null ? [editionId] : [], works: [workId], chapters: [] };
}

function volumeRow(v: SearchVolume, q: string, editionId: number | null) {
  const child = v.parent_work_id !== null;
  const total = v.text_hits + v.apparatus_hits;
  return (
    <li key={v.work_id} className={`search-volume ${child ? 'is-child' : ''}`}>
      <Link to={searchPath(q, volumeScope(v.work_id, editionId))} className="search-volume-link">
        {v.title}
        {child && <span className="search-volume-role"> — передние листы</span>}
      </Link>
      <span className="search-volume-meta">
        {v.author && <span>{v.author}, </span>}
        {v.volume_label && v.volume_label !== v.title && <span>{v.volume_label}, </span>}
        {v.edition_title && <span>{v.edition_title}</span>}
      </span>
      <span className="search-volume-hits">
        {plural(v.text_hits, ['полоса', 'полосы', 'полос'])}
        {v.apparatus_hits > 0 && (
          <span className="search-volume-apparatus">, {v.apparatus_hits} в аппарате</span>
        )}
      </span>
      {v.pages.length > 0 && (
        <ol className="search-page-list">
          {v.pages.map((p) => (
            <PageHit
              key={p.page_number}
              work={{ id: v.work_id, slug: v.work_slug }}
              q={q}
              page={p}
              folio={volumeFolioLabel(p, v)}
            />
          ))}
        </ol>
      )}
      {total > v.pages.length && (
        <Link to={searchPath(q, volumeScope(v.work_id, editionId))} className="search-volume-more">
          все {plural(total, ['полоса', 'полосы', 'полос'])} →
        </Link>
      )}
    </li>
  );
}

function Snippet({ text }: { text: string }) {
  return (
    <p className="search-page-snippet">
      {splitSnippet(text).map((part, i) =>
        part.hit ? (
          <mark key={i} className="search-hit">
            {part.text}
          </mark>
        ) : (
          <Fragment key={i}>{part.text}</Fragment>
        ),
      )}
    </p>
  );
}

/**
 * Одна найденная полоса: колонцифра-ссылкой, ближайшая глава и отрывок. Один
 * компонент на оба места, где полосы показываются, — строку тома в общей
 * выдаче и список полос внутри тома: форма ответа у них одна (SearchPage), и
 * выглядеть они обязаны одинаково. Колонцифру считает вызывающий: в томе она
 * берётся из работы, в списке томов — из самой строки тома.
 */
function PageHit({
  work,
  q,
  page,
  folio,
}: {
  work: { id: number; slug?: string };
  q: string;
  page: SearchPage;
  folio: string;
}) {
  return (
    <li className={`search-page ${page.is_apparatus ? 'is-apparatus' : ''}`}>
      <Link to={pageHref(work, page.page_number, q)} className="search-page-number">
        {folio}
      </Link>
      {page.chapter_title &&
        (page.chapter_id === null ? (
          <span className="search-page-chapter">{page.chapter_title}</span>
        ) : (
          // SearchPage не несёт слаг главы (в отличие от SearchChapter в
          // общем каталоге ниже) — сервер его для этой строки не шлёт,
          // адрес выходит с голым номером главы.
          <Link to={chapterPath(work, { id: page.chapter_id })} className="search-page-chapter">
            {page.chapter_title}
          </Link>
        ))}
      <Snippet text={page.snippet} />
    </li>
  );
}

/**
 * Состояние загрузки привязано к ключу запроса, а не сбрасывается setState
 * прямо в эффекте: react-hooks 7 (set-state-in-effect) такой сброс запрещает.
 * Ответ с чужим ключом — устаревший, он не рисуется.
 */
interface PagesState {
  key: string;
  pages: SearchPage[];
  total: number;
  error: string;
  /** «Ещё» уже в пути — второй клик не должен уйти отдельным запросом. */
  loadingMore: boolean;
  /** Фасет «где нашлось» — приходит только первым запросом окна, «Ещё» его не трогает. */
  chapters: SearchChapterFacet[];
  chaptersTotal: number;
}

/**
 * Колонцифра полосы в выдаче. Работа приходит вторым запросом, поэтому до её
 * ответа печатается сырая арифметика сервера (`page_number + page_offset`), а
 * как только работа известна — общая для всей читальни `printedFolio`: у
 * передних листов счёт римский, а ниже единицы (обложка вне счёта) колонцифры
 * нет вовсе — «б/н», как на карточке полосы.
 */
function folioLabel(p: SearchPage, work: Work | null): string {
  if (!work) return `с. ${p.printed_number}`;
  const folio = printedFolio(p.page_number, work);
  return folio === null ? 'б/н' : `с. ${folio}`;
}

/**
 * Полосы одного тома (состояние 3). Область принимается целиком (`scope`), а
 * не только главами: фасет умеет менять лишь одну ось (`scope.chapters`), но
 * адрес после клика обязан пронести и остальные — иначе действие по одной
 * оси области стирает другую (см. правку ниже, `toggleChapter`).
 */
function VolumePages({
  q,
  workId,
  scope,
  onChapterTitles,
}: {
  q: string;
  workId: number;
  scope: SearchScope;
  // Находка 3 итоговой рецензии: заголовки глав из фасета нужны не только
  // самому фасету, но и чипам строки области (ScopeBar) — им неоткуда взять
  // название иначе, кроме как получить его отсюда, из того же ответа.
  onChapterTitles: (chapters: SearchChapterFacet[]) => void;
}) {
  const chapters = scope.chapters;
  const key = `${q}|${workId}|${chapters.join(',')}`;
  const navigate = useNavigate();
  const [loadedWork, setLoadedWork] = useState<Work | null>(null);
  const [state, setState] = useState<PagesState | null>(null);
  // Работа от прошлого тома не годится: её page_offset посчитал бы чужую
  // колонцифру. Сверка по id — та же защита, что ключ запроса у полос.
  const work = loadedWork && loadedWork.id === workId ? loadedWork : null;

  useEffect(() => {
    let cancelled = false;
    worksApi
      .get(workId)
      .then((res) => {
        if (!cancelled) setLoadedWork(res.data);
      })
      .catch(() => {});
    searchApi
      .pages({
        q,
        work_id: workId,
        limit: PAGE_SIZE,
        offset: 0,
        ...(chapters.length > 0 ? { chapters } : {}),
      })
      .then((res) => {
        if (!cancelled) {
          setState({
            key,
            pages: res.data.pages,
            total: res.data.total,
            error: '',
            loadingMore: false,
            chapters: res.data.chapters,
            chaptersTotal: res.data.chapters_total,
          });
          onChapterTitles(res.data.chapters);
        }
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            key,
            pages: [],
            total: 0,
            error: apiErrorMessage(err, 'Поиск не удался'),
            loadingMore: false,
            chapters: [],
            chaptersTotal: 0,
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [q, workId, chapters, key, onChapterTitles]);

  const current = state?.key === key ? state : null;

  const loadMore = () => {
    // current.loadingMore защищает от двойного клика: без него два клика до
    // ответа первого запроса читают один и тот же current.pages.length и
    // оба просят одно окно офсетов — список задваивается вместе с ключами.
    if (!current || current.loadingMore) return;
    setState((prev) => (prev && prev.key === key ? { ...prev, loadingMore: true } : prev));
    searchApi
      .pages({
        q,
        work_id: workId,
        limit: PAGE_SIZE,
        offset: current.pages.length,
        ...(chapters.length > 0 ? { chapters } : {}),
      })
      .then((res) =>
        setState((prev) =>
          prev && prev.key === key
            ? {
                ...prev,
                pages: [...prev.pages, ...res.data.pages],
                loadingMore: false,
                error: '',
              }
            : prev,
        ),
      )
      .catch((err: unknown) =>
        setState((prev) =>
          prev && prev.key === key
            ? { ...prev, loadingMore: false, error: apiErrorMessage(err, 'Поиск не удался') }
            : prev,
        ),
      );
  };

  // Клик по фасету — переход по адресу, а не смена состояния компонента:
  // область поиска живёт в адресе целиком, иначе выдачу с выбранными главами
  // нельзя переслать ссылкой. Адрес собирается из ВСЕЙ входящей области
  // (`{...scope, chapters: next}`), а не только из глав и тома: фасет меняет
  // одну ось (главы), остальные оси (собрание — и любая, что заведётся
  // позже) обязаны пройти через клик нетронутыми.
  const toggleChapter = (id: number) => {
    const next = chapters.includes(id) ? chapters.filter((c) => c !== id) : [...chapters, id];
    navigate(searchPath(q, { ...scope, chapters: next }));
  };

  // Ошибка без единой уже загруженной полосы — то же положение, что и в
  // Overview: сказать только об отказе, не рисовать поверх «ничего не
  // найдено» (сейчас или изначально — total в этой ветке всегда 0). Ошибку
  // от «Ещё», когда полосы уже есть, оставляем баннером над списком —
  // ранний return здесь стоил бы читателю доступа к уже найденному и самой
  // кнопки повтора. Строка области (кто такой этот том, какое собрание рядом)
  // теперь рисуется один раз в ScopeBar над обеими ветками — здесь ей не место.
  if (current?.error && current.pages.length === 0) {
    return (
      <section className="search-pages">
        <div className="error-message">{current.error}</div>
      </section>
    );
  }

  return (
    <section className="search-pages">
      {current?.error && <div className="error-message">{current.error}</div>}
      {!current ? (
        <p className="search-status">Ищем…</p>
      ) : (
        <>
          <h2 className="search-pages-total">
            {plural(current.total, ['полоса', 'полосы', 'полос'])}
          </h2>
          {current.total === 0 && <p className="search-status">В этом томе ничего не найдено.</p>}
          <ChapterFacet
            chapters={current.chapters}
            total={current.chaptersTotal}
            selected={chapters}
            onToggle={toggleChapter}
          />
          <ol className="search-page-list">
            {current.pages.map((p) => (
              <PageHit
                key={p.page_number}
                work={{ id: workId, slug: work?.slug }}
                q={q}
                page={p}
                folio={folioLabel(p, work)}
              />
            ))}
          </ol>
          {current.pages.length < current.total && (
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={loadMore}
              disabled={current.loadingMore}
            >
              Ещё
            </button>
          )}
        </>
      )}
    </section>
  );
}

interface OverviewState {
  key: string;
  data: SearchResponse | null;
  error: string;
}

/**
 * Каталог и тома (состояния 1 и 2). Ключ запроса — та же защита от setState в
 * эффекте. Область приходит целиком (а не разобранной на workId/editionId):
 * строку области теперь рисует ScopeBar в корневом компоненте, Overview
 * только зовёт searchApi.search(q, scope) и передаёт editionId ниже, в
 * volumeRow, чтобы ссылка на том сохраняла собрание.
 */
function Overview({ q, scope }: { q: string; scope: SearchScope }) {
  const key = `${q}|${JSON.stringify(scope)}`;
  const [state, setState] = useState<OverviewState | null>(null);

  useEffect(() => {
    let cancelled = false;
    searchApi
      .search(q, scope)
      .then((res) => {
        if (!cancelled) setState({ key, data: res.data, error: '' });
      })
      .catch((err: unknown) => {
        if (!cancelled)
          setState({ key, data: null, error: apiErrorMessage(err, 'Поиск не удался') });
      });
    return () => {
      cancelled = true;
    };
  }, [q, scope, key]);

  const current = state?.key === key ? state : null;
  if (current?.error) return <div className="error-message">{current.error}</div>;
  if (!current?.data) return <p className="search-status">Ищем…</p>;
  const data = current.data;
  // Единственное собрание в области сопровождает ссылки на тома внутри
  // выдачи (volumeRow) — тем же значением, каким эта выдача была построена.
  const editionId = scope.editions.length === 1 ? scope.editions[0] : null;

  const empty = data.chapters.length + data.concepts.length + data.volumes.length === 0;
  return (
    <>
      {empty && (
        <p className="search-status">
          Ничего не найдено. Фразу целиком ищите в кавычках: «что делать».
        </p>
      )}
      {(data.chapters.length > 0 || data.concepts.length > 0) && (
        <section className="search-catalog">
          {data.concepts.length > 0 && (
            <>
              <h2>Понятия</h2>
              <ul className="search-catalog-list">
                {data.concepts.map((c) => (
                  <li key={c.slug}>
                    <Link to={`/concepts/${encodeURIComponent(c.slug)}`}>{c.title}</Link>
                  </li>
                ))}
              </ul>
            </>
          )}
          {data.chapters.length > 0 && (
            <>
              <h2>Главы</h2>
              <ul className="search-catalog-list">
                {data.chapters.map((c) => (
                  <li key={c.id} className={c.is_apparatus ? 'is-apparatus' : ''}>
                    <Link
                      to={chapterPath(
                        { id: c.work_id, slug: c.work_slug },
                        { id: c.id, slug: c.slug },
                      )}
                    >
                      {c.title}
                    </Link>
                    <span className="search-catalog-meta">
                      {' '}
                      — {c.work_title}
                      {c.volume_label && c.volume_label !== c.work_title && `, ${c.volume_label}`}
                      {c.edition_title && `, ${c.edition_title}`}
                    </span>
                  </li>
                ))}
              </ul>
            </>
          )}
        </section>
      )}
      {data.volumes.length > 0 && (
        <section className="search-volumes">
          <h2>В тексте — {plural(data.total_hits, ['полоса', 'полосы', 'полос'])}</h2>
          <ul className="search-volume-list">
            {orderVolumes(data.volumes).map((v) => volumeRow(v, q, editionId))}
          </ul>
        </section>
      )}
    </>
  );
}

/** Заголовок собрания по id — из уже загруженной полки, без отдельного запроса. */
function shelfEditionTitle(shelf: Shelf | null, id: number): string | null {
  return shelf?.editions.find((e) => e.edition.id === id)?.edition.title ?? null;
}

/** Заголовок тома по id — том бывает и в собрании, и вне его (loose_works). */
function shelfWorkTitle(shelf: Shelf | null, id: number): string | null {
  if (!shelf) return null;
  for (const e of shelf.editions) {
    const found = e.volumes.find((v) => v.id === id);
    if (found) return found.title;
  }
  return shelf.loose_works.find((w) => w.id === id)?.title ?? null;
}

/**
 * Один чип строки области — название и крестик, снимающий ровно этот
 * элемент. Крестик — обычная ссылка на searchPath с уменьшенной областью
 * (область живёт в адресе, снятие — это переход, а не состояние компонента);
 * если снимаемый элемент был последним, `searchPath` сам не пишет пустые
 * параметры области — снятие последнего чипа равнозначно «Сбросить» без
 * отдельной ветки. Доступное имя крестика называет, что именно снимается
 * («Снять том «…»», не голый «✕») — иначе список из трёх одинаковых
 * крестиков неразличим для читалки экрана.
 */
function ScopeChip({ text, removeLabel, to }: { text: string; removeLabel: string; to: string }) {
  return (
    <span className="scope-chip">
      {text}
      <Link to={to} className="scope-chip-remove" aria-label={removeLabel}>
        ✕
      </Link>
    </span>
  );
}

/**
 * Строка области поиска — одна вместо прежних двух отдельных плашек
 * («в собрании: … ✕» у обзора и «в томе: … ← ко всем томам» у полос тома):
 * оба места показывали одно и то же (что выбрано в адресе), а правили это
 * по-разному, каждое своей ссылкой. Здесь — общий вид: чипсы выбранного
 * (собрания/тома/главы), каждый со своим крестиком (см. ScopeChip), и два
 * общих действия — «Изменить» открывает ТУ ЖЕ панель поиска, что и общая
 * затравка (SearchTrigger, второго способа выбрать область не заводим),
 * «Сбросить» ведёт на чистый searchPath(q).
 *
 * Крестик у чипа тома восстанавливает путь, которым выборка одним кликом
 * возвращалась «ко всем томам собрания»: до этой правки такой ссылки не было
 * вовсе, и снять один том, оставив собрание, было нечем — только через
 * «Изменить» → снять галочку → «Найти». Тот же приём разом решает и то, чего
 * старая ссылка не умела, — снять один лишний том из области в несколько.
 *
 * Названия собраний и томов берутся из полки (loadShelfOnce, тот же кэш, что
 * у ScopePicker) — до её ответа чип печатает «…». Названия глав берутся из
 * фасета «где нашлось» (chapterTitles, накопленный VolumePages по мере
 * ответов searchApi.pages) — своего источника у ScopeBar для них нет, а
 * заводить второй запрос ради одних заголовков ради чипа было бы дороже
 * самого чипа. Находка 3 итоговой рецензии: до этой правки глава в строке
 * области печаталась голым `глава ${id}` — внутренним ключом БД, который
 * читателю ничего не говорит. Фасет отдаёт не более 30 глав по числу
 * совпадений (facetLimit на бэкенде) — если выбранной главы среди них нет
 * (переход по сохранённой ссылке на главу, которая перестала быть в топе),
 * заголовок остаётся неизвестным и чип возвращается к прежнему виду с
 * номером, а не зависает на «…» навсегда: в отличие от полки, второй попытки
 * узнать название у фасета не будет.
 */
function ScopeBar({
  q,
  scope,
  chapterTitles,
}: {
  q: string;
  scope: SearchScope;
  chapterTitles: Record<number, string>;
}) {
  const empty = isEmptyScope(scope);
  const [shelf, setShelf] = useState<Shelf | null>(null);

  useEffect(() => {
    if (empty) return;
    let cancelled = false;
    loadShelfOnce()
      .then((s) => {
        if (!cancelled) setShelf(s);
      })
      .catch(() => {});
    return () => {
      cancelled = true;
    };
  }, [empty]);

  if (empty) return null;

  return (
    <div className="scope-bar">
      <span className="scope-bar-label">Область поиска:</span>
      {scope.editions.map((id) => {
        const title = shelfEditionTitle(shelf, id) ?? '…';
        return (
          <ScopeChip
            key={`edition-${id}`}
            text={title}
            removeLabel={`Снять собрание «${title}» из области поиска`}
            to={searchPath(q, { ...scope, editions: scope.editions.filter((e) => e !== id) })}
          />
        );
      })}
      {scope.works.map((id) => {
        const title = shelfWorkTitle(shelf, id) ?? '…';
        return (
          <ScopeChip
            key={`work-${id}`}
            text={title}
            removeLabel={`Снять том «${title}» из области поиска`}
            to={searchPath(q, { ...scope, works: scope.works.filter((w) => w !== id) })}
          />
        );
      })}
      {scope.chapters.map((id) => {
        const title = chapterTitles[id] ?? `глава ${id}`;
        return (
          <ScopeChip
            key={`chapter-${id}`}
            text={title}
            removeLabel={`Снять главу «${title}» из области поиска`}
            to={searchPath(q, { ...scope, chapters: scope.chapters.filter((c) => c !== id) })}
          />
        );
      })}
      <span className="scope-bar-actions">
        <SearchTrigger initialQuery={q} scope={scope} label="Изменить" />
        <Link to={searchPath(q)} className="scope-bar-reset">
          Сбросить
        </Link>
      </span>
    </div>
  );
}

export const Search: React.FC = () => {
  const [params] = useSearchParams();
  const q = (params.get('q') ?? '').trim();
  // Круг правок 1, находка 1: после перевода ссылок на множественные
  // works=/editions= (searchScope.ts) голое params.get('work')/('edition')
  // читало null на любой свежей ссылке — сужение терялось, читатель попадал
  // в общий обзор вместо полос тома. parseScope понимает и новый, и старый
  // (легаси work=/edition=) формат сама, без отдельной ветки здесь.
  const scope = parseScope(params);
  // Поправка спеки от 18.09.2026 (см. searchScope.ts): singleWorkOf теперь
  // показывает полосы единственного тома независимо от того, выбрано ли ещё
  // и собрание рядом — собрание остаётся контекстом строки области (ScopeBar
  // ниже), а не условием, гасящим показ полос. Старый комментарий здесь
  // объяснял, почему singleWorkOf «не годится» — это было верно для
  // отменённого правила и вместе с ним отменено.
  const workId = singleWorkOf(scope);

  // Находка 3 итоговой рецензии: заголовки глав нужны и ScopeBar (чипы), и
  // они же уже приезжают в ответе VolumePages (фасет «где нашлось») — здесь
  // просто копилка id → title, общая для обоих. useCallback с пустыми
  // зависимостями и функциональной формой setState — без него ссылка на
  // колбэк менялась бы на каждый рендер Search и попадала в зависимости
  // эффекта VolumePages, вызывая его заново без всякой связи со сменой
  // q/workId/chapters.
  const [chapterTitles, setChapterTitles] = useState<Record<number, string>>({});
  const rememberChapterTitles = useCallback((chapters: SearchChapterFacet[]) => {
    if (chapters.length === 0) return;
    setChapterTitles((prev) => {
      let changed = false;
      const next = { ...prev };
      for (const c of chapters) {
        if (next[c.id] !== c.title) {
          next[c.id] = c.title;
          changed = true;
        }
      }
      return changed ? next : prev;
    });
  }, []);

  useDocumentTitle(q ? `Поиск: ${q}` : 'Поиск');

  return (
    <div className="search-container">
      <h1>Поиск</h1>
      <SearchTrigger initialQuery={q} scope={scope} />
      <p className="search-help-link">
        <Link to="/help#search">Как искать →</Link>
      </p>
      {q === '' ? (
        <p className="search-status">Введите слово, имя или «фразу в кавычках».</p>
      ) : /* Форма такой запрос не пропустит, но сюда приходят и по прямой
             ссылке — из письма, закладок, адресной строки. Без этой ветки
             читатель увидел бы отказ сервера, а не объяснение. */
      isTooShortQuery(q) ? (
        <p className="search-status">{TOO_SHORT_MESSAGE}</p>
      ) : (
        <>
          <ScopeBar q={q} scope={scope} chapterTitles={chapterTitles} />
          {workId !== null ? (
            <VolumePages
              q={q}
              workId={workId}
              scope={scope}
              onChapterTitles={rememberChapterTitles}
            />
          ) : (
            <Overview q={q} scope={scope} />
          )}
        </>
      )}
    </div>
  );
};
