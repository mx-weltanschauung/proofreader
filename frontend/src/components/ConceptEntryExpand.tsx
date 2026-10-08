import { useEffect, useRef, useState } from 'react';

import { useAuth } from '../hooks/useAuth';
import { useCutEditor } from '../hooks/useCutEditor';
import { conceptsApi } from '../services/api';
import type { ConceptCut, ConceptCutInput, ConceptExpandedPage } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { selectionOffsets } from '../utils/cutSelection';
import './ConceptEntryExpand.css';

interface ConceptEntryExpandProps {
  slug: string;
  referenceId: number;
  pageId: number;
  /**
   * ВСЕ вырезки адреса (не только этой страницы) — редактору границ нужен
   * полный набор, потому что запись на сервер заменяющая: не увидев чужие
   * вырезки, он бы молча стёр их при сохранении своей.
   */
  cuts: ConceptCut[];
  /**
   * Вырезки адреса, отвязавшиеся от текста при переякоривании другой его
   * страницы (находка 1 финального разбора) — у них нет parts, поэтому их
   * не видно ни здесь, ни в cutToInput через cuts. Запись на сервер всё
   * равно заменяющая: не включив их сюда по id, редактор молча стёр бы их
   * первым же своим сохранением, хотя сам их не касался и статус трогать не
   * должен.
   */
  staleCuts?: ConceptCut[];
  /**
   * Точечное обновление свёрнутой записи в потоке после сохранения правки
   * границ (задача 13a) — см. useConceptFragments.replaceEntry. Необязательный
   * проп: разворот используется и вне потока (там обновлять нечего), поэтому
   * без обработчика просто не вызывается.
   */
  replaceEntry?: (referenceId: number) => Promise<void>;
}

/**
 * Переводит вырезку потока в форму запроса на запись. Настоящую вырезку с
 * видимыми частями — номерами страниц, как того просит PUT .../cuts, потому
 * что только так по ней можно узнать правимую страницу (см. isOnPage в
 * useCutEditor). Настоящую вырезку с исчезнувшими частями (якорь вне
 * диапазона адреса — на клиенте нет номера страницы, чтобы её описать
 * границами), а также вырезку, у которой пропал только ОДИН из двух якорей
 * (cutRanges оттянул видимую часть к первой или последней доступной странице
 * адреса, см. concept_cuts.go) — ссылкой по id: сервер перенесёт её как есть.
 * Смешивать в одних границах page_number из parts с offset из bounds можно
 * только когда обе стороны согласны, какой странице принадлежат — иначе
 * offset пропавшей страницы описал бы совсем другую. Синтетическую вырезку
 * (bounds: null, id: null) — никак, у неё нет id и она никогда не была
 * настоящей вырезкой.
 */
function cutToInput(cut: ConceptCut): ConceptCutInput | null {
  if (cut.parts.length === 0) {
    return cut.id !== null ? { id: cut.id } : null;
  }
  if (!cut.bounds) return null;
  const first = cut.parts[0];
  const last = cut.parts[cut.parts.length - 1];
  if (first.page_id !== cut.bounds.start_page_id || last.page_id !== cut.bounds.end_page_id) {
    return cut.id !== null ? { id: cut.id } : null;
  }
  return {
    start_page: first.page_number,
    start_offset: cut.bounds.start_offset,
    end_page: last.page_number,
    end_offset: cut.bounds.end_offset,
    status: cut.status === 'confirmed' ? 'confirmed' : 'machine',
  };
}

/**
 * Что уйдёт на сервер при сохранении. Запись заменяющая — человек должен
 * увидеть состав набора до нажатия «сохранить», а не заметить пропажу
 * вырезки на другой странице после.
 */
function saveSummary(editableCount: number, foreignCount: number): string {
  const total = editableCount + foreignCount;
  if (foreignCount === 0) {
    return `Сохранится ${total} вырезок этой страницы.`;
  }
  return (
    `Сохранится ${total} вырезок адреса: ${editableCount} этой страницы, ` +
    `${foreignCount} с других страниц или разворотов — без изменений.`
  );
}

type LoadState =
  | { status: 'loading' }
  | { status: 'ok'; page: ConceptExpandedPage }
  | { status: 'error'; message: string };

/**
 * Страница адреса целиком: куски приходят с сервера уже размеченными
 * «внутри вырезки», поэтому подсветка точна без сопоставления смещений с DOM.
 * Тумблер правки подменяет отрендеренный текст исходником — по нему
 * выделение переводится в смещения даром.
 */
