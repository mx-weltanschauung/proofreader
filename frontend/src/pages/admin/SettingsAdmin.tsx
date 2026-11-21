import { useEffect, useState, type FormEvent } from 'react';
import { settingsApi } from '../../services/api';
import { apiErrorMessage } from '../../utils/apiError';
import './SettingsAdmin.css';

const MAX_POSTGRES_INTEGER = 2147483647;

function positiveLimit(raw: string): number | null {
  if (!/^[0-9]+$/.test(raw)) return null;
  const value = Number(raw);
  return Number.isSafeInteger(value) && value > 0 && value <= MAX_POSTGRES_INTEGER ? value : null;
}

export const SettingsAdmin: React.FC = () => {
  const [raw, setRaw] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  const [loadError, setLoadError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [isSaving, setIsSaving] = useState(false);
  const [saved, setSaved] = useState(false);

  useEffect(() => {
    let alive = true;
    void settingsApi.get().then(
      (response) => {
        if (!alive) return;
        setRaw(String(response.data.suggestions_per_hour));
        setIsLoading(false);
      },
      (err: unknown) => {
        if (!alive) return;
        setLoadError(apiErrorMessage(err, 'Не удалось загрузить настройки'));
        setIsLoading(false);
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const save = async (event: FormEvent<HTMLFormElement>) => {
    event.preventDefault();
    const value = positiveLimit(raw);
    if (value === null || isSaving) return;
    setIsSaving(true);
    setSaveError('');
    setSaved(false);
    try {
      const response = await settingsApi.update(value);
      setRaw(String(response.data.suggestions_per_hour));
      setSaved(true);
    } catch (err: unknown) {
      setSaveError(apiErrorMessage(err, 'Не удалось сохранить настройки'));
    } finally {
      setIsSaving(false);
    }
  };

  if (isLoading) return <div className="loading-state">Загрузка…</div>;
  if (loadError)
    return (
      <div className="error-state" role="alert">
        {loadError}
      </div>
    );

  return (
    <div className="settings-admin">
      <h1>Настройки</h1>
      <form
        onSubmit={(event) => {
          void save(event);
        }}
      >
        <label htmlFor="suggestions-per-hour">Предложений правок за час</label>
        <input
          id="suggestions-per-hour"
          type="number"
          min="1"
          max={MAX_POSTGRES_INTEGER}
          step="1"
          inputMode="numeric"
          value={raw}
          disabled={isSaving}
          onChange={(event) => {
            setRaw(event.target.value);
            setSaved(false);
            setSaveError('');
          }}
        />
        <p className="settings-admin-note">
          Число предложений от одного адреса за скользящий час. Новый предел начинает действовать не
          позднее чем через минуту.
        </p>
        {saveError && (
          <p className="error-state" role="alert">
            {saveError}
          </p>
        )}
        {saved && <p role="status">Сохранено</p>}
        <button type="submit" disabled={positiveLimit(raw) === null || isSaving}>
          {isSaving ? 'Сохранение…' : 'Сохранить'}
        </button>
      </form>
    </div>
  );
};
