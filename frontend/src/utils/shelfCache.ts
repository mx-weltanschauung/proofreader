/**
 * Полка запрашивается один раз за сессию: 22 КБ по проводу и 0.31 с на боевом
 * — терпимо однажды и заметно, если ходить на каждое открытие панели.
 * Неудача не запоминается: следующая попытка сходит заново.
 */
import { shelfApi } from '../services/api';
import type { Shelf } from '../types';

let inFlight: Promise<Shelf> | null = null;

export function loadShelfOnce(): Promise<Shelf> {
  if (!inFlight) {
    inFlight = shelfApi
      .get()
      .then((res) => res.data)
      .catch((err: unknown) => {
        inFlight = null;
        throw err;
      });
  }
  return inFlight;
}
