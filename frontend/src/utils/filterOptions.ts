import { normalizeWord } from './searchHighlight';

/**
 * Строка списка. `group` — заголовок полки (собрание), под которым строка
 * стоит; соседние строки одной группы рисуются под одним заголовком, поэтому
 * список приходит уже упорядоченным. `depth` — уровень в дереве глав, список
 * тогда идёт в порядке чтения (обход в глубину). `search` — то, в чём ищется
 * запрос, если одной подписи мало: у тома это ещё и название собрания.
 */
export interface ComboOption {
  id: number;
  label: string;
  group?: string;
  depth?: number;
  search?: string;
}

const DIGITS = /^\d+$/;

function matches(haystack: string, token: string): boolean {
  if (!DIGITS.test(token)) return haystack.includes(token);
  // Число ищется целым: «том 1» не должен тащить тома 10—19.
  return new RegExp(`(^|\\D)${token}(\\D|$)`).test(haystack);
}

/**
 * Отбор по запросу: каждое слово запроса должно найтись в `search ?? label`
 * без учёта регистра и различия ё/е. У совпавшей строки дерева остаются и её
 * предки — иначе «Письмо второе» висело бы без указания, из какой оно работы.
 */
export function filterOptions(options: ComboOption[], query: string): ComboOption[] {
  const tokens = normalizeWord(query).split(/\s+/).filter(Boolean);
  if (tokens.length === 0) return options;

  const keep = new Set<number>();
  options.forEach((option, index) => {
    const haystack = normalizeWord(option.search ?? option.label);
    if (!tokens.every((t) => matches(haystack, t))) return;
    keep.add(index);
    // Предки в обходе в глубину — ближайшие строки выше с меньшей глубиной.
    let depth = option.depth ?? 0;
    for (let i = index - 1; i >= 0 && depth > 0; i--) {
      const d = options[i].depth ?? 0;
      if (d < depth) {
        keep.add(i);
        depth = d;
      }
    }
  });
  return options.filter((_, index) => keep.has(index));
}
