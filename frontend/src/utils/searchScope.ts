/**
 * Область поиска — то, что читатель выбрал в панели, и то, что едет в адресе.
 * Адрес остаётся единственным источником правды: выдачу должно быть можно
 * переслать ссылкой.
 */
export interface SearchScope {
  editions: number[];
  works: number[];
  chapters: number[];
}

export const EMPTY_SCOPE: SearchScope = { editions: [], works: [], chapters: [] };

/**
 * Список id из параметра. Мусор здесь именно отбрасывается, а не роняет
 * разбор: на сервере тот же список отклоняется целиком (400), но клиент
 * читает и собственные адреса, набранные руками, и должен показать выдачу по
 * уцелевшему, а не белый экран.
 */
function ids(params: URLSearchParams, name: string): number[] {
  const raw = params.get(name);
  if (!raw) return [];
  const out: number[] = [];
  for (const part of raw.split(',')) {
    const n = Number(part.trim());
    if (Number.isInteger(n) && n > 0 && !out.includes(n)) out.push(n);
  }
  return out;
}

export function parseScope(params: URLSearchParams): SearchScope {
  const editions = ids(params, 'editions');
  const works = ids(params, 'works');
  // Прежние имена: на них ведут ссылки из выдачи, с карточек томов, из чужих
  // писем и закладок. Редиректов не заводим — параметр просто читается.
  const legacyWork = Number(params.get('work'));
  if (Number.isInteger(legacyWork) && legacyWork > 0 && !works.includes(legacyWork)) {
    works.push(legacyWork);
  }
  const legacyEdition = Number(params.get('edition'));
  if (Number.isInteger(legacyEdition) && legacyEdition > 0 && !editions.includes(legacyEdition)) {
    editions.push(legacyEdition);
  }
  // Главы имеют смысл только внутри одного тома: фасет строится по тому, и
  // область «главы из разных томов» ни запросом, ни выдачей не поддержана.
  // Собрания в счёт не идут (поправка спеки от 18.09.2026): читатель,
  // пришедший в том из поиска по собранию, вправе сузиться до глав того же
  // тома — тем же правилом, что и у singleWorkOf ниже, собрание остаётся
  // контекстом возврата, а не условием, гасящим фильтр.
  const chapters = works.length === 1 ? ids(params, 'chapters') : [];
  return { editions, works, chapters };
}

export function isEmptyScope(scope: SearchScope): boolean {
  return scope.editions.length === 0 && scope.works.length === 0;
}

/**
 * Том, если он в области ровно один: выдача сразу показывает его полосы.
 * Собрания, выбранные вместе с ним, не мешают — до этой правки условие
 * требовало ещё и «собраний нет», и обычный путь читателя (искал в
 * собрании → кликнул том → должен читать его полосы) молча ломался: адрес
 * после клика нёс и editions, и works, а функция отвечала null, откатывая
 * читателя обратно в обзор собрания. Собрание остаётся в области — оно
 * контекст возврата (строка области), а не условие показа полос.
 */
export function singleWorkOf(scope: SearchScope): number | null {
  return scope.works.length === 1 ? scope.works[0] : null;
}

export function searchPath(q: string, scope: SearchScope = EMPTY_SCOPE): string {
  const params = new URLSearchParams({ q });
  if (scope.editions.length > 0) params.set('editions', scope.editions.join(','));
  if (scope.works.length > 0) params.set('works', scope.works.join(','));
  if (scope.chapters.length > 0 && singleWorkOf(scope) !== null) {
    params.set('chapters', scope.chapters.join(','));
  }
  return `/search?${params.toString()}`;
}
