import { useEffect, useState } from 'react';
import { searchApi } from '../services/api';
import { isTooShortQuery } from '../utils/searchQuery';

/**
 * Леммы для подсветки по ?q= в адресе режима чтения. Один лёгкий запрос
 * (terms_only=1) — каталог и тома читалке не нужны. Отказ молчаливый:
 * подсветка — украшение, ломать чтение из-за неё нельзя.
 */
export function useSearchTerms(q: string): string[] {
  const [terms, setTerms] = useState<string[]>([]);

  useEffect(() => {
    // Короткий запрос сервер отклонит (minQueryRunes), и подсветки не будет
    // в любом случае — ходить за отказом незачем.
    if (isTooShortQuery(q)) return;
    let cancelled = false;
    searchApi
      .terms(q)
      .then((res) => {
        if (!cancelled) setTerms(res.data.terms);
      })
      .catch(() => {
        if (!cancelled) setTerms([]);
      });
    return () => {
      cancelled = true;
    };
  }, [q]);

  return q.trim() ? terms : [];
}
