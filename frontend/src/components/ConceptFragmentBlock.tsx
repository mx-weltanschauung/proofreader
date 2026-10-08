import { useRef, useState } from 'react';
import { Link } from 'react-router-dom';

import { useFootnotePreview } from '../hooks/useFootnotePreview';
import { useKatex } from '../hooks/useKatex';
import { useNoteXrefs } from '../hooks/useNoteXrefs';
import type { ConceptCut, ConceptEntry, ConceptEntryPage } from '../types';
import { entryAddressLabel, entryAnchorId } from '../utils/conceptEntries';
import { rubricPathLabel } from '../utils/conceptReferences';
import { readerStatusLabel } from '../utils/pageStatusLabel';
import { chapterPath, pagePath } from '../utils/paths';
import { ConceptEntryExpand } from './ConceptEntryExpand';
import './ConceptFragmentBlock.css';

interface ConceptFragmentBlockProps {
  entry: ConceptEntry;
  slug: string;
  /**
   * Показывать ли подрубрику в шапке. В порядке по рубрикам её несёт заголовок
   * группы над записями, и повтор на каждой записи только дублировал бы текст;
   * в порядке по томам заголовков групп нет, и шапка — единственное место, где
   * подрубрика вообще названа.
   */
  showRubric: boolean;
  /**
   * Точечное обновление этой записи после сохранения правки границ (задача
   * 13a) — прокидывается насквозь до ConceptEntryExpand, где и вызывается.
   */
  replaceEntry?: (referenceId: number) => Promise<void>;
}

/**
 * Пометка о состоянии вырезок записи. Молчит там, где сказать нечего.
 *
 * Смешанный случай (задача финального разбора, находка 1) — состояние
 * записи остаётся fragment: живая часть показывается как обычно, но
 * отвязавшаяся вырезка (entry.stale_cuts) не рендерится вовсе (у неё нет
 * текста), и без отдельной пометки читатель принял бы её отсутствие за то,
 * что там никогда ничего не было.
 */
function stateNote(entry: ConceptEntry): string {
  if (entry.state === 'whole_page') return 'фрагмент не выделен';
  if (entry.state === 'stale') return 'границы съехали после правки страницы';
  if (entry.state === 'fragment' && entry.stale_cuts.length > 0) {
    return 'часть вырезок адреса отвязалась после правки другой страницы';
  }
  return '';
}

/** Блок тела записи: вырезка адреса либо его ненарезанная страница. */
type EntryBlock =
  | { kind: 'cut'; cut: ConceptCut; page: number }
  | { kind: 'uncut'; page: ConceptEntryPage };

/** Номер страницы, по которому блок встаёт в порядок чтения. */
function blockPage(block: EntryBlock): number {
  return block.kind === 'cut' ? block.page : block.page.page_number;
}

/**
 * Тело записи: вырезки адреса и его ненарезанные страницы, в порядке страниц.
 *
 * Адрес шире своих вырезок: указатель называет 730—731, а вырезка может
 * лежать только на 730-й. Страница без вырезки всё равно принадлежит адресу —
 * без неё читатель не узнает, что смотреть надо и там, а редактор не сможет
 * её открыть: разворот адресуется по page_id, которого взять больше неоткуда.
 *
 * Ключ сортировки вырезки — номер её первой части. Вырезка без частей (якорь
 * вне текущего диапазона адреса) уходит в конец — тем же правилом, каким
 * сервер укладывает безъякорные фрагменты (sortFragmentsByPosition).
 */
function entryBlocks(entry: ConceptEntry): EntryBlock[] {
  const covered = new Set(entry.cuts.flatMap((c) => c.parts.map((p) => p.page_id)));
  const blocks: EntryBlock[] = [
    ...entry.cuts.map(
      (cut): EntryBlock => ({
        kind: 'cut',
        cut,
        page: cut.parts[0]?.page_number ?? Number.MAX_SAFE_INTEGER,
      }),
    ),
    ...entry.pages
      .filter((page) => !covered.has(page.page_id))
      .map((page): EntryBlock => ({ kind: 'uncut', page })),
  ];
  // Сортировка стабильна по стандарту (ES2019): две вырезки на одной странице
  // сохраняют порядок, в котором их прислал сервер, — а он уже упорядочен по
  // смещению.
  return blocks.sort((a, b) => blockPage(a) - blockPage(b));
}

