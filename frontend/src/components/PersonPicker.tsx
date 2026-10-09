import { useEffect, useState } from 'react';
import { Link } from 'react-router-dom';
import { personsApi } from '../services/api';
import type { Person } from '../types';
import './PersonPicker.css';

export interface PickedPerson {
  id: number;
  name: string;
  slug: string;
}

export interface PersonPickerProps {
  /** Номер строки подписи с единицы — для подписей полей. */
  index: number;
  personId: number | null;
  /** Имя (выбран из поиска) или слаг (приехал с подписью); пусто — неизвестно. */
  personName: string;
  personSlug: string;
  onChange: (person: PickedPerson | null) => void;
}

/**
 * Человек подписи статьи: поиск по имени (`GET /persons?q=`, тот же, что у
 * слияния на странице автора) или «без человека». Голого номера в форме нет:
 * номер человека нигде на экранах не показан, и вводить его было неоткуда.
 */
export function PersonPicker({
  index,
  personId,
  personName,
  personSlug,
  onChange,
}: PersonPickerProps) {
  const [query, setQuery] = useState('');
  // Выдача помнит, на какой запрос она пришла: «никого не нашлось» до
  // ответа сервера было бы неправдой.
  const [result, setResult] = useState<{ q: string; people: Person[] } | null>(null);
  const q = query.trim();
  const searching = personId === null && q.length >= 2;
  const found = searching && result?.q === q ? result.people : null;

  useEffect(() => {
    if (!searching) return;
    let alive = true;
    void personsApi.search(q).then(
      ({ data }) => alive && setResult({ q, people: data }),
      () => alive && setResult({ q, people: [] }),
    );
    return () => {
      alive = false;
    };
  }, [q, searching]);

  if (personId !== null) {
    const label = personName || personSlug || `№ ${personId}`;
    return (
      <div className="person-picker">
        <span className="person-picker-current">
          Человек:{' '}
          {personSlug ? (
            <Link to={`/authors/${encodeURIComponent(personSlug)}`}>{label}</Link>
          ) : (
            label
          )}
        </span>
        <button
          type="button"
          className="btn btn-secondary btn-sm"
          aria-label={`Без человека ${index}`}
          onClick={() => {
            setQuery('');
            onChange(null);
          }}
        >
          Без человека
        </button>
      </div>
    );
  }

  return (
    <div className="person-picker">
      <input
        aria-label={`Найти человека ${index}`}
        placeholder="без человека — найти по фамилии"
        value={query}
        onChange={(e) => setQuery(e.target.value)}
      />
      {found && found.length > 0 && (
        <ul className="person-picker-results">
          {found.map((p) => (
            <li key={p.id}>
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                onClick={() => onChange({ id: p.id, name: p.name, slug: p.slug })}
              >
                {p.name}
              </button>
            </li>
          ))}
        </ul>
      )}
      {found && found.length === 0 && (
        <span className="person-picker-empty">никого не нашлось</span>
      )}
    </div>
  );
}
