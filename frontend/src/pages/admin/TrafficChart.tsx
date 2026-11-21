import { useMemo, useState } from 'react';
import type { StatsTrafficRow } from '../../types';
import { moscowToday } from '../../utils/statsTotals';

const CHANNELS = [
  { key: 'spa', label: 'Читатели' },
  { key: 'md', label: 'Текст для нейросетей' },
  { key: 'download', label: 'Скачивания' },
  { key: 'seo', label: 'Краулеры' },
  { key: 'mcp', label: 'MCP' },
  { key: 'opds', label: 'Каталог OPDS' },
] as const;

type Days = 7 | 30 | 90;
interface Props {
  rows: StatsTrafficRow[];
  days: Days;
  onDaysChange: (days: Days) => void;
}

function calendarDays(today: string, days: number): string[] {
  const start = new Date(`${today}T00:00:00Z`);
  start.setUTCDate(start.getUTCDate() - days + 1);
  return Array.from({ length: days }, (_, i) => {
    const d = new Date(start);
    d.setUTCDate(d.getUTCDate() + i);
    return d.toISOString().slice(0, 10);
  });
}

export const TrafficChart: React.FC<Props> = ({ rows, days, onDaysChange }) => {
  const [active, setActive] = useState<Set<string>>(() => new Set(CHANNELS.map((c) => c.key)));
  const today = moscowToday(new Date());
  const dates = useMemo(() => calendarDays(today, days), [today, days]);
  const byDay = useMemo(() => {
    const m = new Map<string, Record<string, number>>();
    for (const row of rows) {
      const day = row.day.slice(0, 10);
      const counts = m.get(day) ?? {};
      counts[row.channel] = (counts[row.channel] ?? 0) + row.views;
      m.set(day, counts);
    }
    return dates.map((day) => ({
      day,
      counts: m.get(day) ?? {},
      total: CHANNELS.reduce(
        (sum, c) => sum + (active.has(c.key) ? (m.get(day)?.[c.key] ?? 0) : 0),
        0,
      ),
    }));
  }, [rows, dates, active]);
  const maxDay = Math.max(1, ...byDay.map((d) => d.total));
  const toggle = (key: string) =>
    setActive((current) => {
      const next = new Set(current);
      if (next.has(key)) next.delete(key);
      else next.add(key);
      return next;
    });
  return (
    <section aria-label="График посещаемости">
      <h3>Посещаемость по дням</h3>
      <div className="stats-admin-periods" role="group" aria-label="Период графика">
        {([7, 30, 90] as const).map((n) => (
          <button key={n} type="button" aria-pressed={days === n} onClick={() => onDaysChange(n)}>
            {n} дней
          </button>
        ))}
      </div>
      <div className="stats-admin-legend" role="group" aria-label="Каналы графика">
        {CHANNELS.map((c) => (
          <button
            key={c.key}
            type="button"
            aria-pressed={active.has(c.key)}
            onClick={() => toggle(c.key)}
          >
            <span className={`stats-swatch stats-bar-${c.key}`} aria-hidden="true" />
            {c.label}
          </button>
        ))}
      </div>
      {active.size === 0 ? (
        <p>Выберите канал</p>
      ) : (
        <svg
          className="stats-admin-bars"
          viewBox={`0 0 ${days * 12} 100`}
          preserveAspectRatio="none"
          role="img"
          aria-label={`Просмотры за ${days} дней`}
        >
          {byDay.map(({ day, counts, total }, i) => {
            let y = 100;
            return (
              <g key={day} data-day={day} data-total={total}>
                <title>{`${day}: ${total} просмотров`}</title>
                {CHANNELS.filter((c) => active.has(c.key)).map((c) => {
                  const value = counts[c.key] ?? 0;
                  const height = (value / maxDay) * 100;
                  y -= height;
                  return value > 0 ? (
                    <rect
                      key={c.key}
                      x={i * 12 + 1}
                      y={y}
                      width={10}
                      height={height}
                      className={`stats-bar stats-bar-${c.key}`}
                    >
                      <title>{`${day}: ${c.label} ${value}`}</title>
                    </rect>
                  ) : null;
                })}
              </g>
            );
          })}
        </svg>
      )}
    </section>
  );
};