/**
 * Одна запись потока: адрес указателя со своей подрубрикой и вырезками.
 *
 * Единица — адрес, а не страница: подрубрика ведёт чтение, и два адреса,
 * попавших на одни страницы, показывают разные её места.
 */
export const ConceptFragmentBlock: React.FC<ConceptFragmentBlockProps> = ({
  entry,
  slug,
  showRubric,
  replaceEntry,
}) => {
  const contentRef = useRef<HTMLDivElement>(null);
  const [expanded, setExpanded] = useState<number | null>(null);

  // Подпись содержимого, а не просто счётчик вырезок: сохранение правки
  // границ (replaceEntry) может поменять HTML вырезок, не поменяв их
  // число — например, вырезка сузилась на той же странице. cutCount этого
  // не заметит, эффекты KaTeX/сносок/кросс-ссылок не перезапустятся, и
  // разметка внутри contentRef останется необработанной старой версией.
  const contentSignature = entry.cuts
    .flatMap((cut) => cut.parts.map((part) => `${part.page_id}:${part.html}`))
    .join('\0');
  useKatex(contentRef, [entry.reference_id, contentSignature]);
  useNoteXrefs(contentRef, entry.work_id, entry.work_slug, [entry.reference_id, contentSignature]);
  useFootnotePreview(contentRef, [entry.reference_id, contentSignature]);

  // Статус — по первой странице записи, о которой есть что сказать:
  // ранжирования серьёзности статусов в проекте нет, «худшую» выбрать нечем.
  const statuses = entry.cuts
    .flatMap((cut) => cut.parts)
    .map((part) => readerStatusLabel(part.page_status))
    .filter(Boolean);
  const status = statuses[0] ?? '';
  const note = stateNote(entry);
  const firstPage = entry.cuts[0]?.parts[0];
  const headQuote = entry.cuts[0]?.head_quote ?? '';
  const blocks = entryBlocks(entry);

  return (
    <article className="concept-fragment" id={entryAnchorId(entry.reference_id)}>
      <header className="concept-fragment-header">
        {showRubric && <div className="concept-fragment-rubrics">{rubricPathLabel(entry)}</div>}
        <div className="concept-fragment-address">{entryAddressLabel(entry)}</div>
        <div className="concept-fragment-source">
          {entry.work_title}
          {entry.chapter_title && (
            <span className="concept-fragment-chapter">
              {' → '}
              {entry.chapter_id != null ? (
                <Link
                  to={chapterPath(
                    { id: entry.work_id, slug: entry.work_slug },
                    { id: entry.chapter_id, slug: entry.chapter_slug },
                  )}
                  className="concept-fragment-chapter-link"
                >
                  {entry.chapter_title}
                </Link>
              ) : (
                entry.chapter_title
              )}
            </span>
          )}
        </div>
        <div className="concept-fragment-meta">
          {status && <span className="concept-cut-status">{status}</span>}
          {note && <span className="concept-cut-note">{note}</span>}
          {firstPage && (
            <Link
              // Адрес МЕСТА, а не всей полосы: головная цитата уже хранится и
              // уже поддерживается живым переякориванием (applyPageEdit), и
              // превратить её в ?quote= — одна строка. Смещения, хэш и
              // статус наружу по-прежнему не идут.
              //
              // Цитата — срез markdown, а ?quote= ищется в отрисованном
              // тексте: вырезка, начавшаяся внутри разметки, промахнётся и
              // покажет читателю честное «не нашлась», приведя его при этом
              // на нужную полосу. Замер по корпусу — примерно четыре промаха
              // из десяти.
              //
              // Пустая head_quote — у синтетической/отвязавшейся вырезки:
              // якорь не подтверждён переякориванием, и вести читателя на
              // конкретное место значило бы вести его наугад — адрес
              // остаётся голой полосой.
              to={
                headQuote
                  ? `${pagePath({ id: entry.work_id, slug: entry.work_slug }, firstPage.page_number)}?quote=${encodeURIComponent(headQuote)}`
                  : pagePath({ id: entry.work_id, slug: entry.work_slug }, firstPage.page_number)
              }
              className="concept-fragment-link"
            >
              открыть страницу тома
            </Link>
          )}
        </div>
      </header>

      <div className="concept-fragment-body" ref={contentRef}>
        {blocks.map((block, i) =>
          block.kind === 'uncut' ? (
            <div key={`uncut-${block.page.page_id}`} className="concept-cut-uncut">
              <div className="concept-fragment-folio">· с. {block.page.printed_page} ·</div>
              <span className="concept-cut-note">фрагмент не выделен</span>
              <button
                type="button"
                className="concept-cut-expand"
                onClick={() =>
                  setExpanded(expanded === block.page.page_id ? null : block.page.page_id)
                }
              >
                {expanded === block.page.page_id
                  ? 'свернуть страницу'
                  : 'показать страницу целиком'}
              </button>
              {expanded === block.page.page_id && (
                <ConceptEntryExpand
                  slug={slug}
                  referenceId={entry.reference_id}
                  pageId={block.page.page_id}
                  cuts={entry.cuts}
                  staleCuts={entry.stale_cuts}
                  replaceEntry={replaceEntry}
                />
              )}
            </div>
          ) : (
            <div key={block.cut.id ?? `whole-${i}`} className="concept-cut">
              {blocks[i - 1]?.kind === 'cut' && (
                // Между двумя вырезками одного адреса — пропуск: это разные
                // места страницы, и склеивать их значит соврать о
                // непрерывности. Рядом с заглушкой пропуск не нужен: она сама
                // и есть разрыв, причём подписанный колонцифрой.
                <div className="concept-cut-gap" aria-hidden="true">
                  ⋯
                </div>
              )}
              {block.cut.parts.length > 0
                ? block.cut.parts.map((part) => (
                    <div key={part.page_id} className="concept-cut-part">
                      {
                        // Каждая часть вырезки подписана печатной колонцифрой.
                        // Разные части одной вырезки могут оказаться на разных
                        // страницах при разрыве мысли; вырезки одного адреса лежат
                        // на разных страницах, и голый пропуск об этом молчит.
                        // Колонцифра показывает и первую часть, и вторую. На первой
                        // части это может дублировать информацию из шапки (если весь
                        // адрес на одной странице), но однозначность важнее краткости.
                        <div className="concept-fragment-folio">· с. {part.printed_page} ·</div>
                      }
                      <div
                        className="page-html-content"
                        dangerouslySetInnerHTML={{ __html: part.html }}
                      />
                      <button
                        type="button"
                        className="concept-cut-expand"
                        onClick={() => setExpanded(expanded === part.page_id ? null : part.page_id)}
                      >
                        {expanded === part.page_id
                          ? 'свернуть страницу'
                          : 'показать страницу целиком'}
                      </button>
                      {expanded === part.page_id && (
                        <ConceptEntryExpand
                          slug={slug}
                          referenceId={entry.reference_id}
                          pageId={part.page_id}
                          cuts={entry.cuts}
                          staleCuts={entry.stale_cuts}
                          replaceEntry={replaceEntry}
                        />
                      )}
                    </div>
                  ))
                : // Cheap fix финального разбора: живая вырезка (id есть, статус
                  // не stale), у которой якорь всё же не попал в текущий диапазон
                  // адреса — parts пуст, но границы (bounds) есть. Раньше это
                  // рендерилось совсем пусто: без текста, кнопки, пометки и
                  // ссылки — запись выглядела так, будто вырезки не существует.
                  // Показываем откат на страницу целиком по known start_page_id.
                  block.cut.bounds && (
                    <div className="concept-cut-part">
                      <span className="concept-cut-note">
                        текст вырезки недоступен — страница выпала из диапазона адреса
                      </span>
                      <button
                        type="button"
                        className="concept-cut-expand"
                        onClick={() =>
                          setExpanded(
                            expanded === block.cut.bounds!.start_page_id
                              ? null
                              : block.cut.bounds!.start_page_id,
                          )
                        }
                      >
                        {expanded === block.cut.bounds.start_page_id
                          ? 'свернуть страницу'
                          : 'показать страницу целиком'}
                      </button>
                      {expanded === block.cut.bounds.start_page_id && (
                        <ConceptEntryExpand
                          slug={slug}
                          referenceId={entry.reference_id}
                          pageId={block.cut.bounds.start_page_id}
                          cuts={entry.cuts}
                          staleCuts={entry.stale_cuts}
                          replaceEntry={replaceEntry}
                        />
                      )}
                    </div>
                  )}
            </div>
          ),
        )}
      </div>
    </article>
  );
};