export const ConceptEntryExpand: React.FC<ConceptEntryExpandProps> = ({
  slug,
  referenceId,
  pageId,
  cuts,
  staleCuts = [],
  replaceEntry,
}) => {
  const [state, setState] = useState<LoadState>({ status: 'loading' });
  const [editing, setEditing] = useState(false);
  const sourceRef = useRef<HTMLPreElement>(null);
  const { isAuthenticated } = useAuth();
  // Не звать setState после размонтирования (человек свернул запись, пока
  // сохранение ещё летело) — не ошибка сама по себе, но и незачем.
  const mountedRef = useRef(true);
  useEffect(
    () => () => {
      mountedRef.current = false;
    },
    [],
  );

  useEffect(() => {
    let cancelled = false;
    conceptsApi
      .expandPage(slug, referenceId, pageId)
      .then((res) => {
        if (!cancelled) setState({ status: 'ok', page: res.data });
      })
      .catch((err: unknown) => {
        if (!cancelled) {
          setState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить страницу'),
          });
        }
      });
    return () => {
      cancelled = true;
    };
  }, [slug, referenceId, pageId]);

  const page = state.status === 'ok' ? state.page : null;
  // staleCuts всегда идут по id — parts у них пуст, границами их не описать
  // (см. doc-комментарий проекта), а статус (stale) редактор не трогает: он
  // просто пересылает ссылку, сервер переносит вырезку как есть.
  const existing = [
    ...cuts.map(cutToInput).filter((c): c is ConceptCutInput => c !== null),
    ...staleCuts.filter((c) => c.id !== null).map((c) => ({ id: c.id as number })),
  ];
  const editor = useCutEditor(slug, referenceId, page?.page_number ?? 0, existing);

  // useCutEditor закрыт задачами 12/12a и не трогается: save() никогда не
  // отклоняет промис, ошибку кладёт в editor.error, — поэтому успех виден
  // только по переходу isSaving true → false при пустом error. Сравнение с
  // прошлым значением через ref, а не setState в теле эффекта по кругу: сама
  // реакция (два сетевых запроса) — законный побочный эффект, а не обход
  // правила react-hooks/set-state-in-effect.
  const wasSaving = useRef(false);
  useEffect(() => {
    const justSucceeded = wasSaving.current && !editor.isSaving && editor.error === '';
    wasSaving.current = editor.isSaving;
    if (!justSucceeded) return;

    // Порядок важен: запись в потоке обновляется раньше или одновременно с
    // разворотом — человек не должен ни на миг увидеть уже подсвеченный по-
    // новому разворот при ещё старом состоянии/статусе свёрнутой записи.
    void (async () => {
      await replaceEntry?.(referenceId);
      if (!mountedRef.current) return;
      try {
        const res = await conceptsApi.expandPage(slug, referenceId, pageId);
        if (mountedRef.current) setState({ status: 'ok', page: res.data });
      } catch (err: unknown) {
        if (mountedRef.current) {
          setState({
            status: 'error',
            message: apiErrorMessage(err, 'Не удалось загрузить страницу'),
          });
        }
      }
    })();
  }, [editor.isSaving, editor.error, referenceId, replaceEntry, slug, pageId]);

  if (state.status === 'loading') {
    return <div className="concept-cut-expand-note">Загружаем страницу…</div>;
  }
  if (state.status === 'error') {
    return <div className="concept-cut-expand-error">{state.message}</div>;
  }

  const onMarkSelection = () => {
    const container = sourceRef.current;
    const selection = window.getSelection();
    if (!container || !selection) return;
    const offsets = selectionOffsets(container, selection);
    if (offsets) editor.addCut(offsets);
  };

  return (
    <div className="concept-cut-expanded">
      {editing ? (
        <>
          <pre ref={sourceRef} className="concept-cut-source">
            {state.page.markdown}
          </pre>
          <div className="concept-cut-save-summary">
            {/* Запись заменяет набор вырезок адреса целиком — человек должен
                видеть, что уйдёт на сервер, до нажатия, а не после. */}
            {saveSummary(editor.cuts.length, editor.foreignCount)}
          </div>
          <div className="concept-cut-editor-actions">
            <button type="button" onClick={onMarkSelection}>
              сделать вырезкой
            </button>
            <button type="button" onClick={() => void editor.save()} disabled={editor.isSaving}>
              {editor.isSaving ? 'сохраняем…' : 'сохранить границы'}
            </button>
            <button type="button" onClick={() => setEditing(false)}>
              закончить правку
            </button>
          </div>
          <ul className="concept-cut-list">
            {editor.cuts.map((cut, i) => (
              <li key={`${cut.start}-${cut.end}`}>
                {cut.start}—{cut.end}{' '}
                <button
                  type="button"
                  onClick={() => editor.removeCut(i)}
                  aria-label="убрать вырезку"
                >
                  ×
                </button>
              </li>
            ))}
          </ul>
          {editor.error && <div className="concept-cut-expand-error">{editor.error}</div>}
        </>
      ) : (
        <>
          <div className="concept-cut-chunks">
            {state.page.chunks.map((chunk, i) => (
              <div
                key={i}
                className={
                  chunk.inside
                    ? 'concept-cut-chunk concept-cut-chunk-inside page-html-content'
                    : 'concept-cut-chunk page-html-content'
                }
                dangerouslySetInnerHTML={{ __html: chunk.html }}
              />
            ))}
          </div>
          {isAuthenticated && (
            <button type="button" className="concept-cut-expand" onClick={() => setEditing(true)}>
              править границы
            </button>
          )}
        </>
      )}
    </div>
  );
};
