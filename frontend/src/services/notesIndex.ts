import { notesApi } from './api';
import type { NoteIndexEntry } from '../types';

const cache = new Map<number, Promise<Map<number, NoteIndexEntry>>>();

// Fetch a work's note index once and memoize it as number -> entry.
export function getWorkNotesIndex(workId: number): Promise<Map<number, NoteIndexEntry>> {
  let pending = cache.get(workId);
  if (!pending) {
    pending = notesApi
      .list(workId)
      .then((res) => {
        const m = new Map<number, NoteIndexEntry>();
        for (const n of res.data) m.set(n.number, n);
        return m;
      })
      .catch((err) => {
        cache.delete(workId); // allow retry on next call
        throw err;
      });
    cache.set(workId, pending);
  }
  return pending;
}
