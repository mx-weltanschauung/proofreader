import { useState } from 'react';
import { isAxiosError } from 'axios';
import toast from 'react-hot-toast';
import { usePlayer } from '../audio/playerStore';
import { audioApi } from '../services/api';
import { probeDuration, putWithProgress } from '../services/audioUpload';
import type { AudioRecording } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { recordingContentType } from '../utils/audio';
import './RecordingManager.css';

interface Deps {
  put: typeof putWithProgress;
  probe: typeof probeDuration;
}

interface Props {
  workId: number;
  chapterId: number;
  /** Записи ИМЕННО этой главы (без подглав), по position. */
  recordings: AudioRecording[];
  onChanged: () => Promise<void>;
  /** Подмена заливки и замера длительности в тестах. */
  deps?: Deps;
}

const DEFAULT_DEPS: Deps = { put: putWithProgress, probe: probeDuration };

/** Потолок сервера (maxRecordingBytes) — отказ до заливки, а не после. */
const MAX_BYTES = 2 * 1024 * 1024 * 1024;

/** Строка статуса пачки. Ключ — номер файла в пачке, а не имя: два «1.mp3» из
 *  разных папок иначе делили бы строку, и второй стирал бы итог первого. */
interface StatusLine {
  n: number;
  text: string;
}

