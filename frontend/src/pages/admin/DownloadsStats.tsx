import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { statsApi } from '../../services/api';
import type { StatsDownloads } from '../../types';
import { chapterPath, workPath } from '../../utils/paths';
import { apiErrorMessage } from '../../utils/apiError';

const KIND: Record<string, string> = {
  work: 'Том',
  chapter: 'Глава',
  collection: 'Подборка',
  archive: 'Архив',
};
const FORMATS: Record<string, string> = {
  epub: 'EPUB',
  fb2: 'FB2',
  pdf: 'PDF',
  md: 'MD',
  html: 'HTML',
  m3u: 'M3U',
};
const formatLabel = (format: string) => (format ? (FORMATS[format] ?? format) : '—');

export const DownloadsStats: React.FC<{ days: number }> = ({ days }) => {
  const [response, setResponse] = useState<{
    days: number;
    data?: StatsDownloads;
    error?: string;
  } | null>(null);
  const current = response?.days === days ? response : null;
  useEffect(() => {
    let alive = true;
    statsApi
      .downloads(days)
      .then((res) => {
        if (alive) setResponse({ days, data: res.data });
      })
      .catch((err: unknown) => {
        if (alive)
          setResponse({ days, error: apiErrorMessage(err, 'Не удалось загрузить выдачи') });
      });
    return () => {
      alive = false;
    };
  }, [days]);
  return (
    <section aria-label="Выдачи файлов" className="stats-admin-section">
      <h2>Выдачи файлов</h2>
      <p>
        Считаются успешные ответы на запрос файла, а не завершённые сохранения. В выдачи могут
        входить сотрудники.
      </p>
      {current?.error && <p className="stats-admin-error">{current.error}</p>}
      {current?.data && (
        <>
          <h3>По виду и формату</h3>
          <div className="stats-admin-scroll">
            <table className="stats-admin-table">
              <thead>
                <tr>
                  <th>Вид</th>
                  <th>Формат</th>
                  <th>Выдано</th>
                </tr>
              </thead>
              <tbody>
                {current.data.summary.map((s) => (
                  <tr key={`${s.kind}/${s.format}`}>
                    <td>{KIND[s.kind] ?? s.kind}</td>
                    <td>{formatLabel(s.format)}</td>
                    <td>{s.views}</td>
                  </tr>
                ))}
              </tbody>
            </table>
          </div>
          <h3>Тома и главы</h3>
          <div className="stats-admin-scroll">
            <table className="stats-admin-table">
              <thead>
                <tr>
                  <th>Что</th>
                  <th>Формат</th>
                  <th>Выдано</th>
                </tr>
              </thead>
              <tbody>
                {current.data.top.map((r) => (
                  <tr key={`${r.kind}/${r.work_id}/${r.entity_id}/${r.format}`}>
                    <td>
                      {r.missing ? (
                        `снято (#${r.entity_id || r.work_id})`
                      ) : (
                        <Link
                          to={
                            r.kind === 'chapter'
                              ? chapterPath({ id: r.work_id }, { id: r.entity_id })
                              : workPath({ id: r.work_id })
                          }
                        >
                          {r.title}
                        </Link>
                      )}
                    </td>
                    <td>{formatLabel(r.format)}</td>
                    <td>{r.views}</td>
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
