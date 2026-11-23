import { useCallback, useContext, useEffect, useMemo, useState } from 'react';
import { ReadingPreferencesContext } from '../contexts/readingPrefsContext';
import { DEFAULT_PREFS } from '../contexts/readingPrefs';
import { useAuth } from './useAuth';
import { readerCommentsApi } from '../services/api';
import type { ReaderComment, ReaderCommentThread } from '../types';

const EMPTY: ReaderComment[] = [];
export interface ReaderCommentsState {
  comments: ReaderComment[];
  requestedThread?: ReaderCommentThread;
  replace(comment: ReaderComment): void;
  drop(id: number): void;
}
/** Ключ включает сессию и настройку: старый ответ не раскрывает чужие тексты. */
export function useReaderComments(
  workId: number | null,
  threadId: number | null = null,
): ReaderCommentsState {
  const userId = useAuth((s) => s.user?.id ?? null);
  const context = useContext(ReadingPreferencesContext);
  const show = context?.prefs.readerComments ?? DEFAULT_PREFS.readerComments;
  const key = workId !== null && (show || userId !== null) ? `${workId}:${userId}:${show}` : null;
  // Ссылка из личного списка открывает конкретную ветку и при скрытых комментариях.
  const requestedKey = threadId !== null && key !== null ? `${workId}:${userId}:${threadId}` : null;
  const [state, setState] = useState<{ key: string | null; items: ReaderComment[] }>({
    key: null,
    items: EMPTY,
  });
  const [refresh, setRefresh] = useState(0);
  const [requested, setRequested] = useState<{
    key: string;
    thread: ReaderCommentThread;
  } | null>(null);
  useEffect(() => {
    if (workId === null) return;
    const changed = () => setRefresh((v) => v + 1);
    const visible = () => {
      if (document.visibilityState === 'visible') changed();
    };
    window.addEventListener('reader-comments-changed', changed);
    document.addEventListener('visibilitychange', visible);
    return () => {
      window.removeEventListener('reader-comments-changed', changed);
      document.removeEventListener('visibilitychange', visible);
    };
  }, [workId]);
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
  useEffect(() => {
    if (threadId === null || requestedKey === null) return;
    let alive = true;
    void readerCommentsApi
      .thread(threadId)
      .then(({ data }) => {
        if (!alive) return;
        setRequested(
          data.root.id === threadId && data.root.work_id === workId
            ? { key: requestedKey, thread: data }
            : null,
        );
      })
      .catch(() => {
        if (alive) setRequested(null);
      });
    return () => {
      alive = false;
    };
  }, [requestedKey, threadId, workId, refresh]);
  const replace = useCallback(
    (c: ReaderComment) => {
      setState((s) =>
        s.key === key ? { ...s, items: s.items.map((x) => (x.id === c.id ? c : x)) } : s,
      );
      setRequested((s) =>
        s?.key === requestedKey && s.thread.root.id === c.id
          ? { ...s, thread: { ...s.thread, root: c } }
          : s,
      );
      window.dispatchEvent(new Event('reader-comments-changed'));
    },
    [key, requestedKey],
  );
  const drop = useCallback(
    (id: number) => {
      setState((s) => (s.key === key ? { ...s, items: s.items.filter((x) => x.id !== id) } : s));
      setRequested((s) => (s?.key === requestedKey && s.thread.root.id === id ? null : s));
      window.dispatchEvent(new Event('reader-comments-changed'));
    },
    [key, requestedKey],
  );
  const items = key !== null && state.key === key ? state.items : EMPTY;
  const requestedThread = requested?.key === requestedKey ? requested.thread : undefined;
  return useMemo(() => {
    const root = requestedThread?.root;
    const comments = root ? [root, ...items.filter((c) => c.id !== root.id)] : items;
    return { comments, requestedThread, replace, drop };
  }, [items, requestedThread, replace, drop]);
}