export const RecordingManager: React.FC<Props> = ({
  workId,
  chapterId,
  recordings,
  onChanged,
  deps = DEFAULT_DEPS,
}) => {
  const [reader, setReader] = useState('');
  const [status, setStatus] = useState<StatusLine[]>([]);
  const [busy, setBusy] = useState(false);
  /** Запись, у которой открыто поле «кто читает», и набранное в нём. */
  const [editing, setEditing] = useState<{ id: number; draft: string } | null>(null);

  // Строка файла n — заменить, если есть, иначе дописать в конец.
  const say = (n: number, text: string) =>
    setStatus((prev) =>
      prev.some((l) => l.n === n)
        ? prev.map((l) => (l.n === n ? { n, text } : l))
        : [...prev, { n, text }],
    );

  // Файлы — по одному: параллельная заливка делила бы канал и перемешала бы
  // порядок позиций (позиция — в конец главы на момент регистрации).
  const attach = async (files: File[]) => {
    setBusy(true);
    setStatus([]);
    for (const [n, file] of files.entries()) {
      const type = recordingContentType(file.name);
      if (!type) {
        say(n, `${file.name}: формат не поддерживается — годятся mp3, m4a, ogg, opus, flac`);
        continue;
      }
      if (file.size <= 0 || file.size > MAX_BYTES) {
        say(n, `${file.name}: размер файла — от 1 байта до 2 ГБ`);
        continue;
      }
      try {
        const seconds = await deps.probe(file);
        const { data: up } = await audioApi.recordingUploadURL(workId, chapterId, {
          content_type: type,
          bytes: file.size,
        });
        await deps.put(up.url, file, up.content_type, (loaded, total) =>
          say(n, `${file.name}: ${Math.round((loaded / total) * 100)} %`),
        );
        await audioApi.registerRecording(workId, chapterId, {
          key: up.key,
          content_type: type,
          bytes: file.size,
          duration_ms: Math.round(seconds * 1000),
          reader: reader.trim(),
        });
        say(n, `${file.name}: прикреплена`);
      } catch (err: unknown) {
        // Свои отказы (замер, заливка в хранилище) уже по-русски. У axios
        // response есть только при ответе сервера: без него это обрыв связи
        // или таймаут, и err.message — «Network Error» по-английски.
        const text = !isAxiosError(err)
          ? err instanceof Error
            ? err.message
            : 'не прикрепилась'
          : err.response
            ? apiErrorMessage(err, 'не прикрепилась')
            : 'нет связи с сервером — запись не прикрепилась';
        say(n, `${file.name}: ${text}`);
      }
    }
    setBusy(false);
    await onChanged();
  };

  /** true — действие прошло: поле правки чтеца закрывается только по успеху. */
  const run = async (action: () => Promise<unknown>, fallback: string): Promise<boolean> => {
    try {
      await action();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, fallback));
      return false;
    }
    await onChanged();
    return true;
  };

  const saveReader = async (r: AudioRecording, value: string) => {
    const next = value.trim();
    if (
      next === r.reader ||
      (await run(
        () => audioApi.updateRecording(r.id, { reader: next }),
        'Не удалось сохранить, кто читает',
      ))
    ) {
      setEditing(null);
    }
  };

  const remove = (r: AudioRecording) => {
    const who = r.reader ? ` (читает ${r.reader})` : '';
    if (
      window.confirm(`Удалить запись ${r.position}${who}? Файл удалится из хранилища безвозвратно.`)
    ) {
      void run(async () => {
        await audioApi.deleteRecording(r.id);
        // Удалённую запись проигрыватель доигрывал бы до 410 на перемотке.
        const player = usePlayer.getState();
        if (player.queue[player.index]?.key === `rec:${r.id}`) player.close();
      }, 'Не удалось удалить запись');
    }
  };

  return (
    <div className="audio-recordings-manage">
      {recordings.length > 0 && (
        <ol className="audio-recordings-manage-list">
          {recordings.map((r, i) =>
            editing?.id === r.id ? (
              <li key={r.id} className="audio-recordings-manage-item">
                <span>{r.position}.</span>
                <input
                  type="text"
                  aria-label={`Кто читает запись ${r.position}`}
                  value={editing.draft}
                  autoFocus
                  onChange={(e) => setEditing({ id: r.id, draft: e.target.value })}
                  onKeyDown={(e) => {
                    if (e.key === 'Enter') void saveReader(r, editing.draft);
                    if (e.key === 'Escape') setEditing(null);
                  }}
                />
                <button
                  type="button"
                  className="btn btn-primary btn-sm"
                  onClick={() => void saveReader(r, editing.draft)}
                >
                  Сохранить
                </button>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => setEditing(null)}
                >
                  Отмена
                </button>
              </li>
            ) : (
              <li key={r.id} className="audio-recordings-manage-item">
                <span>{r.position}.</span>
                <span className="audio-recordings-reader">
                  {r.reader ? `Читает: ${r.reader}` : 'Чтец не указан'}
                </span>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  aria-label={`Изменить чтеца записи ${r.position}`}
                  onClick={() => setEditing({ id: r.id, draft: r.reader })}
                >
                  Изменить
                </button>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  aria-label="Выше"
                  disabled={i === 0}
                  onClick={() =>
                    void run(
                      () => audioApi.updateRecording(r.id, { position: r.position - 1 }),
                      'Не удалось переставить',
                    )
                  }
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  aria-label="Ниже"
                  disabled={i === recordings.length - 1}
                  onClick={() =>
                    void run(
                      () => audioApi.updateRecording(r.id, { position: r.position + 1 }),
                      'Не удалось переставить',
                    )
                  }
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="btn btn-danger btn-sm"
                  aria-label={`Удалить запись ${r.position}`}
                  onClick={() => remove(r)}
                >
                  Удалить
                </button>
              </li>
            ),
          )}
        </ol>
      )}

      <div className="audio-recordings-attach">
        <label>
          Кто читает (для прикрепляемых файлов)
          <input type="text" value={reader} onChange={(e) => setReader(e.target.value)} />
        </label>
        <label className="btn btn-secondary">
          Прикрепить запись
          <input
            type="file"
            multiple
            accept=".mp3,.m4a,.ogg,.opus,.flac"
            disabled={busy}
            className="audio-recordings-file"
            onChange={(e) => {
              const files = Array.from(e.currentTarget.files ?? []);
              e.currentTarget.value = '';
              if (files.length > 0) void attach(files);
            }}
          />
        </label>
      </div>

      {status.length > 0 && (
        <ul className="audio-recordings-status" aria-live="polite">
          {status.map((line) => (
            <li key={line.n}>{line.text}</li>
          ))}
        </ul>
      )}
    </div>
  );
};
