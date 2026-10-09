import { useEffect, useState } from 'react';
import {
  shelfApi,
  worksApi,
  chaptersApi,
  pagesApi,
  searchApi,
  documentsApi,
  type DocumentKey,
} from '../services/api';
import { citationSignature } from '../utils/citation';
import { citationPlace, issueOf } from '../utils/citationPlace';
import { printedFolio } from '../utils/folio';
import { apiErrorMessage } from '../utils/apiError';
import { BlockPicker, type BlockMark } from './BlockPicker';
import type { Shelf, Work, Chapter, Page, PageMapEntry, SearchVolume } from '../types';
import './CutPicker.css';

export interface CutPickerProps {
  documentKey: DocumentKey;
  onInsert: (cutId: number) => void;
  onClose: () => void;
}

/**
 * Подборщик вклейки: выбор тома и полосы, выбор куска БЛОКАМИ (см.
 * `BlockPicker.tsx`), отправка `POST /documents/{слаг}/cuts` (или
 * `/documents/{ник}/{слаг}/cuts` у читательского разбора). Открывается
 * кнопкой «Вклейка» в `DocumentForm.tsx`; вставку тега `<cut id="N">` на
 * место курсора делает вызывающий экран через `onInsert`.
 *
 * Поверхность выбора раньше была сырым markdown в `<pre>` с выделением мышью
 * (как в редакторе вырезок понятий, ConceptEntryExpand.tsx) — читателю такую
 * разметку показывать нельзя, а карты «отрисованный текст → байты markdown»
 * в проекте нет и строить её здесь не входило в задачу. Решение — сервер сам
 * режет полосу на блоки и отдаёт каждому байтовые границы и готовую
 * вёрстку; автор указывает первый и последний блок, а не выделяет текст.
 *
 * Начало и конец ставятся ДВУМЯ отдельными жестами и могут лежать на разных
 * полосах одного тома — этим закрыта многостраничная вклейка, которую API и
 * рендер умели всегда, а прежний подборщик — нет.
 */
