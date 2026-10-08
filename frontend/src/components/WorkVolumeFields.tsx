import type { Edition } from '../types';
import { partsForVolume } from '../utils/volumeParts';
import './WorkVolumeFields.css';

export interface WorkVolumePatch {
  editionId?: number | null;
  volumeNumber?: number | null;
  volumePart?: string | null;
  pageOffset?: number;
  shelfLabel?: string;
}

interface WorkVolumeFieldsProps {
  editions: Edition[];
  editionId: number | null;
  volumeNumber: number | null;
  volumePart: string | null;
  pageOffset: number;
  shelfLabel: string;
  onChange: (patch: WorkVolumePatch) => void;
}

export const WorkVolumeFields: React.FC<WorkVolumeFieldsProps> = ({
  editions,
  editionId,
  volumeNumber,
  volumePart,
  pageOffset,
  shelfLabel,
  onChange,
}) => {
  const parts = partsForVolume(volumeNumber);

  const handleVolumeNumber = (raw: string) => {
    // Пустое поле — это «нет номера», то есть null. Number('') === 0, а ноль
    // сервер отвергает как volume_number < 1.
    const next = raw.trim() === '' ? null : parseInt(raw, 10);
    const value = next !== null && Number.isNaN(next) ? null : next;

    // Полутом, ставший недопустимым, снимается здесь же: иначе на экране
    // осталась бы комбинация, о непригодности которой человек узнал бы
    // только из ответа сервера.
    if (volumePart !== null && !partsForVolume(value).includes(volumePart)) {
      onChange({ volumeNumber: value, volumePart: null });
      return;
    }
    onChange({ volumeNumber: value });
  };

  return (
    <fieldset className="work-volume-fields">
      <legend>Собрание и том</legend>

      <div className="form-group">
        <label htmlFor="work-edition">Собрание</label>
        <select
          id="work-edition"
          value={editionId ?? ''}
          onChange={(e) =>
            onChange({ editionId: e.target.value === '' ? null : parseInt(e.target.value, 10) })
          }
        >
          <option value="">— вне собраний —</option>
          {editions.map((edition) => (
            <option key={edition.id} value={edition.id}>
              {edition.title}
            </option>
          ))}
        </select>
      </div>

      <div className="form-row">
        <div className="form-group">
          <label htmlFor="work-volume-number">Номер тома</label>
          <input
            id="work-volume-number"
            type="number"
            min="1"
            value={volumeNumber ?? ''}
            onChange={(e) => handleVolumeNumber(e.target.value)}
            placeholder="—"
          />
          <p className="form-help-text">Пусто — справочный том, например указатель.</p>
        </div>

        <div className="form-group">
          <label htmlFor="work-volume-part">Полутом</label>
          <select
            id="work-volume-part"
            value={volumePart ?? ''}
            disabled={parts.length === 0}
            onChange={(e) =>
              onChange({ volumePart: e.target.value === '' ? null : e.target.value })
            }
          >
            <option value="">—</option>
            {parts.map((part) => (
              <option key={part} value={part}>
                {part}
              </option>
            ))}
          </select>
          <p className="form-help-text">Есть только у томов 25 и 26.</p>
        </div>

        <div className="form-group">
          <label htmlFor="work-page-offset">Смещение страниц</label>
          <input
            id="work-page-offset"
            type="number"
            value={pageOffset}
            onChange={(e) => {
              const parsed = parseInt(e.target.value, 10);
              onChange({ pageOffset: Number.isNaN(parsed) ? 0 : parsed });
            }}
          />
          <p className="form-help-text">печатная страница = номер + смещение</p>
        </div>
      </div>

      <div className="form-group">
        <label htmlFor="work-shelf-label">Подпись корешка</label>
        <input
          id="work-shelf-label"
          type="text"
          maxLength={120}
          value={shelfLabel}
          onChange={(e) => onChange({ shelfLabel: e.target.value })}
          placeholder="—"
        />
        <p className="form-help-text">
          Чем подписан том на полке. Пусто — подпись собирается из работ тома.
        </p>
      </div>
    </fieldset>
  );
};
