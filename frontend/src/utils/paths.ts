/**
 * Адреса читальни. Номер — ключ, слаг — украшение: `/works/49-lenin-t06` и
 * `/works/49` ведут в один том, поэтому пустой слаг здесь законен, а не
 * повод для запасного варианта.
 *
 * Транслитерации здесь нет намеренно: слаг приходит полем ответа API, и
 * правило живёт на сервере (`pkg/slug`). Второй транслитератор на клиенте
 * означал бы, что адрес, скопированный читателем из строки браузера,
 * расходится с каноном.
 */

type Identified = { id: number; slug?: string };
type IdentifiedEdition = { id: number; slug?: string; url_slug?: string };

/** Ведущее целое сегмента: `'49-lenin-t06'` → 49. `null`, если цифр нет. */
export function numericId(segment: string | undefined | null): number | null {
  if (!segment) return null;
  const match = /^(\d+)(?:-|$)/.exec(segment);
  return match ? Number(match[1]) : null;
}

function segment(id: number, slug?: string): string {
  return slug ? `${id}-${slug}` : `${id}`;
}

export function journalPath(journal: { slug: string }): string {
  return `/journals/${journal.slug}`;
}

export function authorPath(person: { slug: string }): string {
  return `/authors/${person.slug}`;
}

export function workPath(work: Identified): string {
  return `/works/${segment(work.id, work.slug)}`;
}

export function chapterPath(work: Identified, chapter: Identified): string {
  return `${workPath(work)}/chapters/${segment(chapter.id, chapter.slug)}`;
}

export function pagePath(work: Identified, pageNumber: number): string {
  return `${workPath(work)}/pages/${pageNumber}`;
}

export function readPath(work: Identified, pageNumber: number): string {
  return `${workPath(work)}/read/${pageNumber}`;
}

// Пустой url_slug откатывается на slug (ключ журнала публикации) — так же,
// как effectiveEditionSlug на сервере (internal/seo/canonical.go): канон,
// печатаемый краулеру, и адрес в строке браузера не должны расходиться.
export function editionPath(edition: IdentifiedEdition): string {
  return `/editions/${segment(edition.id, edition.url_slug || edition.slug)}`;
}

type IdentifiedCollection = { slug: string; author_nickname?: string };

/** Короткий адрес (`/collections/{slug}`) у сотруднической подборки, длинный
 *  (`/collections/{ник}/{slug}`) у читательской — тот же выбор адреса, что
 *  делает сервер: у витринной строки author_nickname пуст. */
export function collectionPath(collection: IdentifiedCollection): string {
  return collection.author_nickname
    ? `/collections/${collection.author_nickname}/${collection.slug}`
    : `/collections/${collection.slug}`;
}

export function collectionReadPath(collection: IdentifiedCollection, itemId: number): string {
  return `${collectionPath(collection)}/read/${itemId}`;
}
