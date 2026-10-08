import type { StatsTrafficRow } from '../types';

/** Сегодняшняя дата в Москве, 'YYYY-MM-DD' (sv-SE печатает в ISO-порядке). */
export function moscowToday(now: Date): string {
  return new Intl.DateTimeFormat('sv-SE', { timeZone: 'Europe/Moscow' }).format(now);
}

/** Итоги по читателям (канал spa) за календарные окна в 1, 7 и 30 дней,
 *  оканчивающиеся todayIso. Строки сравниваются по дате (первые 10 знаков day),
 *  арифметика дней — в UTC над датой без времени, чтобы часовой пояс не сдвигал. */
export function calendarTotals(rows: StatsTrafficRow[], todayIso: string) {
  const startOf = (n: number) => {
    const d = new Date(`${todayIso}T00:00:00Z`);
    d.setUTCDate(d.getUTCDate() - (n - 1));
    return d.toISOString().slice(0, 10);
  };
  const window = (n: number) => {
    const from = startOf(n);
    const inside = rows.filter((r) => {
      const day = r.day.slice(0, 10);
      return r.channel === 'spa' && day >= from && day <= todayIso;
    });
    return {
      views: inside.reduce((a, r) => a + r.views, 0),
      visitors: inside.reduce((a, r) => a + r.visitors, 0),
    };
  };
  return { day: window(1), week: window(7), month: window(30) };
}
