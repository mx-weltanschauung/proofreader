import { useCallback, useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { closuresApi } from '../../services/api';
import type { ClosureEntry } from '../../types';
import { apiErrorMessage } from '../../utils/apiError';
import { editionPath, workPath } from '../../utils/paths';
import './ClosuresAdmin.css';

const PAGE = 100;

const DATE = new Intl.DateTimeFormat('ru-RU', {
  timeZone: 'Europe/Moscow',
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
});

const KIND_LABEL: Record<ClosureEntry['kind'], string> = {
  work: 'том',
  edition: 'собрание',
  journal: 'журнал',
  chapter: 'глава',
};

/** Куда вести по заглавию: у тома и собрания адрес выводится из номера, у журнала и главы — нет. */
function entityHref(row: ClosureEntry): string | null {
  if (row.kind === 'work') return workPath({ id: row.entity_id });
  if (row.kind === 'edition') return editionPath({ id: row.entity_id });
  return null;
}

export const ClosuresAdmin: React.FC = () => {
  const [rows, setRows] = useState<ClosureEntry[]>([]);
  const [isLoading, setIsLoading] = useState(true);
  const [isMore, setIsMore] = useState(false);
  const [more, setMore] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async (offset: number) => {
    try {
      const { data } = await closuresApi.list({ limit: PAGE, offset });
      setRows((prev) => (offset === 0 ? data : [...prev, ...data]));
      setMore(data.length === PAGE);
      setError('');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось прочитать журнал закрытий'));
    } finally {
      setIsLoading(false);
      setIsMore(false);
    }
  }, []);

  useEffect(() => {
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load(0);
  }, [load]);

  if (isLoading) return <div className="loading-state">Загрузка…</div>;
  if (error) return <div className="error-state">{error}</div>;

  return (
    <div className="closures-admin">
      <h1>Журнал закрытий</h1>
      <p className="closures-admin-note">
        Кто и почему закрывал или открывал тома, собрания, журналы и главы. Причину видит только
        администратор.
      </p>
      {rows.length === 0 ? (
        <p>Закрытий пока не было.</p>
      ) : (
        <table className="closures-admin-table">
          <thead>
            <tr>
              <th>Когда</th>
              <th>Что</th>
              <th>Действие</th>
              <th>Причина</th>
              <th>Кто</th>
            </tr>
          </thead>
          <tbody>
            {rows.map((row) => {
              const href = entityHref(row);
              return (
                <tr key={`${row.kind}-${row.entity_id}-${row.at}`}>
                  <td>{DATE.format(new Date(row.at))}</td>
                  <td>
                    <span className="closures-admin-kind">{KIND_LABEL[row.kind]}</span>{' '}
                    {href ? <Link to={href}>{row.title}</Link> : row.title}
                  </td>
                  <td>{row.closed ? 'закрыто' : 'открыто'}</td>
                  <td>{row.reason}</td>
                  <td>{row.actor_name}</td>
                </tr>
              );
            })}
          </tbody>
        </table>
      )}
      {more && (
        <button
          type="button"
          className="btn btn-secondary"
          disabled={isMore}
          onClick={() => {
            setIsMore(true);
            void load(rows.length);
          }}
        >
          Ещё
        </button>
      )}
    </div>
  );
};
