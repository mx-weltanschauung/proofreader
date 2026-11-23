import { useId, useRef, useState, type FormEvent } from 'react';
import { Link, useNavigate } from 'react-router-dom';
import { worksApi } from '../services/api';
import type { ShelfEdition, VolumeSummary, Work } from '../types';
import { apiErrorStatus } from '../utils/apiError';
import { pagePath } from '../utils/paths';
import {
  matchingEditions,
  parsePrintedReference,
  printedReferencePage,
  sameVolumePart,
} from '../utils/printedReference';
import { shownVolumeNumber } from '../utils/shownVolume';
import './PrintedPageLookup.css';

type Reference = NonNullable<ReturnType<typeof parsePrintedReference>>;
type Match = { volume: VolumeSummary; work: Work; pageNumber: number };
type Result =
  | { message: string }
  | { message: string; reference: Reference; editions: ShelfEdition[] }
  | { message: string; matches: Match[]; page: string };

async function visibleResponse<T>(request: Promise<{ data: T }>): Promise<T | null> {
  try {
    return (await request).data;
  } catch (error) {
    // Между загрузкой полки и поиском том или служебную работу могли закрыть.
    if (apiErrorStatus(error) === 404) return null;
    throw error;
  }
}

async function findMatches(volume: VolumeSummary, page: string): Promise<Match[]> {
  let works: Work[] = [volume];
  // У тома и его предваряющих материалов отдельные шкалы. Римская ссылка
  // может вести на вводные листы, которые полка сама не перечисляет.
  if (/^[IVXLCDM]+$/.test(page)) {
    const detail = await visibleResponse(worksApi.get(volume.id));
    if (!detail) return [];
    works = [detail, ...(detail.children ?? [])];
  }

  const matches = await Promise.all(
    works.map(async (work): Promise<Match | null> => {
      const pageNumber = printedReferencePage(page, work);
      if (pageNumber === null) return null;
      const map = await visibleResponse(worksApi.pageMap(work.id));
      return map?.some((entry) => entry.page_number === pageNumber)
        ? { volume, work, pageNumber }
        : null;
    }),
  );
  return matches.filter((match) => match !== null);
}

function matchLabel({ volume, work }: Match, page: string): string {
  const part = volume.volume_part ? 'Часть ' + volume.volume_part + ' · ' : '';
  return part + work.title + ', с. ' + page;
}

export function PrintedPageLookup({ shelves }: { shelves: ShelfEdition[] }) {
  const id = useId();
  const navigate = useNavigate();
  const inputRef = useRef<HTMLTextAreaElement>(null);
  const [query, setQuery] = useState('');
  const [result, setResult] = useState<Result | null>(null);
  const [pending, setPending] = useState(false);

  async function lookup(shelf: ShelfEdition, reference: Reference) {
    setResult(null);
    setPending(true);
    const volumes = shelf.volumes.filter(
      (volume) =>
        shownVolumeNumber(volume) === reference.volume &&
        (!reference.part || sameVolumePart(reference.part, volume.volume_part)),
    );
    const source = shelf.edition.title + ' · т. ' + reference.volume;
    const location = reference.part ? source + ', ч. ' + reference.part : source;

    try {
      const matches = (
        await Promise.all(volumes.map((v) => findMatches(v, reference.page)))
      ).flat();
      if (!inputRef.current) return;
      if (matches.length === 1) {
        const { work, pageNumber } = matches[0];
        navigate(pagePath(work, pageNumber));
      } else if (matches.length > 1) {
        setResult({
          message: location + '. Выберите нужную полосу.',
          matches,
          page: reference.page,
        });
      } else {
        setResult({ message: location + '. Страница ' + reference.page + ' не найдена.' });
      }
    } catch {
      if (inputRef.current) {
        setResult({ message: 'Не удалось проверить ссылку. Попробуйте ещё раз.' });
      }
    } finally {
      if (inputRef.current) setPending(false);
    }
  }

  function submit(event: FormEvent<HTMLFormElement>) {
    event.preventDefault();
    if (pending) return;
    const reference = parsePrintedReference(query);
    if (!reference) {
      setResult({ message: 'Укажите название издания, том и страницу по образцу в поле.' });
      return;
    }
    const eligible = matchingEditions(
      reference.source,
      shelves.map((shelf) => shelf.edition),
    );
    const matches = shelves.filter((shelf) => eligible.includes(shelf.edition));
    if (matches.length === 1) {
      void lookup(matches[0], reference);
    } else if (matches.length > 1) {
      setResult({ message: 'Уточните издание.', reference, editions: matches });
    } else {
      setResult({ message: 'Издание с таким названием или номером не найдено. Уточните ссылку.' });
    }
  }

  return (
    <section className="printed-page-lookup" aria-label="Поиск по печатной ссылке">
      <form onSubmit={submit}>
        <label htmlFor={id}>Библиографическая ссылка</label>
        <div className="printed-page-lookup-fields">
          <textarea
            ref={inputRef}
            id={id}
            rows={2}
            spellCheck={false}
            value={query}
            onChange={(event) => {
              setQuery(event.target.value);
              setResult(null);
            }}
            onKeyDown={(event) => {
              if (event.key === 'Enter' && !event.shiftKey && !event.nativeEvent.isComposing) {
                event.preventDefault();
                event.currentTarget.form?.requestSubmit();
              }
            }}
            placeholder="Сочинения, 5-е изд., т. 4, с. 358"
            maxLength={1200}
            readOnly={pending}
          />
          <button className="btn btn-primary" type="submit" disabled={pending}>
            Найти страницу
          </button>
        </div>
      </form>
      <div className="printed-page-lookup-status" role="status">
        {pending ? 'Проверяем ссылку…' : result?.message}
      </div>
      {result && 'editions' in result && (
        <ul className="printed-page-lookup-options">
          {result.editions.map((shelf) => (
            <li key={shelf.edition.id}>
              <button
                className="btn btn-secondary"
                type="button"
                onClick={() => {
                  inputRef.current?.focus();
                  void lookup(shelf, result.reference);
                }}
              >
                {shelf.edition.title}
              </button>
            </li>
          ))}
        </ul>
      )}
      {result && 'matches' in result && (
        <ul className="printed-page-lookup-options">
          {result.matches.map((match) => (
            <li key={match.work.id}>
              <Link className="btn btn-secondary" to={pagePath(match.work, match.pageNumber)}>
                {matchLabel(match, result.page)}
              </Link>
            </li>
          ))}
        </ul>
      )}
    </section>
  );
}
