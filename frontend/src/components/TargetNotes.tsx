import { useState } from 'react';
import { useAuth } from '../hooks/useAuth';
import { marginNotesApi } from '../services/api';
import type { MarginNote } from '../types';
import { MarginNoteEditor } from './MarginNoteEditor';
import './MarginNotes.css';
import './TargetNotes.css';

export interface TargetNotesProps {
  workId: number;
  /** Есть — заметки к этой главе; нет — к тому целиком. */
  chapterId?: number;
  /** Все свои заметки тома: блок отбирает нужные сам. */
  notes: MarginNote[];
  onCreated(n: MarginNote): void;
  onChange(n: MarginNote): void;
  onDelete(id: number): void;
}

/**
 * Заметки читателя к главе или ко всему тому: свёрнутый блок «Мои заметки
 * к главе (N)» со списком и кнопкой новой заметки. Анониму не рисуется;
 * без заметок — одна кнопка, без заголовка. Текст заметки — простой текст
 * (pre-wrap), не разметка.
 */
const TargetNotesInner: React.FC<TargetNotesProps> = ({
  workId,
  chapterId,
  notes,
  onCreated,
  onChange,
  onDelete,
}) => {
  const { user } = useAuth();
  const [open, setOpen] = useState(false);
  const [adding, setAdding] = useState(false);
  const [editingId, setEditingId] = useState<number | null>(null);

  if (!user) return null;

  const ofChapter = chapterId !== undefined;
  const mine = notes.filter((n) =>
    ofChapter ? n.target === 'chapter' && n.chapter_id === chapterId : n.target === 'work',
  );
  const noun = ofChapter ? 'главе' : 'тому';
  const addLabel = ofChapter ? 'Заметка к главе' : 'Заметка к тому';

  const addEditor = (
    <MarginNoteEditor
      save={(body) =>
        marginNotesApi
          .create(
            workId,
            chapterId !== undefined
              ? { target: 'chapter', chapter_id: chapterId, body }
              : { target: 'work', body },
          )
          .then((r) => r.data)
      }
      onSaved={(n) => {
        setAdding(false);
        setOpen(true);
        onCreated(n);
      }}
      onCancel={() => setAdding(false)}
    />
  );

  const addButton = !adding && (
    <button type="button" className="btn btn-secondary btn-sm" onClick={() => setAdding(true)}>
      {addLabel}
    </button>
  );

  if (mine.length === 0) {
    return (
      <div className="target-notes">
        {addButton}
        {adding && addEditor}
      </div>
    );
  }

  return (
    <div className="target-notes">
      <button
        type="button"
        className="target-notes-toggle"
        aria-expanded={open}
        onClick={() => setOpen((v) => !v)}
      >
        Мои заметки к {noun} ({mine.length})
      </button>
      {open && (
        <div className="target-notes-body">
          <ul className="target-notes-list">
            {mine.map((n) => (
              <li key={n.id} className="target-notes-item">
                {editingId === n.id ? (
                  <MarginNoteEditor
                    initial={n.body}
                    save={(body) => marginNotesApi.update(n.id, body).then((r) => r.data)}
                    onSaved={(saved) => {
                      setEditingId(null);
                      onChange(saved);
                    }}
                    onCancel={() => setEditingId(null)}
                    remove={() => marginNotesApi.remove(n.id)}
                    onRemoved={() => {
                      setEditingId(null);
                      onDelete(n.id);
                    }}
                  />
                ) : (
                  <>
                    <p className="target-notes-text">{n.body}</p>
                    <button
                      type="button"
                      className="btn btn-ghost btn-sm"
                      onClick={() => setEditingId(n.id)}
                    >
                      Изменить
                    </button>
                  </>
                )}
              </li>
            ))}
          </ul>
          {addButton}
          {adding && addEditor}
        </div>
      )}
    </div>
  );
};

/**
 * Состояние блока (раскрыт ли, открытый редактор, недописанный текст) живёт
 * в TargetNotesInner и сбрасывается ключом «том, глава»: ChapterView и
 * WorkDetail не размонтируются при переходе к соседней главе или тому, и
 * черновик иначе уехал бы в чужую главу.
 */
export const TargetNotes: React.FC<TargetNotesProps> = (props) => (
  <TargetNotesInner key={`${props.workId}:${props.chapterId ?? 'work'}`} {...props} />
);
