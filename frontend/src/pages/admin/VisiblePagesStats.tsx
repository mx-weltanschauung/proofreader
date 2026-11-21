import { useEffect, useMemo, useState } from 'react';
import { Link } from 'react-router-dom';
import { statsApi } from '../../services/api';
import type { VisiblePageStats as VisiblePageStatsData } from '../../types';
import { pagePath } from '../../utils/paths';
import { apiErrorMessage } from '../../utils/apiError';
import './VisiblePagesStats.css';

interface Props {
  workId: number;
  chapterId: number;
  days: number;
}

export const VisiblePagesStats: React.FC<Props> = ({ workId, chapterId, days }) => {
  const key = `${workId}/${chapterId}/${days}`;
  const [response, setResponse] = useState<{
    key: string;
    data?: VisiblePageStatsData;
    error?: string;
  } | null>(null);
  const current = response?.key === key ? response : null;
  const data = current?.data;
  useEffect(() => {
    let alive = true;
    statsApi
      .visiblePages(workId, chapterId, days)
      .then((res) => {
        if (alive) setResponse({ key, data: res.data });
      })
      .catch((err: unknown) => {
        if (alive) setResponse({ key, error: apiErrorMessage(err, 'Не удалось загрузить полосы') });
      });
    return () => {
      alive = false;
    };
  }, [workId, chapterId, days, key]);
  const max = useMemo(() => Math.max(1, ...(data?.pages ?? []).map((p) => p.visitors)), [data]);
  return (
    <section aria-label="Видимые полосы" className="stats-admin-section">
      <h2>Видимые полосы главы #{chapterId}</h2>
      <p>
        «Увидели полосу» означает появление в области чтения. За период считаются посетителе-дни.
      </p>
      {current?.error && <p className="stats-admin-error">{current.error}</p>}
      {data?.collecting_since === null && <p>Данных пока нет</p>}
      {data?.collecting_since && (
        <>
          <p>
            Сбор с {data.collecting_since.slice(0, 10)} · последние {days} дней
          </p>
          <div className="visible-pages-scroll">
            <table className="stats-admin-table visible-pages-table">
              <thead>
                <tr>
                  <th>Полоса</th>
                  <th>Посетителе-дни</th>
                  <th>Увидели полосу</th>
                </tr>
              </thead>
              <tbody>
                {data.pages.map((p) => (
                  <tr key={p.page_number}>
                    <td>
                      <Link to={pagePath({ id: workId }, p.page_number)}>{p.page_number}</Link>
                    </td>
                    <td>{p.visitors}</td>
                    <td>
                      <div
                        className="visible-pages-bar"
                        style={{ width: `${(p.visitors / max) * 100}%` }}
                        aria-label={`${p.visitors} посетителе-дней`}
                      />
                    </td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
        </>
      )}
    </section>
  );
};
