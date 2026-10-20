import { useEffect, useState } from 'react';
import { chaptersApi, eventsApi, worksApi } from '../services/api';
import type { AgendaInput, EventSection } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { pickableChapters } from '../utils/pickableChapters';
import './AgendaEditor.css';

interface TargetDraft {
  key: number;
  workId: string;
  /** '' — глава не выбрана, 'work' — весь том, иначе id главы. */
  pick: string;
  label: string;
  /** Битая цель, которую ещё не выбрали заново. */
  broken: boolean;
  snapshot: string;
}
interface EntryDraft {
  key: number;
  label: string;
  targets: TargetDraft[];
}
interface SectionDraft {
  key: number;
  title: string;
  entries: EntryDraft[];
}

let nextKey = 1;
const uid = () => nextKey++;

function fromServer(sections: EventSection[]): SectionDraft[] {
  return sections.map((s) => ({
    key: uid(),
    title: s.title,
    entries: s.entries.map((e) => ({
      key: uid(),
      label: e.label,
      targets: e.targets.map((t) => ({
        key: uid(),
        workId: t.work_id == null ? '' : String(t.work_id),
        pick: t.kind === 'work' ? 'work' : t.chapter_id == null ? '' : String(t.chapter_id),
        label: t.label,
        broken: t.broken,
        snapshot: t.title,
      })),
    })),
  }));
}

function move<T>(list: T[], i: number, d: number): T[] {
  const j = i + d;
  if (j < 0 || j >= list.length) return list;
  const out = [...list];
  [out[i], out[j]] = [out[j], out[i]];
  return out;
}

const workIdOf = (t: TargetDraft) => (/^[1-9]\d*$/.test(t.workId.trim()) ? Number(t.workId) : 0);

/** Цель негодна к сохранению: битая и не выбранная заново, без тома или главы. */
function targetBlocked(t: TargetDraft): boolean {
  return t.broken || workIdOf(t) === 0 || t.pick === '';
}

function toInput(sections: SectionDraft[]): AgendaInput {
  return {
    sections: sections.map((s) => ({
      title: s.title,
      entries: s.entries.map((e) => ({
        label: e.label,
        targets: e.targets.map((t) =>
          t.pick === 'work'
            ? { kind: 'work' as const, work_id: workIdOf(t), label: t.label }
            : {
                kind: 'chapter' as const,
                work_id: workIdOf(t),
                chapter_id: Number(t.pick),
                label: t.label,
              },
        ),
      })),
    })),
  };
}

const TargetRow: React.FC<{
  at: string;
  t: TargetDraft;
  onChange: (t: TargetDraft) => void;
  onRemove: () => void;
}> = ({ at, t, onChange, onRemove }) => {
  const [workTitle, setWorkTitle] = useState('');
  const [chapters, setChapters] = useState<{ id: number; title: string; depth: number }[]>([]);
  const [loadError, setLoadError] = useState('');
  const id = workIdOf(t);

  useEffect(() => {
    if (id === 0) return;
    let ignore = false;
    const load = async () => {
      try {
        const [w, c] = await Promise.all([worksApi.get(id), chaptersApi.list(id)]);
        if (ignore) return;
        setLoadError('');
        setWorkTitle(w.data.title);
        setChapters(
          pickableChapters(c.data ?? []).map((x) => ({
            id: x.chapter.id,
            title: x.chapter.title,
            depth: x.depth,
          })),
        );
      } catch (err: unknown) {
        if (ignore) return;
        setWorkTitle('');
        setChapters([]);
        setLoadError(apiErrorMessage(err, 'Не удалось загрузить том'));
      }
    };
    void load();
    return () => {
      ignore = true;
    };
  }, [id]);

  return (
    <li className="agenda-target">
      <label>
        id тома
        <input
          aria-label={`id тома ${at}`}
          value={t.workId}
          inputMode="numeric"
          onChange={(e) => onChange({ ...t, workId: e.target.value, pick: '', broken: false })}
        />
      </label>
      {id !== 0 && (workTitle || loadError) && (
        <span className={loadError ? 'agenda-error' : 'agenda-work-title'}>
          {loadError || workTitle}
        </span>
      )}
      <select
        aria-label={`Глава ${at}`}
        value={t.pick}
        disabled={id === 0}
        onChange={(e) => onChange({ ...t, pick: e.target.value, broken: false })}
      >
        <option value="" disabled>
          — выберите главу —
        </option>
        <option value="work">весь том</option>
        {chapters.map((c) => (
          <option key={c.id} value={String(c.id)}>
            {'— '.repeat(c.depth)}
            {c.title}
          </option>
        ))}
      </select>
      <input
        aria-label={`Подпись цели ${at}`}
        value={t.label}
        placeholder="Подпись цели"
        onChange={(e) => onChange({ ...t, label: e.target.value })}
      />
      <button type="button" onClick={onRemove} aria-label={`Удалить цель ${at}`}>
        Удалить
      </button>
      {t.broken && (
        <p className="agenda-error">
          {t.pick === 'work' ? 'том не найден' : 'глава не найдена'}: {t.snapshot} — выберите главу
          заново
        </p>
      )}
    </li>
  );
};

/**
 * Редактор повестки: пункты → строки → цели. Сохраняется целиком
 * (`PUT /events/{id}/agenda`); текст 400 сервера показывается как есть.
 */
