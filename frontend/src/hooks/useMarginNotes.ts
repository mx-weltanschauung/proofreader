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
  const [state, setState] = useState<{ key: string | null; notes: MarginNote[] }>({
    key: null,
    notes: EMPTY,
  });

  useEffect(() => {
    if (key === null || workId === null) return;
    let alive = true;
    marginNotesApi
      .ofWork(workId)
      .then((r) => {
        if (alive) setState({ key, notes: Array.isArray(r.data) ? r.data : EMPTY });
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
  const add = useCallback(
    (n: MarginNote) =>
      setState((s) => (s.key === key ? { key, notes: [...s.notes, n] } : { key, notes: [n] })),
    [key],
  );
  const replace = useCallback(
    (n: MarginNote) =>
      setState((s) =>
        s.key === key ? { key, notes: s.notes.map((x) => (x.id === n.id ? n : x)) } : s,
      ),
    [key],
  );
  const drop = useCallback(
    (id: number) =>
      setState((s) => (s.key === key ? { key, notes: s.notes.filter((x) => x.id !== id) } : s)),
    [key],
  );

  return useMemo(() => ({ notes, add, replace, drop }), [notes, add, replace, drop]);
}
