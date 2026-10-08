import { useEffect, useState } from 'react';
import type { Shelf, VolumeSummary } from '../types';
import type { SearchScope } from '../utils/searchScope';
import { loadShelfOnce } from '../utils/shelfCache';
import './ScopePicker.css';

interface Props {
  value: SearchScope;
  onChange: (scope: SearchScope) => void;
}

type LoadState = 'loading' | 'error' | 'ready';

/** Добавляет/снимает id и держит список отсортированным — порядок клика на
 * порядок в адресе влиять не должен. */
function toggleId(list: number[], id: number): number[] {
  return list.includes(id) ? list.filter((x) => x !== id) : [...list, id].sort((a, b) => a - b);
}

/**
 * Служебные работы (передние листы конкретного тома) на полке — не то, что
 * читатель выбирает: поиск по самому тому и так захватывает их (сделано на
 * сервере). `/api/shelf` их и не отдаёт, но проверка здесь не лишняя —
 * фронт не должен молча повести читателя не в тот адрес, если это когда-то
 * изменится.
 */
function isServiceWork(work: Pick<VolumeSummary, 'parent_work_id' | 'role'>): boolean {
  return work.parent_work_id != null || work.role === 'front_matter';
}

/**
 * Выбор области поиска: собрания и тома галочками. Полка (`/api/shelf`)
 * запрашивается один раз за сессию через `loadShelfOnce` — она не меняется
 * между открытиями панели, а стоит 22 КБ и треть секунды на боевом.
 */
export const ScopePicker: React.FC<Props> = ({ value, onChange }) => {
  const [state, setState] = useState<LoadState>('loading');
  const [shelf, setShelf] = useState<Shelf | null>(null);
  // Раскрытые собрания — только вид, не часть области: у Ленина 55 томов, и
  // держать их список развёрнутым для всех сразу незачем.
  const [expanded, setExpanded] = useState<ReadonlySet<number>>(new Set());

  useEffect(() => {
    let cancelled = false;
    loadShelfOnce()
      .then((data) => {
        if (!cancelled) {
          setShelf(data);
          setState('ready');
        }
      })
      .catch(() => {
        if (!cancelled) setState('error');
      });
    return () => {
      cancelled = true;
    };
  }, []);

  const toggleEdition = (id: number) => {
    onChange({ ...value, editions: toggleId(value.editions, id) });
  };

  const toggleWork = (id: number) => {
    // Любое изменение набора томов сбрасывает главы: они принадлежат
    // конкретному тому, и оставшиеся от прежнего выбора увели бы выдачу не
    // туда — вплоть до тома, у которого таких глав вовсе нет.
    onChange({ editions: value.editions, works: toggleId(value.works, id), chapters: [] });
  };

  const toggleExpanded = (id: number) => {
    setExpanded((prev) => {
      const next = new Set(prev);
      if (next.has(id)) next.delete(id);
      else next.add(id);
      return next;
    });
  };

  if (state === 'error') {
    // Поле запроса рисует SearchPanel независимо от этого компонента — поиск
    // по всей читальне остаётся возможен, даже если полка не загрузилась.
    return <p className="scope-picker-error">Не удалось загрузить список собраний</p>;
  }

  if (state === 'loading' || !shelf) {
    return <p className="scope-picker-loading">Загрузка списка собраний…</p>;
  }

  return (
    <div className="scope-picker">
      <ul className="scope-picker-editions">
        {shelf.editions.map(({ edition, volumes }) => {
          const isExpanded = expanded.has(edition.id);
          const visibleVolumes = volumes.filter((v) => !isServiceWork(v));
          return (
            <li key={edition.id} className="scope-picker-edition">
              <div className="scope-picker-edition-row">
                <label className="scope-picker-checkbox-label">
                  <input
                    type="checkbox"
                    checked={value.editions.includes(edition.id)}
                    onChange={() => toggleEdition(edition.id)}
                  />
                  {edition.title}
                </label>
                {visibleVolumes.length > 0 && (
                  <button
                    type="button"
                    className="scope-picker-toggle"
                    aria-expanded={isExpanded}
                    onClick={() => toggleExpanded(edition.id)}
                  >
                    {isExpanded ? 'Скрыть тома' : 'Показать тома'}
                  </button>
                )}
              </div>
              {isExpanded && (
                <ul className="scope-picker-volumes">
                  {visibleVolumes.map((volume) => (
                    <li key={volume.id}>
                      <label className="scope-picker-checkbox-label">
                        <input
                          type="checkbox"
                          checked={value.works.includes(volume.id)}
                          onChange={() => toggleWork(volume.id)}
                        />
                        {volume.title}
                      </label>
                    </li>
                  ))}
                </ul>
              )}
            </li>
          );
        })}
      </ul>
      {shelf.loose_works.length > 0 && (
        <ul className="scope-picker-loose">
          {shelf.loose_works.map((work) => (
            <li key={work.id}>
              <label className="scope-picker-checkbox-label">
                <input
                  type="checkbox"
                  checked={value.works.includes(work.id)}
                  onChange={() => toggleWork(work.id)}
                />
                {work.title}
              </label>
            </li>
          ))}
        </ul>
      )}
    </div>
  );
};
