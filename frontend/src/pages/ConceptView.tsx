import { useEffect, useMemo, useState } from 'react';
import { useParams, useSearchParams, Link } from 'react-router-dom';
import { conceptsApi } from '../services/api';
import type { Concept, ConceptOrder } from '../types';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { askAiConceptPrompt } from '../utils/askAi';
import { plural } from '../utils/volumeLabel';
import { AskAiButton } from '../components/AskAiButton';
import { ShareButton } from '../components/ShareButton';
import { ConceptAddressPanel } from '../components/ConceptAddressPanel';
import { ConceptFragmentStream } from '../components/ConceptFragmentStream';
import { useConceptFragments } from '../hooks/useConceptFragments';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { DEFAULT_CONCEPT_ORDER, parseConceptOrder } from '../utils/conceptOrder';
import { decodeRubricPath, encodeRubricPath } from '../utils/rubricPathParam';
import './ConceptView.css';

// Единое состояние загрузки вместо связки isLoading/notFound/error: они
// иначе рассинхронизируются между собой, а обнулять их синхронно в начале
// эффекта запрещает react-hooks/set-state-in-effect (см. useChaptersForPage.ts).
type LoadState =
  | { status: 'loading' }
  | { status: 'ok'; concept: Concept }
  | { status: 'notFound' }
  | { status: 'error'; message: string };

