/**
 * Адрес разбора. Отдельным файлом, а не в `paths.ts`: тот объявлен «номер —
 * ключ, слаг — украшение» (см. его шапку), а у разбора слаг вместе с подписью
 * и есть ключ — слага без подписи разборы не адресуют вовсе, id в адресе нет.
 */

type IdentifiedDocument = { slug: string; author_nickname?: string };

/** Короткий адрес (`/documents/{slug}`) у сотруднического разбора, длинный
 *  (`/documents/{ник}/{slug}`) у читательского — тот же выбор адреса, что
 *  делает сервер: у сотруднической строки author_nickname пуст. */
export function documentPath(document: IdentifiedDocument): string {
  return document.author_nickname
    ? `/documents/${document.author_nickname}/${document.slug}`
    : `/documents/${document.slug}`;
}

export function documentEditPath(document: IdentifiedDocument): string {
  return `${documentPath(document)}/edit`;
}
