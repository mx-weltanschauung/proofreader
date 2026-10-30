import { useState } from 'react';
import { readerCommentsApi } from '../services/api';
import { useAuth, canEdit } from '../hooks/useAuth';
import { Link, useLocation } from 'react-router-dom';
import type { MarginNote } from '../types';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { joinPathFor } from '../utils/returnUrl';
import './MarginNotes.css';

/** Потолок текста заметки — тот же, что держит сервер (10 000 знаков). */
const BODY_LIMIT = 10_000;

export interface MarginNoteEditorProps {
  /** Начальный текст поля: текст заметки при правке, пусто при создании. */
  initial?: string;
  /** Запись: создание или правка. Ответ — заметка в том виде, как её сохранил сервер. */
  save(body: string): Promise<MarginNote>;
  onSaved(n: MarginNote): void;
  onCancel(): void;
  /** Удаление — только у уже записанной заметки; без него кнопки «Удалить» нет. */
  remove?(): Promise<unknown>;
  onRemoved?(): void;
  /** Ставить ли курсор в поле сразу (по умолчанию да: редактор открывают ради записи). */
  autoFocus?: boolean;
  kind?: 'note' | 'comment';
  noteId?: number;
}

type Failure = { kind: 'auth' } | { kind: 'error'; message: string } | null;

/**
 * Поле заметки с «Сохранить», «Отмена» и — у записанной — «Удалить».
 *
 * Запись делает вызывающий (`save`): создание к цитате, к главе и к тому и
 * правка различаются только запросом, а поле, кнопки и разбор отказов у них
 * одни. Отказ 401 — сессия истекла или читатель вышел в другой вкладке —
 * текст в поле НЕ трогает: написанное читателем дороже формы, и войти он
 * может в новой вкладке, а потом нажать «Сохранить» ещё раз.
 */
export const MarginNoteEditor: React.FC<MarginNoteEditorProps> = ({
  initial = '',
  save,
  onSaved,
  onCancel,
  remove,
  onRemoved,
  autoFocus = true,
  kind = 'note',
  noteId,
}) => {
  const location = useLocation();
  const staff = useAuth((s) => canEdit(s.user));
  const [text, setText] = useState(initial);
  const [busy, setBusy] = useState(false);
  const [failure, setFailure] = useState<Failure>(null);

  const fail = (err: unknown, fallback: string) => {
    if (apiErrorStatus(err) === 401) setFailure({ kind: 'auth' });
    else setFailure({ kind: 'error', message: apiErrorMessage(err, fallback) });
  };

  const onSave = async () => {
    const body = text.trim();
    if (body === '' || busy) return;
    setBusy(true);
    setFailure(null);
    try {
      const saved = await save(body);
      setBusy(false);
      onSaved(saved);
    } catch (err) {
      setBusy(false);
      fail(err, 'Не удалось сохранить заметку');
    }
  };

  const onRemove = async () => {
    if (!remove || busy) return;
    if (!window.confirm('Удалить заметку?')) return;
    setBusy(true);
    setFailure(null);
    try {
      await remove();
      setBusy(false);
      onRemoved?.();
    } catch (err) {
      setBusy(false);
      fail(err, 'Не удалось удалить заметку');
    }
  };

  // Escape закрывает поле, как «Отмена», и дальше не всплывает: на экране
  // потока Escape на window уводит из чтения (WorkRead), и написанное
  // пропало бы вместе с экраном. Начатый текст без спроса не выбрасывается.
  const onKeyDown = (e: React.KeyboardEvent) => {
    if (e.key !== 'Escape') return;
    e.preventDefault();
    e.stopPropagation();
    if (busy) return;
    if (text.trim() !== initial.trim() && !window.confirm('Закрыть заметку без сохранения?')) {
      return;
    }
    onCancel();
  };

  return (
    <div className="margin-note-editor" onClick={(e) => e.stopPropagation()} onKeyDown={onKeyDown}>
      <textarea
        className="margin-note-field"
        aria-label={kind === 'comment' ? 'Текст комментария' : 'Текст заметки'}
        value={text}
        maxLength={kind === 'comment' ? 2000 : BODY_LIMIT}
        rows={4}
        // Редактор открывают ради записи: курсор сразу в поле, без второго нажатия.
        autoFocus={autoFocus}
        onChange={(e) => setText(e.target.value)}
        onKeyDown={(e) => {
          if (e.key === 'Enter' && (e.ctrlKey || e.metaKey)) {
            e.preventDefault();
            void onSave();
          }
        }}
      />
      {failure?.kind === 'auth' && (
        <div className="margin-note-failure" role="alert">
          <Link to={joinPathFor(location.pathname, location.search)}>
            {kind === 'comment'
              ? 'Войдите, чтобы отправить комментарий'
              : 'Войдите, чтобы сохранить заметку'}
          </Link>
        </div>
      )}
      {failure?.kind === 'error' && (
        <div className="margin-note-failure" role="alert">
          {failure.message}
        </div>
      )}
      <div className="margin-note-actions">
        <button
          type="button"
          className="btn btn-primary btn-sm"
          disabled={busy || text.trim() === ''}
          onClick={() => void onSave()}
        >
          {kind === 'comment' ? (staff ? 'Опубликовать' : 'Отправить на проверку') : 'Сохранить'}
        </button>
        <button type="button" className="btn btn-secondary btn-sm" onClick={onCancel}>
          Отмена
        </button>
        {remove && (
          <button
            type="button"
            className="btn btn-ghost btn-sm margin-note-remove"
            disabled={busy}
            onClick={() => void onRemove()}
          >
            Удалить
          </button>
        )}
        {noteId !== undefined && (
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={busy || text.trim() === '' || Array.from(text.trim()).length > 2000}
            onClick={() => {
              setBusy(true);
              setFailure(null);
              void readerCommentsApi
                .create(0, { note_id: noteId, body: text.trim() })
                .then(() => {
                  setBusy(false);
                  window.dispatchEvent(new Event('reader-comments-changed'));
                  onRemoved?.();
                })
                .catch((err: unknown) => {
                  setBusy(false);
                  fail(err, 'Не удалось отправить комментарий');
                });
            }}
          >
            Сделать комментарием
          </button>
        )}
      </div>
    </div>
  );
};
