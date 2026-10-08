import { useCallback, useEffect, useState } from 'react';
import toast from 'react-hot-toast';
import { cacheApi } from '../../services/api';
import type { CacheStats } from '../../types';
import { apiErrorMessage } from '../../utils/apiError';
import './CacheAdmin.css';

function formatBytes(bytes: number): string {
  if (bytes <= 0) return '0 МБ';
  return `${(bytes / (1024 * 1024)).toFixed(1)} МБ`;
}

function formatAge(seconds: number): string {
  if (seconds <= 0) return '—';
  const minutes = Math.round(seconds / 60);
  return `${minutes} мин`;
}

export const CacheAdmin: React.FC = () => {
  const [stats, setStats] = useState<CacheStats | null>(null);
  const [isLoading, setIsLoading] = useState(true);
  const [isPurging, setIsPurging] = useState(false);
  const [confirming, setConfirming] = useState(false);
  const [error, setError] = useState('');

  const load = useCallback(async () => {
    try {
      const response = await cacheApi.stats();
      setStats(response.data);
      setError('');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось прочитать состояние кэша'));
    } finally {
      setIsLoading(false);
    }
  }, []);

  useEffect(() => {
    // set-state-in-effect: все setState загрузчика стоят после await, синхронного
    // каскада рендеров нет — см. пояснение в UsersList.tsx (facebook/react#34905).
    // eslint-disable-next-line react-hooks/set-state-in-effect
    void load();
  }, [load]);

  const purge = useCallback(async () => {
    setIsPurging(true);
    try {
      const response = await cacheApi.purgeAll();
      toast.success(
        `Снято ${response.data.removed} файлов, освобождено ${formatBytes(response.data.bytes)}`,
      );
      setConfirming(false);
      await load();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось сбросить кэш'));
    } finally {
      setIsPurging(false);
    }
  }, [load]);

  if (isLoading) return <div className="loading-state">Загрузка…</div>;
  if (error) return <div className="error-state">{error}</div>;

  return (
    <div className="cache-admin">
      <h1>Кэш глав</h1>
      <p className="cache-admin-note">
        Готовые главы лежат на диске час. Правка полосы доезжает до режима чтения не сразу —
        сбросить кэш одной главы можно кнопкой на самой главе.
      </p>

      {stats && !stats.enabled ? (
        <p className="cache-admin-off">Кэш выключен: каталог не задан (PAGE_CACHE_DIR).</p>
      ) : (
        <dl className="cache-admin-stats">
          <div>
            <dt>Файлов</dt>
            <dd>{stats?.files ?? 0}</dd>
          </div>
          <div>
            <dt>Объём</dt>
            <dd>{formatBytes(stats?.bytes ?? 0)}</dd>
          </div>
          <div>
            <dt>Самому старому</dt>
            <dd>{formatAge(stats?.oldest_age_seconds ?? 0)}</dd>
          </div>
        </dl>
      )}

      {confirming ? (
        <div className="cache-admin-confirm">
          <span>Сбросить весь кэш? Ближайшие читатели снова оплатят сборку глав.</span>
          <button type="button" className="btn btn-danger" onClick={purge} disabled={isPurging}>
            {isPurging ? 'Сбрасываю…' : 'Да, сбросить'}
          </button>
          <button type="button" className="btn btn-secondary" onClick={() => setConfirming(false)}>
            Отмена
          </button>
        </div>
      ) : (
        <button type="button" className="btn btn-secondary" onClick={() => setConfirming(true)}>
          Сбросить весь кэш
        </button>
      )}
    </div>
  );
};
