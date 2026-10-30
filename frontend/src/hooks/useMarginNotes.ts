import { useCallback, useEffect, useMemo, useState } from 'react';
import { marginNotesApi } from '../services/api';
import { useAuth } from './useAuth';
import type { MarginNote } from '../types';

export interface MarginNotesState {
  notes: MarginNote[];
  add(n: MarginNote): void;
  replace(n: MarginNote): void;
  drop(id: number): void;
}

const EMPTY: MarginNote[] = [];

interface State {
  key: string | null;
  notes: MarginNote[];
  /** Ответ на список тома уже пришёл; до него правки копятся поверх пустого. */
  loaded: boolean;
  /** Снятые до ответа — ответ, отправленный раньше снятия, их ещё несёт. */
  dropped: number[];
}

/**
 * Свои заметки тома, разложенные по полосам; пустой список и без запроса у
 * анонима.
 *
 * `workId === null` — тоже без запроса: так зовёт хук поверхность, которой
 * заметки уже пришли пропом (ChapterView грузит их сам, на весь экран), —
 * хук зовётся всегда, правило хуков не даёт звать его условно.
 *
 * Список привязан к паре «том, читатель»: вышел читатель или сменился том —
 * прежний список перестаёт отдаваться сразу, не дожидаясь нового ответа, иначе
 * на полях чужого тома мелькнули бы заметки прежнего.
 */
export function useMarginNotes(workId: number | null): MarginNotesState {
  const userId = useAuth((s) => s.user?.id ?? null);
  const key = workId !== null && userId !== null ? `${workId}:${userId}` : null;
  const [state, setState] = useState<State>({
    key: null,
    notes: EMPTY,
    loaded: false,
    dropped: [],
  });

  useEffect(() => {
    if (key === null || workId === null) return;
    let alive = true;
    marginNotesApi
      .ofWork(workId)
      .then((r) => {
        if (!alive) return;
        const list = Array.isArray(r.data) ? r.data : EMPTY;
        // Заметка, записанная, пока список ещё ехал, в ответе может не
        // оказаться (запрос ушёл раньше записи): ответ ложится под неё, а не
        // поверх. Снятая за это время — наоборот, из ответа выпадает.
        setState((s) => {
          if (s.key !== key || s.loaded) return { key, notes: list, loaded: true, dropped: [] };
          const ids = new Set(list.map((n) => n.id));
          const gone = new Set(s.dropped);
          const kept = list
            .filter((n) => !gone.has(n.id))
            .map((n) => s.notes.find((x) => x.id === n.id) ?? n);
          const added = s.notes.filter((n) => !ids.has(n.id));
          return { key, notes: [...kept, ...added], loaded: true, dropped: [] };
        });
      })
      // Поля без заметок — не поломка чтения: текст главы читается и так, а
      // сообщение об ошибке посреди текста мешало бы больше пустых полей.
      .catch(() => {});
    return () => {
      alive = false;
    };
  }, [key, workId]);

  const notes = key !== null && state.key === key ? state.notes : EMPTY;

  // Правки идут через функцию состояния и сверяют ключ: ответ на запись,
  // пришедший после смены тома, не должен лечь в список другого тома.
  // До ответа на список (`s.key !== key`) правки начинают состояние этого
  // ключа заново, с loaded: false, — ответ потом сольётся с ними.
  const add = useCallback(
    (n: MarginNote) =>
      setState((s) =>
        s.key === key
          ? { ...s, notes: [...s.notes, n] }
          : { key, notes: [n], loaded: false, dropped: [] },
      ),
    [key],
  );
  const replace = useCallback(
    (n: MarginNote) =>
      setState((s) =>
        s.key === key
          ? {
              ...s,
              notes: s.notes.some((x) => x.id === n.id)
                ? s.notes.map((x) => (x.id === n.id ? n : x))
                : s.notes,
            }
          : s,
      ),
    [key],
  );
  const drop = useCallback(
    (id: number) =>
      setState((s) =>
        s.key === key
          ? {
              ...s,
              notes: s.notes.filter((x) => x.id !== id),
              dropped: s.loaded ? s.dropped : [...s.dropped, id],
            }
          : { key, notes: EMPTY, loaded: false, dropped: [id] },
      ),
    [key],
  );

  useEffect(() => {
    const created = (event: Event) => {
      const detail = (event as CustomEvent<{ note: MarginNote; userId: number | null }>).detail;
      if (detail?.userId === userId && detail?.note.work_id === workId) add(detail.note);
    };
    window.addEventListener('margin-note-created', created);
    return () => window.removeEventListener('margin-note-created', created);
  }, [userId, workId, add]);

  return useMemo(() => ({ notes, add, replace, drop }), [notes, add, replace, drop]);
}
