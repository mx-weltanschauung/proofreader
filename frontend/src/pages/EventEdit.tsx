import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import toast from 'react-hot-toast';
import { eventsApi } from '../services/api';
import { AgendaEditor } from '../components/AgendaEditor';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import type { EventDetail, EventOrganizationDetail } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { eventPath } from '../utils/paths';
import './EventEdit.css';

/** Правка события: шапка и повестка (повестка — отдельным сохранением). */
export const EventEdit: React.FC = () => {
  const { slug = '', eventSlug = '' } = useParams<{ slug: string; eventSlug: string }>();
  const [event, setEvent] = useState<EventDetail | null>(null);
  const [org, setOrg] = useState<EventOrganizationDetail | null>(null);
  const [form, setForm] = useState({ title: '', dates: '', place: '', description: '', kindId: 0 });
  const [error, setError] = useState('');
  const [saveError, setSaveError] = useState('');
  const [loading, setLoading] = useState(true);
  const [saving, setSaving] = useState(false);

  useDocumentTitle(event ? `Правка — ${event.title}` : 'Правка события');

  useEffect(() => {
    const load = async () => {
      try {
        const [e, o] = await Promise.all([
          eventsApi.event(slug, eventSlug),
          eventsApi.organization(slug),
        ]);
        setEvent(e.data);
        setOrg(o.data);
        setForm({
          title: e.data.title,
          dates: e.data.dates,
          place: e.data.place,
          description: e.data.description,
          kindId: e.data.kind_id,
        });
      } catch (err: unknown) {
        setError(apiErrorMessage(err, 'Не удалось загрузить событие'));
      } finally {
        setLoading(false);
      }
    };
    void load();
  }, [slug, eventSlug]);

  if (loading) return <div className="loading-state">Загружаем событие…</div>;
  if (error || !event || !org) return <div className="error-message">{error || 'Не найдено'}</div>;

  const set = (patch: Partial<typeof form>) => setForm((f) => ({ ...f, ...patch }));

  // PUT — полная замена: уходят все поля шапки.
  const save = async () => {
    setSaving(true);
    setSaveError('');
    try {
      await eventsApi.updateEvent(event.id, {
        kind_id: form.kindId,
        title: form.title,
        dates: form.dates,
        place: form.place,
        description: form.description,
        position: event.position,
      });
      toast.success('Событие сохранено');
    } catch (err: unknown) {
      setSaveError(apiErrorMessage(err, 'Не удалось сохранить событие'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <div className="event-edit">
      <p>
        <Link to={eventPath(slug, eventSlug)}>← К событию</Link>
      </p>
      <h1>Правка события</h1>
      <div className="event-edit-header">
        <label>
          Заглавие
          <input value={form.title} onChange={(e) => set({ title: e.target.value })} />
        </label>
        <label>
          Даты
          <input value={form.dates} onChange={(e) => set({ dates: e.target.value })} />
        </label>
        <label>
          Место
          <input value={form.place} onChange={(e) => set({ place: e.target.value })} />
        </label>
        <label>
          Описание
          <textarea
            rows={4}
            value={form.description}
            onChange={(e) => set({ description: e.target.value })}
          />
        </label>
        <label>
          Вид
          <select value={form.kindId} onChange={(e) => set({ kindId: Number(e.target.value) })}>
            {org.kinds.map((k) => (
              <option key={k.id} value={k.id}>
                {k.title}
              </option>
            ))}
          </select>
        </label>
        <button type="button" onClick={() => void save()} disabled={saving}>
          Сохранить
        </button>
        {saveError && (
          <div className="event-edit-error" role="alert">
            {saveError}
          </div>
        )}
      </div>
      <AgendaEditor eventId={event.id} initial={event.sections} />
    </div>
  );
};