export const ConceptView: React.FC = () => {
  const { slug } = useParams<{ slug: string }>();
  const [params, setParams] = useSearchParams();
  const [state, setState] = useState<LoadState>({ status: 'loading' });

  const rubric = params.get('rubric') ?? '';
  const rubricPath = decodeRubricPath(params.get('rubric_path') ?? '');
  const volume = params.get('volume') ?? '';
  const order = parseConceptOrder(params.get('order'));

  // Средний сегмент — тот же, что печатает internal/seo/render_index.go
  // (Concept): «название — предметный указатель — Читальня».
  useDocumentTitle(state.status === 'ok' ? `${state.concept.title} — предметный указатель` : null);

  // Смена slug (переход по отсылке на другое понятие) должна сразу сбросить
  // прошлый результат — иначе экран покажет статью уже покинутого понятия,
  // пока летит ответ на новый запрос.
  const [prevSlug, setPrevSlug] = useState(slug);
  if (prevSlug !== slug) {
    setPrevSlug(slug);
    setState({ status: 'loading' });
  }

  useEffect(() => {
    if (!slug) return;
    let cancelled = false;

    conceptsApi
      .get(slug)
      .then((response) => {
        if (!cancelled) setState({ status: 'ok', concept: response.data });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setState(
          apiErrorStatus(err) === 404
            ? { status: 'notFound' }
            : { status: 'error', message: apiErrorMessage(err, 'Не удалось загрузить понятие') },
        );
      });

    return () => {
      cancelled = true;
    };
  }, [slug]);

  // Поток запрашивается только для статей: у отсылки адресов нет вовсе.
  // Понятие может нести несколько статей от разных указателей — оно
  // считается статьёй, если хотя бы одна из них не отсылка (та же логика,
  // что у kind списка навигатора: IndexRepository.listConceptsQuery считает
  // понятие статьёй, если хоть один указатель дал о нём статью).
  const isArticle =
    state.status === 'ok' && state.concept.articles.some((a) => a.kind !== 'redirect');
  const stream = useConceptFragments(
    isArticle ? slug : undefined,
    rubric,
    rubricPath,
    volume,
    order,
  );

  // Множество адресов, уже приехавших в поток. Панель по нему решает, вести
  // ссылку на якорь или на страницу тома: якоря вне загруженного окна ещё
  // нет в документе.
  const loadedRefs = useMemo(
    () => new Set(stream.entries.map((e) => e.reference_id)),
    [stream.entries],
  );

  if (state.status === 'loading') {
    return <div className="loading-state">Загрузка понятия…</div>;
  }

  if (state.status !== 'ok') {
    return (
      <div className="error-state">
        <div>{state.status === 'notFound' ? 'Понятие не найдено' : state.message}</div>
        <Link to="/concepts" className="back-link">
          ← К указателю
        </Link>
      </div>
    );
  }

  const concept = state.concept;
  // Сумма адресов ВСЕХ статей: понятие с несколькими статьями показывает
  // общий охват, а не охват первой попавшейся.
  const addresses = concept.articles.reduce((sum, a) => sum + a.references.length, 0);

  // «Поделиться» несёт фильтр экрана — ровно четыре его параметра, которые
  // этот экран сам и пишет в адрес (ниже); прочее в строке браузера получателю
  // ни к чему. Подрубрика попадает и в подпись: иначе получатель прочёл бы
  // «понятие целиком», а открыл кусок.
  const shareQuery = new URLSearchParams();
  for (const key of ['rubric', 'rubric_path', 'volume', 'order']) {
    const value = params.get(key);
    if (value) shareQuery.set(key, value);
  }
  const shareRubric = rubricPath.length > 0 ? rubricPath.join(' / ') : rubric;
  const shareTitle = `${shareRubric ? `${concept.title}: ${shareRubric}` : concept.title} — предметный указатель`;
  const sharePath = `/concepts/${concept.slug}${shareQuery.size > 0 ? `?${shareQuery}` : ''}`;

  const onFilterChange = (next: { rubric: string; rubricPath: string[]; volume: string }) => {
    const updated = new URLSearchParams(params);
    // Фильтр живёт в URL: отфильтрованный вид шарится ссылкой и переживает
    // «назад». Два параметра подрубрики никогда не пишутся разом — путь,
    // выбранный в списке, вытесняет плоское имя пришедшей закладки.
    if (next.rubric) updated.set('rubric', next.rubric);
    else updated.delete('rubric');
    if (next.rubricPath.length > 0) updated.set('rubric_path', encodeRubricPath(next.rubricPath));
    else updated.delete('rubric_path');
    if (next.volume) updated.set('volume', next.volume);
    else updated.delete('volume');
    setParams(updated);
  };

  const onOrderChange = (next: ConceptOrder) => {
    const updated = new URLSearchParams(params);
    // Дефолт в URL не пишем: адрес понятия без параметров и так означает
    // порядок по подрубрикам.
    if (next === DEFAULT_CONCEPT_ORDER) updated.delete('order');
    else updated.set('order', next);
    setParams(updated);
  };

  return (
    <div className="concept-view-container">
      <div className="concept-view-header">
        <div>
          <h1>{concept.title}</h1>
          {isArticle && (
            <p className="concept-view-coverage">
              доступно {plural(stream.total, ['фрагмент', 'фрагмента', 'фрагментов'])} из{' '}
              {plural(addresses, ['адреса', 'адресов', 'адресов'])}
              {/* «из 1 адреса», «из 2 адресов», «из 95 адресов» — родительный
                  падеж во всех формах, поэтому первая форма здесь «адреса». */}
            </p>
          )}
        </div>
        {/* Кнопки одной группой: шапка разводит детей по краям
            (space-between), и третий ребёнок повисал посередине. */}
        <div className="concept-view-actions">
          <AskAiButton
            prompt={() => askAiConceptPrompt(concept, window.location.origin, rubricPath)}
          />
          <ShareButton title={shareTitle} path={sharePath} />
        </div>
        {/* Ссылка на скан статьи («Указатель, стр. N») переехала в блок
            статьи (ConceptArticleBlock) — у понятия с несколькими статьями
            у каждой свой скан и свой work_id. */}
      </div>

      <div className="concept-view-columns">
        <ConceptAddressPanel
          concept={concept}
          rubric={rubric}
          rubricPath={rubricPath}
          volume={volume}
          onFilterChange={onFilterChange}
          loadedRefs={loadedRefs}
        />
        {isArticle && (
          <ConceptFragmentStream
            stream={stream}
            slug={slug ?? ''}
            order={order}
            onOrderChange={onOrderChange}
          />
        )}
      </div>
    </div>
  );
};
