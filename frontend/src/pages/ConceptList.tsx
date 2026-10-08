import { useEffect, useRef, useState } from 'react';
import { Link, useSearchParams } from 'react-router-dom';
import { conceptsApi } from '../services/api';
import type { ConceptSummary } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { CONCEPT_ALPHABET } from '../utils/conceptAlphabet';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import './ConceptList.css';

export const PAGE_SIZE = 100;

export const ConceptList: React.FC = () => {
  // Тот же заголовок, что печатает internal/seo/render_index.go (ConceptList):
  // «Предметный указатель — Читальня». Без него JS ставил бы просто
  // «Читальня» — расхождение с отданным краулеру HTML на индексируемой
  // странице.
  useDocumentTitle('Предметный указатель');

  const [searchParams, setSearchParams] = useSearchParams();
  const query = searchParams.get('q') ?? '';
  const letter = searchParams.get('letter') ?? '';

  const [concepts, setConcepts] = useState<ConceptSummary[]>([]);
  const [hasMore, setHasMore] = useState(false);
  const [isAppending, setIsAppending] = useState(false);
  const [error, setError] = useState('');

  // Ввод отражается сразу, а в API уезжает через паузу: иначе каждый символ
  // названия — отдельный ILIKE по таблице.
  const [draft, setDraft] = useState(query);
  // Подгонка draft под query при внешней смене URL (клик по букве, кнопка
  // «назад») — без эффекта: сравнение с состоянием предыдущего рендера
  // прямо в теле компонента, как рекомендует React для «подстроить
  // состояние под изменившийся проп» (react.dev/learn/you-might-not-need-an-effect).
  // Эффект с setState в теле здесь не годится — react-hooks/set-state-in-effect
  // из recommended-latest запрещает синхронный setState в эффекте.
  const [prevQuery, setPrevQuery] = useState(query);
  if (query !== prevQuery) {
    setPrevQuery(query);
    setDraft(query);
  }

  useEffect(() => {
    if (draft === query) return;
    const timer = setTimeout(() => {
      // Поиск и буква исключают друг друга: сервер складывает их через AND,
      // и «нашлось ноль» стало бы неотличимо от «в этой букве ничего нет».
      setSearchParams(draft ? { q: draft } : {}, { replace: true });
    }, 250);
    return () => clearTimeout(timer);
  }, [draft, query, setSearchParams]);

  // Загрузка — не отдельный флаг, а сравнение параметров текущего запроса с
  // параметрами последнего завершённого: так isLoading остаётся производным
  // значением, и эффекту не нужен синхронный setState(true) в начале тела.
  const [loaded, setLoaded] = useState<{ query: string; letter: string } | null>(null);
  const isLoading = loaded === null || loaded.query !== query || loaded.letter !== letter;

  useEffect(() => {
    let cancelled = false;

    conceptsApi
      .list({ q: query, letter, limit: PAGE_SIZE, offset: 0 })
      .then((response) => {
        if (cancelled) return;
        const page = response.data ?? [];
        setConcepts(page);
        setHasMore(page.length === PAGE_SIZE);
        setError('');
        setLoaded({ query, letter });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setError(apiErrorMessage(err, 'Не удалось загрузить понятия'));
        setConcepts([]);
        setHasMore(false);
        setLoaded({ query, letter });
      });

    return () => {
      cancelled = true;
    };
  }, [query, letter]);

  // Основной эффект загрузки защищён флагом cancelled от собственной же
  // устаревшей версии, а loadMore — асинхронный обработчик клика, а не
  // эффект, так что у него нет автоматической отмены. Пока его ответ летит,
  // query/letter могут смениться (например, кликом по другой букве); ref
  // всегда хранит те query/letter, что сейчас закреплены за списком —
  // сверяем их со значениями на момент запроса и, если они разошлись,
  // ответ выбрасываем, а не дописываем в чужой список.
  const currentParamsRef = useRef({ query, letter });
  useEffect(() => {
    currentParamsRef.current = { query, letter };
  }, [query, letter]);

  const loadMore = async () => {
    const requestedParams = { query, letter };
    setIsAppending(true);
    try {
      const response = await conceptsApi.list({
        q: query,
        letter,
        limit: PAGE_SIZE,
        offset: concepts.length,
      });
      if (
        currentParamsRef.current.query !== requestedParams.query ||
        currentParamsRef.current.letter !== requestedParams.letter
      ) {
        return;
      }
      const page = response.data ?? [];
      setConcepts((prev) => [...prev, ...page]);
      // Общего количества API не отдаёт, поэтому «есть ли ещё» выводится из
      // длины ответа: короче лимита — значит страница последняя.
      setHasMore(page.length === PAGE_SIZE);
    } catch (err: unknown) {
      if (
        currentParamsRef.current.query !== requestedParams.query ||
        currentParamsRef.current.letter !== requestedParams.letter
      ) {
        return;
      }
      setError(apiErrorMessage(err, 'Не удалось загрузить понятия'));
    } finally {
      setIsAppending(false);
    }
  };

  return (
    <div className="concept-list-container">
      <h1>Предметный указатель</h1>

      <input
        type="search"
        className="concept-search"
        value={draft}
        onChange={(e) => setDraft(e.target.value)}
        placeholder="Поиск по названию"
        aria-label="Поиск по названию понятия"
      />

      <div className="concept-alphabet">
        <button
          type="button"
          className={letter === '' && query === '' ? 'is-active' : ''}
          aria-pressed={letter === '' && query === ''}
          onClick={() => {
            // Сбрасываем draft прямо тут, а не через синхронизацию с query:
            // та реагирует только на смену query, а «Все»/буква меняют
            // letter, оставляя query как есть. Без явного сброса уже
            // взведённый таймер дебаунса (он привязан к draft/query, про
            // letter не знает) через 250 мс отправит старый недопечатанный
            // текст и затрёт только что сделанный выбор.
            setDraft('');
            setSearchParams({});
          }}
        >
          Все
        </button>
        {CONCEPT_ALPHABET.map((ch) => (
          <button
            key={ch}
            type="button"
            className={letter === ch.toLowerCase() ? 'is-active' : ''}
            aria-pressed={letter === ch.toLowerCase()}
            // sort_key лежит в нижнем регистре, а фильтр на сервере —
            // LIKE letter || '%'.
            onClick={() => {
              // См. комментарий у кнопки «Все» — тот же сброс той же гонки.
              setDraft('');
              setSearchParams({ letter: ch.toLowerCase() });
            }}
          >
            {ch}
          </button>
        ))}
      </div>

      {error && <div className="error-message">{error}</div>}

      {isLoading ? (
        <div className="loading-state">Загрузка понятий…</div>
      ) : concepts.length === 0 && !error ? (
        <div className="empty-state">
          <p>Ничего не найдено.</p>
        </div>
      ) : (
        <>
          <ul className="concept-list">
            {concepts.map((concept) => (
              <li key={concept.id}>
                <Link to={`/concepts/${concept.slug}`}>{concept.title}</Link>
                {concept.kind === 'redirect' && <span className="concept-redirect-mark">см.</span>}
              </li>
            ))}
          </ul>

          {hasMore && (
            <button
              type="button"
              className="concept-load-more"
              onClick={() => void loadMore()}
              disabled={isAppending}
            >
              {isAppending ? 'Загрузка…' : 'Показать ещё'}
            </button>
          )}
        </>
      )}
    </div>
  );
};
