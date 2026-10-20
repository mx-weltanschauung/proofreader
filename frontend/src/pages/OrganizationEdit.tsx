import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import toast from 'react-hot-toast';
import { eventsApi } from '../services/api';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import type { EventOrganizationDetail } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { eventEditPath, organizationPath } from '../utils/paths';
import './OrganizationEdit.css';

const EMPTY_EVENT = { slug: '', title: '', dates: '', place: '' };

/** Правка организации: описание, виды и события (порядок, удаление, новое). */
export const OrganizationEdit: React.FC = () => {
  const { slug = '' } = useParams<{ slug: string }>();
  const [org, setOrg] = useState<EventOrganizationDetail | null>(null);
  const [head, setHead] = useState({ title: '', description: '' });
  const [newKind, setNewKind] = useState('');
  const [newEvent, setNewEvent] = useState<Record<number, typeof EMPTY_EVENT>>({});
  const [loadError, setLoadError] = useState('');
  const [error, setError] = useState('');
  const [loading, setLoading] = useState(true);

  useDocumentTitle(org ? `Правка — ${org.title}` : 'Правка организации');

  // Слаг организации не меняется, поэтому после записи читаем её заново.
  const reload = async () => {
    const { data } = await eventsApi.organization(slug);
    setOrg(data);
    setHead({ title: data.title, description: data.description });
  };

  useEffect(() => {
    const load = async () => {
      try {
        await reload();
      } catch (err: unknown) {
        setLoadError(apiErrorMessage(err, 'Не удалось загрузить организацию'));
      } finally {
        setLoading(false);
      }
    };
    void load();
    // eslint-disable-next-line react-hooks/exhaustive-deps
  }, [slug]);

  if (loading) return <div className="loading-state">Загружаем организацию…</div>;
  if (loadError || !org) return <div className="error-message">{loadError || 'Не найдено'}</div>;

  // Любая запись: ошибку сервера — на экран, успех — перечитать.
  const run = async (what: string, f: () => Promise<unknown>, ok?: string) => {
    setError('');
    try {
      await f();
      await reload();
      if (ok) toast.success(ok);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, what));
    }
  };

  const moveKind = (i: number, d: number) => {
    const a = org.kinds[i];
    const b = org.kinds[i + d];
    if (!a || !b) return;
    void run('Не удалось переставить вид', async () => {
      await eventsApi.updateKind(a.id, { title: a.title, position: b.position });
      await eventsApi.updateKind(b.id, { title: b.title, position: a.position });
    });
  };

  const moveEvent = (kindId: number, i: number, d: number) => {
    const list = org.kinds.find((k) => k.id === kindId)?.events ?? [];
    const j = i + d;
    if (!list[i] || !list[j]) return;
    void run('Не удалось переставить событие', async () => {
      // Порядок задаётся позицией; новую расставляем подряд по списку.
      const order = list.map((e) => e.id);
      [order[i], order[j]] = [order[j], order[i]];
      for (let p = 0; p < order.length; p++) {
        const { data } = await eventsApi.event(org.slug, list.find((e) => e.id === order[p])!.slug);
        await eventsApi.updateEvent(data.id, {
          kind_id: data.kind_id,
          title: data.title,
          dates: data.dates,
          place: data.place,
          description: data.description,
          position: p,
        });
      }
    });
  };

  const removeEvent = (id: number, title: string) => {
    if (!window.confirm(`Удалить событие «${title}» вместе с повесткой?`)) return;
    void run('Не удалось удалить событие', () => eventsApi.deleteEvent(id));
  };

  const removeKind = (id: number, title: string) => {
    if (!window.confirm(`Удалить вид «${title}»?`)) return;
    void run('Не удалось удалить вид', () => eventsApi.deleteKind(id));
  };

  const draftOf = (kindId: number) => newEvent[kindId] ?? EMPTY_EVENT;
  const setDraft = (kindId: number, patch: Partial<typeof EMPTY_EVENT>) =>
    setNewEvent((m) => ({ ...m, [kindId]: { ...draftOf(kindId), ...patch } }));

  return (
    <div className="org-edit">
      <p>
        <Link to={organizationPath(org)}>← К организации</Link>
      </p>
      <h1>Правка организации</h1>
      <div className="org-edit-head">
        <label>
          Заглавие
          <input value={head.title} onChange={(e) => setHead({ ...head, title: e.target.value })} />
        </label>
        <label>
          Описание
          <textarea
            rows={4}
            value={head.description}
            onChange={(e) => setHead({ ...head, description: e.target.value })}
          />
        </label>
        <button
          type="button"
          onClick={() =>
            void run(
              'Не удалось сохранить организацию',
              () => eventsApi.updateOrganization(org.id, { ...head, position: org.position }),
              'Организация сохранена',
            )
          }
        >
          Сохранить организацию
        </button>
      </div>
      {error && (
        <div className="org-edit-error" role="alert">
          {error}
        </div>
      )}
      <h2>Виды</h2>
      {org.kinds.map((k, ki) => (
        <section key={k.id} className="org-edit-kind">
          <div className="org-edit-row">
            <strong>{k.title}</strong>
            <button
              type="button"
              aria-label={`Вид ${k.title} вверх`}
              onClick={() => moveKind(ki, -1)}
            >
              Вверх
            </button>
            <button
              type="button"
              aria-label={`Вид ${k.title} вниз`}
              onClick={() => moveKind(ki, 1)}
            >
              Вниз
            </button>
            <button
              type="button"
              aria-label={`Удалить вид ${k.title}`}
              onClick={() => removeKind(k.id, k.title)}
            >
              Удалить
            </button>
          </div>
          <ul>
            {k.events.map((e, ei) => (
              <li key={e.id} className="org-edit-row">
                <span>
                  {e.title} {e.dates && `(${e.dates})`}
                </span>
                <button
                  type="button"
                  aria-label={`Событие ${e.title} вверх`}
                  onClick={() => moveEvent(k.id, ei, -1)}
                >
                  Вверх
                </button>
                <button
                  type="button"
                  aria-label={`Событие ${e.title} вниз`}
                  onClick={() => moveEvent(k.id, ei, 1)}
                >
                  Вниз
                </button>
                <button
                  type="button"
                  aria-label={`Удалить событие ${e.title}`}
                  onClick={() => removeEvent(e.id, e.title)}
                >
                  Удалить
                </button>
                <Link to={eventEditPath(org.slug, e.slug)}>Повестка</Link>
              </li>
            ))}
          </ul>
          <fieldset className="org-edit-new">
            <legend>Новое событие в виде «{k.title}»</legend>
            {(['slug', 'title', 'dates', 'place'] as const).map((f) => (
              <input
                key={f}
                aria-label={`${{ slug: 'Слаг', title: 'Заглавие', dates: 'Даты', place: 'Место' }[f]} (${k.title})`}
                placeholder={{ slug: 'Слаг', title: 'Заглавие', dates: 'Даты', place: 'Место' }[f]}
                value={draftOf(k.id)[f]}
                onChange={(e) => setDraft(k.id, { [f]: e.target.value })}
              />
            ))}
            <button
              type="button"
              aria-label={`Создать событие (${k.title})`}
              onClick={() =>
                void run(
                  'Не удалось создать событие',
                  async () => {
                    await eventsApi.createEvent(k.id, {
                      ...draftOf(k.id),
                      description: '',
                      position: k.events.length,
                    });
                    setNewEvent((m) => ({ ...m, [k.id]: EMPTY_EVENT }));
                  },
                  'Событие создано',
                )
              }
            >
              Создать
            </button>
          </fieldset>
        </section>
      ))}
      <div className="org-edit-row">
        <input
          aria-label="Новый вид"
          placeholder="Заглавие нового вида"
          value={newKind}
          onChange={(e) => setNewKind(e.target.value)}
        />
        <button
          type="button"
          onClick={() =>
            void run('Не удалось добавить вид', async () => {
              await eventsApi.createKind(org.id, { title: newKind, position: org.kinds.length });
              setNewKind('');
            })
          }
        >
          Добавить вид
        </button>
      </div>
    </div>
  );
};
