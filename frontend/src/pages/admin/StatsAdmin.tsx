import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { statsApi } from '../../services/api';
import type {
  StatsCountRow,
  StatsCrawlerRow,
  StatsDeviceRow,
  StatsHealth,
  StatsSearches,
  StatsTopKind,
  StatsTopRow,
  StatsTrafficRow,
} from '../../types';
import { chapterPath, editionPath, workPath } from '../../utils/paths';
import { calendarTotals, moscowToday } from '../../utils/statsTotals';
import { apiErrorMessage } from '../../utils/apiError';
import './StatsAdmin.css';

const PERIODS = [1, 7, 30, 90] as const;
const KINDS: { kind: StatsTopKind; label: string }[] = [
  { kind: 'work', label: 'Тома' },
  { kind: 'chapter', label: 'Главы' },
  { kind: 'document', label: 'Разборы' },
  { kind: 'collection', label: 'Подборки' },
  { kind: 'concept', label: 'Понятия' },
  { kind: 'edition', label: 'Издания' },
];
const CHANNELS: { key: string; label: string }[] = [
  { key: 'spa', label: 'Читатели' },
  { key: 'md', label: 'Текст для нейросетей' },
  { key: 'download', label: 'Скачивания' },
  { key: 'seo', label: 'Краулеры' },
  { key: 'mcp', label: 'MCP' },
  { key: 'opds', label: 'Каталог OPDS' },
];
const DEVICE_LABELS: Record<string, string> = {
  narrow: 'Телефон',
  medium: 'Планшет, узкое окно',
  wide: 'Компьютер',
  '': 'Не указано',
};

