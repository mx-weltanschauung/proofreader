import { useEffect } from 'react';
import { sendVisiblePage } from '../services/hit';
import { moscowToday } from '../utils/statsTotals';

// Общий набор на одну вкладку: переход из главы в поток не дублирует событие.
let currentDay = '';
const sent = new Set<string>();

export function useVisiblePageHit(
  workId: number | null,
  pageNumber: number | null,
  ready: boolean,
): void {
  useEffect(() => {
    const sendIfVisible = () => {
      if (
        !ready ||
        document.hidden ||
        workId === null ||
        pageNumber === null ||
        workId < 1 ||
        pageNumber < 1
      )
        return;
      const day = moscowToday(new Date());
      if (currentDay !== day) {
        currentDay = day;
        sent.clear();
      }
      const key = `${workId}/${pageNumber}`;
      if (sent.has(key)) return;
      // Защита от огромной таблицы при долгой жизни вкладки.
      if (sent.size >= 5000) sent.delete(sent.values().next().value as string);
      sent.add(key);
      sendVisiblePage(workId, pageNumber);
    };
    sendIfVisible();
    document.addEventListener('visibilitychange', sendIfVisible);
    return () => document.removeEventListener('visibilitychange', sendIfVisible);
  }, [workId, pageNumber, ready]);
}
