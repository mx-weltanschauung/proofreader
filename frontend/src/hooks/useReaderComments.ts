import { useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { ReadingPreferencesContext } from '../contexts/readingPrefsContext';
import { DEFAULT_PREFS } from '../contexts/readingPrefs';
import { useAuth } from './useAuth';
import { readerCommentsApi } from '../services/api';
import type { ReaderComment } from '../types';

const EMPTY: ReaderComment[] = [];
export interface ReaderCommentsState {
  comments: ReaderComment[];
  replace(comment: ReaderComment): void;
  drop(id: number): void;
}
/** Ключ включает сессию и настройку: старый ответ не раскрывает чужие тексты. */
export function useReaderComments(workId: number | null): ReaderCommentsState {
  const userId = useAuth((s) => s.user?.id ?? null);
  const context = useContext(ReadingPreferencesContext);
  const show = context?.prefs.readerComments ?? DEFAULT_PREFS.readerComments;
  const key = workId !== null && (show || userId !== null) ? `${workId}:${userId}:${show}` : null;
  const [state, setState] = useState<{ key: string | null; items: ReaderComment[] }>({
    key: null,
    items: EMPTY,
  });
  const [refresh, setRefresh] = useState(0);
  useEffect(() => {
    const changed = () => setRefresh((v) => v + 1);
    window.addEventListener('reader-comments-changed', changed);
    return () => window.removeEventListener('reader-comments-changed', changed);
  }, []);
  useEffect(() => {
    if (key === null || workId === null) return;
    let alive = true;
    // Полный список тома нужен всем поверхностям, включая длинный поток.
    const load = async () => {
      const items: ReaderComment[] = [];
      for (let offset = 0; ; offset += 200) {
        const response = await readerCommentsApi.ofWork(workId, show, offset);
        if (!alive) return;
        const page = Array.isArray(response.data) ? response.data : [];
        items.push(...page);
        if (page.length < 200) break;
      }
      if (alive) setState({ key, items });
    };
    void load().catch(() => {});
    return () => {
      alive = false;
    };
  }, [key, workId, show, refresh]);
  const replace = useCallback(
    (c: ReaderComment) => {
      setState((s) =>
        s.key === key ? { ...s, items: s.items.map((x) => (x.id === c.id ? c : x)) } : s,
      );
      window.dispatchEvent(new Event('reader-comments-changed'));
    },
    [key],
  );
  const drop = useCallback(
    (id: number) => {
      setState((s) => (s.key === key ? { ...s, items: s.items.filter((x) => x.id !== id) } : s));
      window.dispatchEvent(new Event('reader-comments-changed'));
    },
    [key],
  );
  const comments = key !== null && state.key === key ? state.items : EMPTY;
  return useMemo(() => ({ comments, replace, drop }), [comments, replace, drop]);
}