// Одна загрузка на раздел: сбой одного запроса не гасит остальные разделы.
function useLoad<T>(load: () => Promise<{ data: T }>, deps: unknown[]) {
  const [data, setData] = useState<T | null>(null);
  const [error, setError] = useState('');
  useEffect(() => {
    let alive = true;
    load()
      .then((r) => {
        if (alive) {
          setData(r.data);
          setError('');
        }
      })
      .catch((e: unknown) => {
        if (alive) setError(apiErrorMessage(e, 'Не удалось загрузить'));
      });
    return () => {
      alive = false;
    };
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, deps);
  return { data, error };
}

function topHref(kind: StatsTopKind, row: StatsTopRow): string | null {
  if (row.missing) return null;
  switch (kind) {
    case 'work':
      return workPath({ id: row.work_id });
    case 'chapter':
      return chapterPath({ id: row.work_id }, { id: row.entity_id });
    case 'edition':
      return editionPath({ id: row.entity_id });
    case 'document':
      return `/documents/${row.slug_key}`;
    case 'collection':
      return `/collections/${row.slug_key}`;
    case 'concept':
      return `/concepts/${row.slug_key}`;
  }
}

function topLabel(kind: StatsTopKind, row: StatsTopRow): string {
  if (row.missing) return `снято (#${row.entity_id || row.work_id || row.slug_key})`;
  if (kind === 'chapter' && row.work_title) return `${row.title} — ${row.work_title}`;
  return row.title || row.slug_key;
}

export const StatsAdmin: React.FC = () => {
  const [days, setDays] = useState<number>(7);
  const [kind, setKind] = useState<StatsTopKind>('work');

  const traffic = useLoad<StatsTrafficRow[]>(() => statsApi.traffic(30), []);
  const top = useLoad<StatsTopRow[]>(() => statsApi.top(kind, days), [kind, days]);
  const refs = useLoad<StatsCountRow[]>(() => statsApi.referrers(days), [days]);
  const searches = useLoad<StatsSearches>(() => statsApi.searches(days), [days]);
  const crawlers = useLoad<StatsCrawlerRow[]>(() => statsApi.crawlers(days), [days]);
  const devices = useLoad<StatsDeviceRow[]>(() => statsApi.devices(days), [days]);
  const health = useLoad<StatsHealth>(() => statsApi.health(), []);

  // Столбики: по дню — сумма по каналам, стопкой.
  const byDay = useMemo(() => {
    const m = new Map<string, Record<string, number>>();
    for (const r of traffic.data ?? []) {
      const d = m.get(r.day) ?? {};
      d[r.channel] = r.views;
      m.set(r.day, d);
    }
    return [...m.entries()].sort(([a], [b]) => a.localeCompare(b));
  }, [traffic.data]);
  const maxDay = Math.max(1, ...byDay.map(([, c]) => Object.values(c).reduce((a, b) => a + b, 0)));

  // Окна считаются по календарю (Москва), а не по последним N дням в данных:
  // без визитов сегодня «Сегодня» не должно показывать вчера.
  const totals = useMemo(
    () => calendarTotals(traffic.data ?? [], moscowToday(new Date())),
    [traffic.data],
  );

  const crawlerTotals = useMemo(() => {
    const m = new Map<string, number>();
    for (const r of crawlers.data ?? []) m.set(r.agent, (m.get(r.agent) ?? 0) + r.views);
    return [...m.entries()].sort((a, b) => b[1] - a[1]);
  }, [crawlers.data]);

  // Доля — по посетителе-дням: человек, открывший десять глав, — один, а не десять.
  // «Не указано» (старый клиент, сутки до выкатки класса) в знаменатель не входит:
  // иначе первую неделю после выкатки доля телефонов выходила бы заниженной —
  // ровно в ту неделю, когда с этого экрана снимается базовая линия.
  const deviceTotals = useMemo(() => {
    const m = new Map<string, number>();
    for (const r of devices.data ?? []) m.set(r.device, (m.get(r.device) ?? 0) + r.visitors);
    const known = [...m.entries()].reduce((a, [d, v]) => (d ? a + v : a), 0);
    return { rows: [...m.entries()].sort((a, b) => b[1] - a[1]), known };
  }, [devices.data]);

  return (
    <div className="stats-admin">
      <h1>Статистика</h1>
      <p className="stats-admin-note">
        Уникальные считаются внутри суток: за период длиннее дня это посетителе-дни, а не люди.
        Сотрудники не учитываются.
      </p>

      <section aria-label="Обзор" className="stats-admin-section">
        <h2>Обзор за 30 дней</h2>
        {traffic.error && <p className="stats-admin-error">{traffic.error}</p>}
        <dl className="stats-admin-totals">
          <div>
            <dt>Читатели сегодня</dt>
            <dd>
              {totals.day.views} просмотров, {totals.day.visitors} посетителей
            </dd>
          </div>
          <div>
            <dt>Читатели за 7 дней</dt>
            <dd>
              {totals.week.views} просмотров, {totals.week.visitors} посетителе-дней
            </dd>
          </div>
          <div>
            <dt>Читатели за 30 дней</dt>
            <dd>
              {totals.month.views} просмотров, {totals.month.visitors} посетителе-дней
            </dd>
          </div>
        </dl>
        <svg
          className="stats-admin-bars"
          viewBox={`0 0 ${Math.max(byDay.length, 1) * 12} 100`}
          preserveAspectRatio="none"
          role="img"
          aria-label="Просмотры по дням"
        >
          {byDay.map(([day, counts], i) => {
            let y = 100;
            return CHANNELS.map(({ key }) => {
              const v = counts[key] ?? 0;
              const h = (v / maxDay) * 100;
              y -= h;
              return v > 0 ? (
                <rect
                  key={`${day}-${key}`}
                  x={i * 12 + 1}
                  y={y}
                  width={10}
                  height={h}
                  className={`stats-bar stats-bar-${key}`}
                >
                  <title>{`${day.slice(0, 10)}: ${key} ${v}`}</title>
                </rect>
              ) : null;
            });
          })}
        </svg>
        <ul className="stats-admin-legend">
          {CHANNELS.map((c) => (
            <li key={c.key}>
              <span className={`stats-swatch stats-bar-${c.key}`} />
              {c.label}
            </li>
          ))}
        </ul>
      </section>

      <div className="stats-admin-periods" role="group" aria-label="Период">
        {PERIODS.map((p) => (
          <button key={p} type="button" aria-pressed={days === p} onClick={() => setDays(p)}>
            {p === 1 ? 'Сутки' : `${p} дней`}
          </button>
        ))}
      </div>

      <section aria-label="Что читают" className="stats-admin-section">
        <h2>Что читают</h2>
        <div className="stats-admin-kinds" role="group" aria-label="Вид">
          {KINDS.map((k) => (
            <button
              key={k.kind}
              type="button"
              aria-pressed={kind === k.kind}
              onClick={() => setKind(k.kind)}
            >
              {k.label}
            </button>
          ))}
        </div>
        {top.error && <p className="stats-admin-error">{top.error}</p>}
        <div className="stats-admin-scroll">
          <table className="stats-admin-table">
            <thead>
              <tr>
                <th>Что</th>
                <th>Просмотры</th>
                <th>Посетителе-дни</th>
              </tr>
            </thead>
            <tbody>
              {(top.data ?? []).map((row) => {
                const href = topHref(kind, row);
                const label = topLabel(kind, row);
                return (
                  <tr
                    key={`${row.work_id}-${row.entity_id}-${row.slug_key}`}
                    className={row.missing ? 'is-missing' : ''}
                  >
                    <td>{href ? <Link to={href}>{label}</Link> : label}</td>
                    <td>{row.views}</td>
                    <td>{row.visitors}</td>
                  </tr>
                );
              })}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Откуда" className="stats-admin-section">
        <h2>Откуда приходят</h2>
        {refs.error && <p className="stats-admin-error">{refs.error}</p>}
        <div className="stats-admin-scroll">
          <table className="stats-admin-table">
            <tbody>
              {(refs.data ?? []).map((r) => (
                <tr key={r.key}>
                  <td>{r.key}</td>
                  <td>{r.views}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Устройства" className="stats-admin-section">
        <h2>С каких экранов</h2>
        {devices.error && <p className="stats-admin-error">{devices.error}</p>}
        <div className="stats-admin-scroll">
          <table className="stats-admin-table">
            <tbody>
              {deviceTotals.rows.map(([device, visitors]) => (
                <tr key={device || 'none'}>
                  <td>{DEVICE_LABELS[device] ?? device}</td>
                  <td>{visitors}</td>
                  <td>
                    {device && deviceTotals.known
                      ? `${Math.round((visitors * 100) / deviceTotals.known)}%`
                      : '—'}
                  </td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Что ищут" className="stats-admin-section">
        <h2>Что ищут</h2>
        {searches.error && <p className="stats-admin-error">{searches.error}</p>}
        <div className="stats-admin-scroll">
          <table className="stats-admin-table">
            <tbody>
              {(searches.data?.frequent ?? []).map((s) => (
                <tr key={s.query}>
                  <td>{s.query}</td>
                  <td>{s.count}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Ищут и не находят" className="stats-admin-section">
        <h2>Ищут и не находят</h2>
        <div className="stats-admin-scroll">
          <table className="stats-admin-table">
            <tbody>
              {(searches.data?.zero ?? []).map((s) => (
                <tr key={s.query}>
                  <td>{s.query}</td>
                  <td>{s.zero_hits}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Краулеры" className="stats-admin-section">
        <h2>Краулеры</h2>
        {crawlers.error && <p className="stats-admin-error">{crawlers.error}</p>}
        <div className="stats-admin-scroll">
          <table className="stats-admin-table">
            <tbody>
              {crawlerTotals.map(([agent, views]) => (
                <tr key={agent}>
                  <td>{agent}</td>
                  <td>{views}</td>
                </tr>
              ))}
            </tbody>
          </table>
        </div>
      </section>

      <section aria-label="Здоровье" className="stats-admin-section">
        <h2>Здоровье</h2>
        {health.error && <p className="stats-admin-error">{health.error}</p>}
        {health.data && (
          <>
            <p>
              С момента старта процесса, {new Date(health.data.started_at).toLocaleString('ru-RU')}
            </p>
            <div className="stats-admin-scroll">
              <table className="stats-admin-table">
                <thead>
                  <tr>
                    <th>Маршрут</th>
                    <th>Запросов</th>
                    <th>p50, мс</th>
                    <th>p95, мс</th>
                    <th>5xx</th>
                    <th>503</th>
                  </tr>
                </thead>
                <tbody>
                  {health.data.routes.map((r) => (
                    <tr key={r.route}>
                      <td>{r.route}</td>
                      <td>{r.requests}</td>
                      <td>{Math.round(r.p50_ms)}</td>
                      <td>{Math.round(r.p95_ms)}</td>
                      <td>{r.status_5xx}</td>
                      <td>{r.status_503}</td>
                    </tr>
                  ))}
                </tbody>
              </table>
            </div>
            <dl className="stats-admin-values">
              {Object.entries(health.data.values)
                .sort(([a], [b]) => a.localeCompare(b))
                .map(([k, v]) => (
                  <div key={k}>
                    <dt>{k}</dt>
                    <dd>{Number.isInteger(v) ? v : v.toFixed(2)}</dd>
                  </div>
                ))}
            </dl>
          </>
        )}
      </section>
    </div>
  );
};