export const AgendaEditor: React.FC<{ eventId: number; initial: EventSection[] }> = ({
  eventId,
  initial,
}) => {
  const [sections, setSections] = useState<SectionDraft[]>(() => fromServer(initial));
  const [error, setError] = useState('');
  const [saving, setSaving] = useState(false);
  const [done, setDone] = useState(false);

  const blocked = sections.some((s) =>
    s.entries.some((e) => e.targets.some((t) => targetBlocked(t))),
  );
  const hasBroken = sections.some((s) => s.entries.some((e) => e.targets.some((t) => t.broken)));

  const edit = (next: SectionDraft[]) => {
    setSections(next);
    setDone(false);
  };
  const setSection = (si: number, f: (s: SectionDraft) => SectionDraft) =>
    edit(sections.map((s, i) => (i === si ? f(s) : s)));
  const setEntry = (si: number, ei: number, f: (e: EntryDraft) => EntryDraft) =>
    setSection(si, (s) => ({ ...s, entries: s.entries.map((e, i) => (i === ei ? f(e) : e)) }));

  const save = async () => {
    setSaving(true);
    setError('');
    try {
      await eventsApi.replaceAgenda(eventId, toInput(sections));
      setDone(true);
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить повестку'));
    } finally {
      setSaving(false);
    }
  };

  return (
    <section className="agenda-editor">
      <h2>Повестка</h2>
      {sections.map((s, si) => {
        const sn = si + 1;
        return (
          <div className="agenda-section" key={s.key}>
            <div className="agenda-row">
              <input
                aria-label={`Заглавие пункта ${sn}`}
                value={s.title}
                placeholder="Заглавие пункта"
                onChange={(e) => setSection(si, (x) => ({ ...x, title: e.target.value }))}
              />
              <button
                type="button"
                aria-label={`Пункт ${sn} вверх`}
                onClick={() => edit(move(sections, si, -1))}
              >
                Вверх
              </button>
              <button
                type="button"
                aria-label={`Пункт ${sn} вниз`}
                onClick={() => edit(move(sections, si, 1))}
              >
                Вниз
              </button>
              <button
                type="button"
                aria-label={`Удалить пункт ${sn}`}
                onClick={() => edit(sections.filter((_, i) => i !== si))}
              >
                Удалить
              </button>
            </div>
            <ul className="agenda-entries">
              {s.entries.map((e, ei) => {
                const en = `${sn}.${ei + 1}`;
                return (
                  <li key={e.key} className="agenda-entry">
                    <div className="agenda-row">
                      <input
                        aria-label={`Подпись строки ${en}`}
                        value={e.label}
                        placeholder="Подпись строки"
                        onChange={(ev) =>
                          setEntry(si, ei, (x) => ({ ...x, label: ev.target.value }))
                        }
                      />
                      <button
                        type="button"
                        aria-label={`Строка ${en} вверх`}
                        onClick={() =>
                          setSection(si, (x) => ({ ...x, entries: move(x.entries, ei, -1) }))
                        }
                      >
                        Вверх
                      </button>
                      <button
                        type="button"
                        aria-label={`Строка ${en} вниз`}
                        onClick={() =>
                          setSection(si, (x) => ({ ...x, entries: move(x.entries, ei, 1) }))
                        }
                      >
                        Вниз
                      </button>
                      <button
                        type="button"
                        aria-label={`Удалить строку ${en}`}
                        onClick={() =>
                          setSection(si, (x) => ({
                            ...x,
                            entries: x.entries.filter((_, i) => i !== ei),
                          }))
                        }
                      >
                        Удалить
                      </button>
                    </div>
                    <ul className="agenda-targets">
                      {e.targets.map((t, ti) => (
                        <TargetRow
                          key={t.key}
                          at={`${en}.${ti + 1}`}
                          t={t}
                          onChange={(nt) =>
                            setEntry(si, ei, (x) => ({
                              ...x,
                              targets: x.targets.map((y, i) => (i === ti ? nt : y)),
                            }))
                          }
                          onRemove={() =>
                            setEntry(si, ei, (x) => ({
                              ...x,
                              targets: x.targets.filter((_, i) => i !== ti),
                            }))
                          }
                        />
                      ))}
                    </ul>
                    <button
                      type="button"
                      aria-label={`Добавить цель в строку ${en}`}
                      onClick={() =>
                        setEntry(si, ei, (x) => ({
                          ...x,
                          targets: [
                            ...x.targets,
                            {
                              key: uid(),
                              workId: '',
                              pick: '',
                              label: '',
                              broken: false,
                              snapshot: '',
                            },
                          ],
                        }))
                      }
                    >
                      Добавить цель
                    </button>
                  </li>
                );
              })}
            </ul>
            <button
              type="button"
              aria-label={`Добавить строку в пункт ${sn}`}
              onClick={() =>
                setSection(si, (x) => ({
                  ...x,
                  entries: [...x.entries, { key: uid(), label: '', targets: [] }],
                }))
              }
            >
              Добавить строку
            </button>
          </div>
        );
      })}
      <button
        type="button"
        onClick={() => edit([...sections, { key: uid(), title: '', entries: [] }])}
      >
        Добавить пункт
      </button>
      <div className="agenda-save">
        <button type="button" onClick={() => void save()} disabled={saving || blocked}>
          Сохранить повестку
        </button>
        {hasBroken && <span className="agenda-error">выберите главу заново</span>}
        {done && <span role="status">Повестка сохранена</span>}
      </div>
      {error && (
        <div className="agenda-error" role="alert">
          {error}
        </div>
      )}
    </section>
  );
};
