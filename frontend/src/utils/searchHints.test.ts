import { describe, it, expect } from 'vitest';
import { SEARCH_HINTS } from './searchHints';

// Файл не назван шагами брифинга (там только модуль и тест Help.tsx), но
// заявлен в разделе Files как отдельный deliverable — модуль без своего теста
// был бы голыми данными без подстраховки. Ключ в <dt key={hint.query}>
// в Help.tsx требует уникальности query — это и проверяется здесь напрямую,
// а не только косвенно через рендер справки.
describe('searchHints', () => {
  it('список не пуст и у каждого приёма есть и запрос, и объяснение', () => {
    expect(SEARCH_HINTS.length).toBeGreaterThan(0);
    for (const hint of SEARCH_HINTS) {
      expect(hint.query.length).toBeGreaterThan(0);
      expect(hint.meaning.length).toBeGreaterThan(0);
    }
  });

  it('запросы приёмов не повторяются — React-ключ в справке строится по ним', () => {
    const queries = SEARCH_HINTS.map((hint) => hint.query);
    expect(new Set(queries).size).toBe(queries.length);
  });
});
