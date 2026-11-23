import { useRef, useState } from 'react';
import type { Collection, CollectionEntry, CollectionProgramInput } from '../types';
import { sourceLabel } from '../utils/sourceLabel';

type MeetingDraft = {
  key: number;
  title: string;
  question: string;
  entries: CollectionEntry[];
};

function move<T>(list: T[], index: number, offset: -1 | 1): T[] {
  const next = index + offset;
  if (next < 0 || next >= list.length) return list;
  const result = [...list];
  [result[index], result[next]] = [result[next], result[index]];
  return result;
}

function OrderButtons({
  title,
  index,
  count,
  onMove,
}: {
  title: string;
  index: number;
  count: number;
  onMove: (offset: -1 | 1) => void;
}) {
  return (
    <div className="program-order">
      <button
        type="button"
        className="arrow-button"
        aria-label={`Переместить «${title}» вверх`}
        disabled={index === 0}
        onClick={() => onMove(-1)}
      >
        ↑
      </button>
      <button
        type="button"
        className="arrow-button"
        aria-label={`Переместить «${title}» вниз`}
        disabled={index === count - 1}
        onClick={() => onMove(1)}
      >
        ↓
      </button>
    </div>
  );
}

export function CollectionProgramForm({
  collection,
  onSave,
  onCancel,
}: {
  collection: Collection;
  onSave: (input: CollectionProgramInput) => Promise<void>;
  onCancel: () => void;
}) {
  const [meetings, setMeetings] = useState<MeetingDraft[]>(() =>
    (collection.meetings ?? []).map((meeting) => ({
      key: meeting.position,
      title: meeting.title,
      question: meeting.question,
      entries: (collection.items ?? []).filter(
        (entry) => entry.meeting_position === meeting.position,
      ),
    })),
  );
  const [unassigned, setUnassigned] = useState(() =>
    (collection.items ?? []).filter((entry) => entry.meeting_position === undefined),
  );
  const nextKey = useRef(meetings.length + 1);
  const [saving, setSaving] = useState(false);
  const [revision] = useState(collection.program_revision ?? 0);

  function updateMeeting(key: number, change: Partial<Omit<MeetingDraft, 'key'>>) {
    setMeetings((current) =>
      current.map((meeting) => (meeting.key === key ? { ...meeting, ...change } : meeting)),
    );
  }

  const handleSubmit = async (event: React.FormEvent) => {
    event.preventDefault();
    setSaving(true);
    try {
      await onSave({
        revision,
        meetings: meetings.map(({ title, question, entries }) => ({
          title,
          question,
          item_ids: entries.map(({ id }) => id),
        })),
        unassigned_item_ids: unassigned.map(({ id }) => id),
      });
    } finally {
      setSaving(false);
    }
  };

  function renderEntries(entries: CollectionEntry[], meetingKey = 0) {
    if (entries.length === 0) return null;

    const updateEntries = (key: number, nextEntries: CollectionEntry[]) => {
      if (key === 0) setUnassigned(nextEntries);
      else updateMeeting(key, { entries: nextEntries });
    };

    return (
      <ol className="composition-list program-entries">
        {entries.map((entry, index) => {
          const title = entry.unavailable ? 'Источник недоступен' : entry.title;
          return (
            <li className="composition-row" key={entry.id}>
              <OrderButtons
                title={title}
                index={index}
                count={entries.length}
                onMove={(offset) => updateEntries(meetingKey, move(entries, index, offset))}
              />
              <div className="composition-body">
                <div className="composition-head">
                  <span className="composition-title">{title}</span>
                  {entry.source && <span className="composition-source">{sourceLabel(entry)}</span>}
                </div>
                {meetings.length > 0 && (
                  <div className="form-group program-assignment">
                    <label htmlFor={`program-item-${entry.id}`}>Занятие для «{title}»</label>
                    <select
                      id={`program-item-${entry.id}`}
                      value={meetingKey}
                      onChange={(event) => {
                        const key = Number(event.target.value);
                        const destination = meetings.find((meeting) => meeting.key === key);
                        updateEntries(
                          meetingKey,
                          entries.filter((item) => item.id !== entry.id),
                        );
                        updateEntries(key, [...(destination?.entries ?? unassigned), entry]);
                      }}
                    >
                      <option value={0}>Вне занятий</option>
                      {meetings.map((meeting, i) => (
                        <option key={meeting.key} value={meeting.key}>
                          {i + 1}. {meeting.title.trim() || 'Без названия'}
                        </option>
                      ))}
                    </select>
                  </div>
                )}
              </div>
            </li>
          );
        })}
      </ol>
    );
  }

  return (
    <form onSubmit={(event) => void handleSubmit(event)} className="collection-program-form">
      <fieldset disabled={saving}>
        {meetings.map((meeting, index) => (
          <section key={meeting.key} className="program-meeting">
            <div className="program-meeting-actions">
              <h3>Занятие {index + 1}</h3>
              <OrderButtons
                title={meeting.title.trim() || `Занятие ${index + 1}`}
                index={index}
                count={meetings.length}
                onMove={(offset) => setMeetings((current) => move(current, index, offset))}
              />
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => {
                  setUnassigned((current) => [...current, ...meeting.entries]);
                  setMeetings((current) => current.filter(({ key }) => key !== meeting.key));
                }}
              >
                Удалить занятие
              </button>
            </div>
            <div className="form-group">
              <label htmlFor={`program-title-${meeting.key}`}>Название занятия</label>
              <input
                id={`program-title-${meeting.key}`}
                value={meeting.title}
                required
                onChange={(event) => updateMeeting(meeting.key, { title: event.target.value })}
              />
            </div>
            <div className="form-group">
              <label htmlFor={`program-question-${meeting.key}`}>Вопрос для обсуждения</label>
              <textarea
                id={`program-question-${meeting.key}`}
                rows={2}
                value={meeting.question}
                onChange={(event) => updateMeeting(meeting.key, { question: event.target.value })}
              />
            </div>
            {renderEntries(meeting.entries, meeting.key)}
          </section>
        ))}
        <button
          type="button"
          className="btn btn-secondary"
          onClick={() => {
            const key = nextKey.current++;
            setMeetings((current) => [...current, { key, title: '', question: '', entries: [] }]);
          }}
        >
          Добавить занятие
        </button>
        {unassigned.length > 0 && (
          <section className="program-unassigned">
            <h3>Тексты вне занятий</h3>
            {renderEntries(unassigned)}
          </section>
        )}
        <div className="form-actions">
          <button type="button" className="cancel-button" onClick={onCancel}>
            Отмена
          </button>
          <button type="submit" className="submit-button">
            {saving ? 'Сохранение…' : 'Сохранить программу'}
          </button>
        </div>
      </fieldset>
    </form>
  );
}
