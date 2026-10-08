import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import toast from 'react-hot-toast';
import { chaptersApi, editionsApi } from '../services/api';
import { EditionHighlights } from '../components/EditionHighlights';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import type { Chapter, Edition, EditionHighlight, VolumeSummary } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { splitEditionVolumes } from '../utils/editionStats';
import { highlightCoordinate, highlightLabel } from '../utils/highlightLabel';
import { editionPath, numericId } from '../utils/paths';
import { pickableChapters } from '../utils/pickableChapters';
import { chapterLabel, spineNumber, volumeLabel } from '../utils/volumeLabel';
import './EditionHighlightsEdit.css';

/** Потолок списка — тот же, что проверяет сервер (maxHighlights). */
export const MAX_HIGHLIGHTS = 8;

/** Что уходит на сервер: глава и подпись, по порядку. */
function payload(list: EditionHighlight[]) {
  return list.map(({ chapter_id, label }) => ({ chapter_id, label }));
}

/**
 * Правка избранного собрания: предпросмотр тем же компонентом, что на
 * главной, список пунктов со стрелками и выбор «том → глава». Сохраняется
 * список целиком — так же устроен и PUT на сервере.
 */
export const EditionHighlightsEdit: React.FC = () => {
  const { id: idParam } = useParams<{ id: string }>();
  const id = numericId(idParam);

  const [edition, setEdition] = useState<Edition | null>(null);
  const [volumes, setVolumes] = useState<VolumeSummary[]>([]);
  const [saved, setSaved] = useState<EditionHighlight[]>([]);
  const [draft, setDraft] = useState<EditionHighlight[]>([]);
  const [pickVolume, setPickVolume] = useState('');
  const [chapters, setChapters] = useState<{ chapter: Chapter; depth: number }[]>([]);
  const [pickChapter, setPickChapter] = useState('');
  const [error, setError] = useState('');
  const [isLoading, setIsLoading] = useState(true);
  // Первичная загрузка не удалась: пустой черновик сохранять нельзя — PUT
  // заменяет список целиком и стёр бы настоящий.
  const [loadFailed, setLoadFailed] = useState(false);
  const [isSaving, setIsSaving] = useState(false);

  useDocumentTitle(edition ? `Избранное — ${edition.title}` : 'Избранное собрания');

  useEffect(() => {
    if (id === null) return;
    const load = async () => {
      try {
        const [e, v, h] = await Promise.all([
          editionsApi.get(id),
          editionsApi.volumes(id),
          editionsApi.highlights(id),
        ]);
        setEdition(e.data);
        setVolumes(splitEditionVolumes(v.data ?? []).shelf);
        setSaved(h.data);
        setDraft(h.data);
      } catch (err: unknown) {
        setLoadFailed(true);
        setError(apiErrorMessage(err, 'Не удалось загрузить избранное'));
      } finally {
        setIsLoading(false);
      }
    };
    void load();
  }, [id]);

  useEffect(() => {
    if (pickVolume === '') return;
    // Ответ прежнего тома, пришедший позже ответа нового, не должен
    // перезаписать список глав: иначе глава одного тома уехала бы в
    // черновик с координатами другого.
    let ignore = false;
    const load = async () => {
      try {
        const { data } = await chaptersApi.list(Number(pickVolume));
        if (!ignore) setChapters(pickableChapters(data ?? []));
      } catch (err: unknown) {
        if (!ignore) setError(apiErrorMessage(err, 'Не удалось загрузить главы тома'));
      }
    };
    void load();
    return () => {
      ignore = true;
    };
  }, [pickVolume]);

  if (id === null) return <div className="error-message">Неверный адрес собрания</div>;
  if (isLoading) return <div className="loading-state">Загружаем избранное…</div>;

  if (loadFailed) {
    return (
      <div className="highlights-edit">
        <div className="error-message">{error}</div>
      </div>
    );
  }

  const dirty = JSON.stringify(payload(draft)) !== JSON.stringify(payload(saved));
  const full = draft.length >= MAX_HIGHLIGHTS;

  // Смена тома сбрасывает выбор главы здесь, а не в эффекте: setState в
  // эффекте запрещён правилом react-hooks/set-state-in-effect.
  const changeVolume = (value: string) => {
    setPickVolume(value);
    setPickChapter('');
    setChapters([]);
  };

  const add = () => {
    const volume = volumes.find((v) => v.id === Number(pickVolume));
    const picked = chapters.find((x) => x.chapter.id === Number(pickChapter))?.chapter;
    if (!volume || !picked) return;
    if (draft.some((h) => h.chapter_id === picked.id)) {
      setError('Эта глава уже в избранном');
      return;
    }
    setError('');
    setDraft([
      ...draft,
      {
        chapter_id: picked.id,
        chapter_slug: picked.slug ?? '',
        chapter_title: picked.title,
        work_id: volume.id,
        work_slug: volume.slug ?? '',
        volume_number: volume.volume_number ?? null,
        volume_part: volume.volume_part ?? null,
        label: '',
      },
    ]);
    setPickChapter('');
  };

  const move = (at: number, by: -1 | 1) => {
    const next = [...draft];
    [next[at], next[at + by]] = [next[at + by], next[at]];
    setDraft(next);
  };

  const save = async () => {
    setIsSaving(true);
    setError('');
    try {
      const { data } = await editionsApi.saveHighlights(id, payload(draft));
      setSaved(data);
      setDraft(data);
      toast.success('Избранное сохранено');
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить избранное'));
    } finally {
      setIsSaving(false);
    }
  };

  return (
    <div className="highlights-edit">
      {edition && (
        <Link to={editionPath(edition)} className="back-link">
          ← {edition.title}
        </Link>
      )}
      <h1>Избранное собрания</h1>

      <section aria-label="Предпросмотр" className="highlights-edit-preview">
        <EditionHighlights highlights={draft} />
        {draft.length === 0 && <p>Избранного нет — на главной будет только ряд томов.</p>}
      </section>

      {error && <div className="error-message">{error}</div>}

      <ol className="highlights-edit-list">
        {draft.map((h, at) => {
          const name = highlightLabel(h);
          return (
            <li key={h.chapter_id} className="highlights-edit-item">
              <label>
                <span className="visually-hidden">Подпись: {name}</span>
                <input
                  type="text"
                  value={h.label}
                  maxLength={80}
                  placeholder={chapterLabel(h.chapter_title)}
                  onChange={(e) =>
                    setDraft(draft.map((x, i) => (i === at ? { ...x, label: e.target.value } : x)))
                  }
                />
              </label>
              <span className="highlights-edit-source">
                {highlightCoordinate(h)} · {h.chapter_title}
              </span>
              <span className="highlights-edit-buttons">
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  disabled={at === 0}
                  aria-label={`Выше: ${name}`}
                  onClick={() => move(at, -1)}
                >
                  ↑
                </button>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  disabled={at === draft.length - 1}
                  aria-label={`Ниже: ${name}`}
                  onClick={() => move(at, 1)}
                >
                  ↓
                </button>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  aria-label={`Убрать: ${name}`}
                  onClick={() => setDraft(draft.filter((_, i) => i !== at))}
                >
                  ×
                </button>
              </span>
            </li>
          );
        })}
      </ol>

      <fieldset className="highlights-edit-add">
        <legend>Добавить работу</legend>
        {/* Подпись отдельно от select, а не обёрткой: обёрнутая метка вбирает
            в своё имя и текст всех опций. */}
        <label htmlFor="highlights-volume">Том</label>
        <select
          id="highlights-volume"
          value={pickVolume}
          onChange={(e) => changeVolume(e.target.value)}
        >
          <option value="">— выберите том —</option>
          {volumes.map((v) => {
            const label = volumeLabel(v);
            return (
              <option key={v.id} value={v.id}>
                {`Том ${spineNumber(v)}${label ? `. ${label}` : ''}`}
              </option>
            );
          })}
        </select>
        <label htmlFor="highlights-chapter">Глава</label>
        <select
          id="highlights-chapter"
          value={pickChapter}
          disabled={chapters.length === 0}
          onChange={(e) => setPickChapter(e.target.value)}
        >
          <option value="">— выберите главу —</option>
          {chapters.map(({ chapter, depth }) => (
            <option key={chapter.id} value={chapter.id}>
              {`${'  '.repeat(depth)}${chapterLabel(chapter.title)}`}
            </option>
          ))}
        </select>
        <button
          type="button"
          className="btn btn-secondary"
          disabled={full || pickChapter === ''}
          onClick={add}
        >
          Добавить
        </button>
        {full && (
          <p className="highlights-edit-note">В избранном не больше {MAX_HIGHLIGHTS} работ.</p>
        )}
      </fieldset>

      <button
        type="button"
        className="btn btn-primary"
        disabled={!dirty || isSaving}
        onClick={() => void save()}
      >
        Сохранить
      </button>
    </div>
  );
};
