import { describe, it, expect } from 'vitest';
import { parseScope, searchPath, singleWorkOf, isEmptyScope, EMPTY_SCOPE } from './searchScope';

const parse = (qs: string) => parseScope(new URLSearchParams(qs));

describe('searchScope', () => {
  it('пустой адрес — пустая область', () => {
    expect(isEmptyScope(parse('q=партия'))).toBe(true);
  });

  it('списки разбираются', () => {
    expect(parse('q=x&editions=2&works=13,14')).toEqual({
      editions: [2],
      works: [13, 14],
      chapters: [],
    });
  });

  it('старые work= и edition= читаются как область из одного', () => {
    expect(parse('q=x&work=7')).toEqual({ editions: [], works: [7], chapters: [] });
    expect(parse('q=x&edition=5')).toEqual({ editions: [5], works: [], chapters: [] });
  });

  it('главы держатся только при ровно одном томе', () => {
    expect(parse('q=x&works=13&chapters=101,102').chapters).toEqual([101, 102]);
    expect(parse('q=x&works=13,14&chapters=101').chapters).toEqual([]);
    // Поправка спеки от 18.09.2026: собрание рядом с единственным томом не
    // должно топить главы — читатель, пришедший в том из поиска по собранию,
    // вправе сузиться до глав, и это тот же обычный путь, каким singleWorkOf
    // теперь показывает полосы тома независимо от того, выбрано ли ещё и
    // собрание. До поправки здесь стояло [] — то же отменённое правило.
    expect(parse('q=x&editions=2&works=13&chapters=101').chapters).toEqual([101]);
  });

  it('мусор отбрасывается, а не превращается в NaN', () => {
    expect(parse('q=x&works=13,ы,0,-4')).toEqual({ editions: [], works: [13], chapters: [] });
  });

  it('адрес собирается обратно и читается тем же разбором', () => {
    // До поправки спеки от 18.09.2026 здесь стояло замечание, что editions
    // и chapters вместе не переживают даже один проход через searchPath —
    // это было верно для отменённого правила («главы только без собраний»).
    // Сейчас единственный том решает всё сам (singleWorkOf), а собрание
    // рядом с ним — обычный контекст возврата, не помеха фильтру по главам:
    // читатель, пришедший в том из поиска по собранию, вправе сузиться до
    // главы, не потеряв ни то, ни другое.
    const scope = { editions: [2], works: [13], chapters: [101] };
    const path = searchPath('партия', scope);
    expect(parse(path.split('?')[1])).toEqual(scope);
  });

  it('пустая область не пишет лишних параметров', () => {
    expect(searchPath('партия', EMPTY_SCOPE)).toBe(
      '/search?q=%D0%BF%D0%B0%D1%80%D1%82%D0%B8%D1%8F',
    );
  });

  it('один том виден отдельно — по нему выдача сразу показывает полосы', () => {
    expect(singleWorkOf(parse('q=x&works=13'))).toBe(13);
    expect(singleWorkOf(parse('q=x&works=13,14'))).toBeNull();
    // Поправка спеки от 18.09.2026: до этой правки собрание рядом с
    // единственным томом гасило показ полос (здесь стояло toBeNull()). Это
    // было ошибкой — до всей этой работы выдача показывала полосы тома
    // ВСЕГДА, когда в адресе есть том, а собрание рядом служило только
    // возвратной ссылкой «ко всем томам». Отменённое правило молча ломало
    // самый обычный путь читателя: искал в собрании → кликнул том → должен
    // читать его полосы, а получал бы обзор собрания заново.
    expect(singleWorkOf(parse('q=x&works=13&editions=2'))).toBe(13);
  });
});