export const CutPicker: React.FC<CutPickerProps> = ({ documentKey, onInsert, onClose }) => {
  const [shelf, setShelf] = useState<Shelf | null>(null);
  const [shelfError, setShelfError] = useState<string | null>(null);

  const [workId, setWorkId] = useState('');
  const [work, setWork] = useState<Work | null>(null);
  const [workError, setWorkError] = useState<string | null>(null);
  const [chapters, setChapters] = useState<Chapter[]>([]);
  const [pageMap, setPageMap] = useState<PageMapEntry[]>([]);

  const [pageNumberInput, setPageNumberInput] = useState('');
  const [page, setPage] = useState<Page | null>(null);
  const [pageError, setPageError] = useState<string | null>(null);

  const [searchQuery, setSearchQuery] = useState('');
  const [searchResults, setSearchResults] = useState<SearchVolume[]>([]);
  const [searchError, setSearchError] = useState<string | null>(null);

  // Начало и конец вклейки ставятся порознь и могут лежать на РАЗНЫХ полосах
  // одного тома — этим закрыта многостраничная вклейка. Смена полосы внутри
  // тома НЕ сбрасывает края (это и есть жест многостраничной вклейки), смена
  // тома — сбрасывает: края из другого тома бессмысленны (см. selectWork).
  const [startMark, setStartMark] = useState<BlockMark | null>(null);
  const [endMark, setEndMark] = useState<BlockMark | null>(null);
  const [edgeHint, setEdgeHint] = useState(false);
  const [createError, setCreateError] = useState<string | null>(null);
  const [isCreating, setIsCreating] = useState(false);

  // Полка — вся навигация первым путём (эдиции + тома вне собраний), одним
  // ответом, как и заведено ради таких экранов (/api/shelf).
  useEffect(() => {
    shelfApi
      .get()
      .then((res) => setShelf(res.data))
      .catch((err: unknown) => setShelfError(apiErrorMessage(err, 'Не удалось загрузить полку')));
  }, []);

  function selectWork(idStr: string) {
    setEdgeHint(false);
    setCreateError(null);
    setStartMark(null);
    setEndMark(null);
    setPage(null);
    setPageError(null);
    setPageNumberInput('');
    setChapters([]);
    setPageMap([]);
    setWorkError(null);
    setWorkId(idStr);

    if (!idStr) {
      setWork(null);
      return;
    }

    const id = Number(idStr);
    setWork(null);

    worksApi
      .get(id)
      .then((res) => setWork(res.data))
      .catch((err: unknown) => setWorkError(apiErrorMessage(err, 'Не удалось загрузить том')));

    // Главы и карта страниц — вспомогательная навигация, а не содержимое:
    // их отказ не должен мешать выбрать полосу вручную по номеру.
    chaptersApi
      .list(id)
      .then((res) => setChapters(res.data))
      .catch(() => {});

    worksApi
      .pageMap(id)
      .then((res) => setPageMap(res.data))
      .catch(() => {});
  }

  async function loadPage() {
    if (!work) return;
    const n = Number(pageNumberInput);
    if (!Number.isInteger(n) || n <= 0) {
      setPageError('Введите номер страницы');
      return;
    }
    setPageError(null);
    setEdgeHint(false);
    setCreateError(null);
    try {
      const res = await pagesApi.getByNumber(work.id, n);
      setPage(res.data);
    } catch (err) {
      setPage(null);
      setPageError(apiErrorMessage(err, 'Не удалось загрузить страницу'));
    }
  }

  async function runSearch() {
    const q = searchQuery.trim();
    if (q.length < 2) {
      setSearchError('Минимум два знака');
      return;
    }
    setSearchError(null);
    try {
      const res = await searchApi.search(q);
      setSearchResults(res.data.volumes);
    } catch (err) {
      setSearchError(apiErrorMessage(err, 'Поиск не удался'));
    }
  }

  function pickEdge(edge: 'start' | 'end', offset: number, pageNumber: number) {
    setCreateError(null);
    setEdgeHint(false);
    if (edge === 'start') setStartMark({ pageNumber, offset });
    else setEndMark({ pageNumber, offset });
  }

  async function onMakeCut() {
    if (!work || !startMark || !endMark) {
      setEdgeHint(true);
      return;
    }
    const sameEdgeOutOfOrder =
      endMark.pageNumber < startMark.pageNumber ||
      (endMark.pageNumber === startMark.pageNumber && endMark.offset < startMark.offset);
    if (sameEdgeOutOfOrder) {
      // Тот же отказ, что и для перепутанных полос: сервер увидел бы то же
      // самое как невалидный диапазон байтов и ответил бы «Граница вклейки
      // вне полосы» — про другое. Автор оперирует блоками, а не байтами, и
      // сообщение обязано сказать ему про перепутанный порядок отметок, а не
      // про границы полосы.
      setCreateError('Конец вклейки раньше её начала — переставьте границы.');
      return;
    }

    setEdgeHint(false);
    setCreateError(null);
    setIsCreating(true);

    const pageNumbers: number[] = [];
    for (let n = startMark.pageNumber; n <= endMark.pageNumber; n++) pageNumbers.push(n);

    const place = citationPlace(work, chapters, startMark.pageNumber);
    const sourceTitle = citationSignature({
      // Тот же помощник, что у кнопки «Цитировать»: вклейка из номера
      // журнала подписывается статьёй и номером, а не «томом».
      author: place.author,
      workTitle: place.workTitle,
      editionTitle: work.edition_title ?? '',
      volumeTitle: work.title,
      volumeNumber: work.volume_number,
      volumePart: work.volume_part,
      folios: pageNumbers.map((n) => printedFolio(n, work)),
      pageNumbers,
      issue: issueOf(work),
    });

    try {
      const res = await documentsApi.createCut(documentKey, {
        work_id: work.id,
        start_page: startMark.pageNumber,
        start_offset: startMark.offset,
        end_page: endMark.pageNumber,
        end_offset: endMark.offset,
        source_title: sourceTitle,
      });
      onInsert(res.data.id);
    } catch (err) {
      setCreateError(apiErrorMessage(err, 'Не удалось создать вклейку'));
    } finally {
      setIsCreating(false);
    }
  }

  return (
    <div className="cut-picker" role="dialog" aria-label="Подборщик вклейки">
      <div className="cut-picker-header">
        <h3>Вклейка из корпуса</h3>
        <button type="button" className="cut-picker-close" aria-label="закрыть" onClick={onClose}>
          ×
        </button>
      </div>

      <div className="cut-picker-section">
        <label htmlFor="cut-picker-work">Том</label>
        <select
          id="cut-picker-work"
          className="cut-picker-work-select"
          value={workId}
          onChange={(e) => selectWork(e.target.value)}
        >
          <option value="">— выбрать —</option>
          {shelf?.editions.map((se) => (
            <optgroup key={se.edition.id} label={se.edition.title}>
              {se.volumes.map((v) => (
                <option key={v.id} value={v.id}>
                  {v.title}
                </option>
              ))}
            </optgroup>
          ))}
          {shelf && shelf.loose_works.length > 0 && (
            <optgroup label="Отдельные работы">
              {shelf.loose_works.map((w) => (
                <option key={w.id} value={w.id}>
                  {w.title}
                </option>
              ))}
            </optgroup>
          )}
        </select>
        {shelfError && <p className="cut-picker-error">{shelfError}</p>}
        {workError && <p className="cut-picker-error">{workError}</p>}
      </div>

      <div className="cut-picker-section">
        <label htmlFor="cut-picker-search">Поиск по корпусу</label>
        <div className="cut-picker-search">
          <input
            id="cut-picker-search"
            type="text"
            value={searchQuery}
            onChange={(e) => setSearchQuery(e.target.value)}
          />
          <button type="button" onClick={() => void runSearch()}>
            Искать
          </button>
        </div>
        {searchError && <p className="cut-picker-error">{searchError}</p>}
        {searchResults.length > 0 && (
          <ul className="cut-picker-search-results">
            {searchResults.map((v) => (
              <li key={v.work_id}>
                <button
                  type="button"
                  className="cut-picker-search-result"
                  onClick={() => selectWork(String(v.work_id))}
                >
                  {v.title}
                  {v.author ? ` — ${v.author}` : ''}
                </button>
              </li>
            ))}
          </ul>
        )}
      </div>

      {work && (
        <div className="cut-picker-section">
          <label htmlFor="cut-picker-page-number">Номер страницы</label>
          <div className="cut-picker-page-controls">
            <input
              id="cut-picker-page-number"
              className="cut-picker-page-input"
              type="text"
              inputMode="numeric"
              list="cut-picker-known-pages"
              value={pageNumberInput}
              onChange={(e) => setPageNumberInput(e.target.value)}
            />
            <datalist id="cut-picker-known-pages">
              {pageMap.map((entry) => (
                <option key={entry.page_number} value={entry.page_number} />
              ))}
            </datalist>
            <button type="button" onClick={() => void loadPage()}>
              Загрузить
            </button>
          </div>
          {pageError && <p className="cut-picker-error">{pageError}</p>}
        </div>
      )}

      {page && work && (
        <div className="cut-picker-section">
          <p className="cut-picker-hint">
            Отметьте первый и последний блок вклейки. Начало и конец могут быть на разных полосах —
            смените номер страницы между жестами.
          </p>
          <BlockPicker
            workId={work.id}
            pageId={page.id}
            pageNumber={page.page_number}
            onPick={pickEdge}
            startMark={startMark ?? undefined}
            endMark={endMark ?? undefined}
          />
        </div>
      )}

      {work && (
        <div className="cut-picker-section cut-picker-footer">
          <p className="cut-picker-marks">
            Начало: {startMark ? `с. ${startMark.pageNumber}` : '—'}; конец:{' '}
            {endMark ? `с. ${endMark.pageNumber}` : '—'}
          </p>
          {edgeHint && <p className="cut-picker-error">Отметьте начало и конец вклейки.</p>}
          {createError && <p className="cut-picker-error">{createError}</p>}
          <div className="cut-picker-actions">
            <button type="button" onClick={() => void onMakeCut()} disabled={isCreating}>
              Вклеить
            </button>
          </div>
        </div>
      )}
    </div>
  );
};
