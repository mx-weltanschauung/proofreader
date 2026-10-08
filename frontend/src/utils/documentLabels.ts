// Общий словарь для экрана разбора (DocumentForm/DocumentView) и «Моего»
// (MySuggestions) — оба места обязаны называть одно и то же состояние
// одними словами (задачи 11—12, CLAUDE.md: «форма и «моё» не должны
// разойтись в словах»).
import type { DocumentRejectReason } from '../types';

/** Причины отказа разбору — закрытый список, тот же, что
 *  models.ValidDocumentRejectReason на сервере. Слова здесь свои: сервер
 *  русских подписей не отдаёт вовсе, только код причины. */
export const DOCUMENT_REJECT_LABEL: Record<DocumentRejectReason, string> = {
  не_по_теме: 'Не по теме читальни',
  текст_без_разбора: 'Корпус без вашего разбора',
  брань_или_оскорбления: 'Брань или оскорбления',
  нарушает_закон: 'Нарушает закон',
};

/** Минимум полей Document, которого достаточно, чтобы отличить состояние —
 *  чтобы не тащить сюда весь тип и не заводить циклическую зависимость.
 *
 *  Редакции необязательны: карточка разбора (`documentsApi.get`) везёт обе
 *  целиком, а страница просмотра (`/view`) — только готовый HTML и готовый
 *  признак `has_unpublished_changes`, потому что тащить туда markdown обеих
 *  редакций ради одного сравнения значило бы возить текст трижды. */
interface DocumentStateFields {
  published_at: string | null;
  was_published: boolean;
  review_status: string;
  title?: string;
  markdown_content?: string;
  published_title?: string;
  published_markdown?: string;
  /** Готовый признак расхождения — когда обеих редакций на руках нет. */
  has_unpublished_changes?: boolean;
}

/** Короткое имя состояния — определяет и текст, и цвет пилюли. Разбор живёт в
 *  одном из ШЕСТИ сочетаний published_at × review_status × was_published ×
 *  «черновик разошёлся с опубликованным» (см.
 *  document_access.go/documentForViewer).
 *
 *  Шестое — `edited-over-live` — перечень пропускал: разбор на людях, автор
 *  сохранил правку, но на проверку не отправил. `review_status` при этом
 *  остаётся «одобрено», и полоса печатала «Разбор на людях», хотя на людях
 *  прежний текст. */
export type DocumentStateKind =
  | 'pending-over-live'
  | 'edited-over-live'
  | 'live'
  | 'taken'
  | 'pending'
  | 'draft';

/** Разошёлся ли черновик с тем, что на людях.
 *
 *  Готовый признак с сервера в приоритете; иначе сравниваются обе редакции —
 *  но только когда опубликованная действительно на руках. У постороннего
 *  сервер подменяет `title`/`markdown_content` опубликованной редакцией и
 *  гасит `published_*` до пустых строк: сравнение без этой оговорки давало бы
 *  «есть правка» на каждом опубликованном разборе. */
function draftDiffersFromPublished(doc: DocumentStateFields): boolean {
  if (doc.has_unpublished_changes !== undefined) {
    return doc.has_unpublished_changes;
  }
  if (!doc.published_markdown && !doc.published_title) {
    return false;
  }
  return doc.markdown_content !== doc.published_markdown || doc.title !== doc.published_title;
}

export function documentStateKind(doc: DocumentStateFields): DocumentStateKind {
  if (doc.published_at) {
    if (doc.review_status === 'на_рассмотрении') return 'pending-over-live';
    return draftDiffersFromPublished(doc) ? 'edited-over-live' : 'live';
  }
  if (doc.was_published) {
    return 'taken';
  }
  return doc.review_status === 'на_рассмотрении' ? 'pending' : 'draft';
}

/** Полная фраза состояния — над редактором в форме, строкой в списке
 *  «Мои разборы» и полосой над самим разбором на странице просмотра. */
export function documentStateLabel(doc: DocumentStateFields): string {
  switch (documentStateKind(doc)) {
    case 'pending-over-live':
      return 'Правка ждёт проверки; на людях — прежняя редакция.';
    case 'edited-over-live':
      return 'Правка сохранена, но на проверку не отправлена; на людях — прежняя редакция.';
    case 'live':
      return 'Разбор на людях.';
    case 'taken':
      return 'Разбор снят с публикации. Черновик остался у вас.';
    case 'pending':
      return 'Разбор ждёт проверки.';
    case 'draft':
      return 'Черновик виден только вам.';
  }
}
