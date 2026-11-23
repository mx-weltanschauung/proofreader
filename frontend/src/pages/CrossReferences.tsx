import { useId, useMemo, useState } from 'react';
import {
  skipToken,
  useInfiniteQuery,
  useMutation,
  useQuery,
  useQueryClient,
} from '@tanstack/react-query';
import { Link, useParams } from 'react-router-dom';
import { ClosedBadge } from '../components/ClosedBadge';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { crossReferencesApi, editionsApi, readingApi } from '../services/api';
import type {
  CrossReferenceCandidate,
  CrossReferenceDecision,
  CrossReferenceIssue,
  CrossReferenceLocation,
  CrossReferenceStatus,
  Edition,
  VolumeSummary,
} from '../types';
import { apiErrorMessage, apiErrorStatus } from '../utils/apiError';
import { editionPath, numericId, pagePath } from '../utils/paths';
import './CrossReferences.css';

const statuses: Record<CrossReferenceStatus, string> = {
  pending: 'На проверке',
  confirmed: 'Подтверждённые',
  rejected: 'Отклонённые',
};

const issues: Record<CrossReferenceIssue, { label: string; message: string }> = {
  edition_mismatch: { label: 'Другое издание', message: 'В отсылке указано другое издание.' },
  edition_uncertain: {
    label: 'Издание неясно',
    message: 'Издание, указанное в отсылке, не удалось установить.',
  },
  missing: { label: 'Нет полосы', message: 'Печатная страница отсутствует в этом собрании.' },
  ambiguous: { label: 'Несколько целей', message: 'Найдено несколько возможных полос.' },
  stale: { label: 'Устарела', message: 'Исходное место или его цель изменились.' },
};

function printedLocation(location: CrossReferenceLocation) {
  return location.printed_page > 0
    ? `Печатная страница ${location.printed_page} · полоса ${location.page_number}`
    : `Полоса ${location.page_number}`;
}

function workLabel(location: CrossReferenceLocation) {
  return [location.work_title, location.volume_part && `ч. ${location.volume_part}`]
    .filter(Boolean)
    .join(' · ');
}

function usePageText(location: CrossReferenceLocation | undefined) {
  return useQuery({
    queryKey: ['cross-reference-page', location?.work_id, location?.page_id, location?.page_number],
    queryFn: location
      ? async () => {
          const { data } = await readingApi.window(location.work_id, location.page_number, 1);
          const page = data.pages.find((item) => item.page_number === location.page_number);
          if (!page) throw new Error('В окне чтения отсутствует запрошенная полоса');
          return page.html + page.notes_html;
        }
      : skipToken,
    retry: false,
  });
}

// Номер полосы в якоре сноски может совпасть у разных томов.
function scopeFootnoteAnchors(html: string, scope: string): string {
  const template = document.createElement('template');
  template.innerHTML = html;
  for (const note of template.content.querySelectorAll('[id^="fn:"], [id^="fnref:"]')) {
    note.id = note.id.replace(':', `:${scope}-`);
  }
  for (const link of template.content.querySelectorAll('a[href^="#fn:"], a[href^="#fnref:"]')) {
    const href = link.getAttribute('href');
    if (href) link.setAttribute('href', href.replace(':', `:${scope}-`));
  }
  return template.innerHTML;
}

function PageComparison({
  heading,
  edition,
  location,
  text,
}: {
  heading: string;
  edition: Edition;
  location: CrossReferenceLocation;
  text: ReturnType<typeof usePageText>;
}) {
  const headingId = useId();
  const html = useMemo(
    () => (text.data ? scopeFootnoteAnchors(text.data, headingId) : ''),
    [headingId, text.data],
  );

  return (
    <section className="cross-references-page" aria-labelledby={headingId}>
      <h3 id={headingId}>{heading}</h3>
      <p className="cross-references-edition">{edition.title}</p>
      <div>
        <strong>{workLabel(location)}</strong>
        {location.closed && <ClosedBadge />}
      </div>
      <p>{printedLocation(location)}</p>
      <Link
        to={pagePath({ id: location.work_id }, location.page_number)}
        target="_blank"
        rel="noopener noreferrer"
      >
        Открыть полосу
      </Link>
      {text.isPending && <p role="status">Загрузка текста…</p>}
      {text.isError && (
        <div>
          <p role="alert">{apiErrorMessage(text.error, 'Не удалось загрузить текст полосы.')}</p>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={() => void text.refetch()}
          >
            Повторить
          </button>
        </div>
      )}
      {text.isSuccess &&
        (html.trim() ? (
          <div
            className="cross-references-page-text page-html-content"
            tabIndex={0}
            aria-label={`Текст полосы ${location.page_number}`}
            role="region"
            dangerouslySetInnerHTML={{ __html: html }}
          />
        ) : (
          <p className="cross-references-edition">У полосы пока нет текста.</p>
        ))}
    </section>
  );
}

function ReviewCandidate({
  candidate,
  edition,
  disabled,
  onDecide,
}: {
  candidate: CrossReferenceCandidate;
  edition: Edition;
  disabled: boolean;
  onDecide: (decision: CrossReferenceDecision) => void;
}) {
  const [chosenId, setChosenId] = useState<number | undefined>(candidate.target_page_id);
  const targets = candidate.targets ?? [];
  const target = targets.find((location) => location.page_id === chosenId);
  const sourceText = usePageText(candidate.source);
  const targetText = usePageText(target);
  const canConfirm =
    target &&
    sourceText.isSuccess &&
    targetText.isSuccess &&
    !sourceText.isFetching &&
    !targetText.isFetching;

  return (
    <section className="cross-references-review" aria-label="Сверка выбранной отсылки">
      <div className="cross-references-citation">
        <h2>{candidate.quote}</h2>
        <p>{candidate.context}</p>
        <p className="cross-references-edition">
          Том {candidate.volume} · с. {candidate.printed_page}
          {candidate.edition_number != null && ` · изд. ${candidate.edition_number}`}
        </p>
        {candidate.issue && (
          <p className="cross-references-issue">{issues[candidate.issue].message}</p>
        )}
        {candidate.status === 'pending' &&
          candidate.edition_number == null &&
          targets.length > 0 && (
            <p className="cross-references-edition">
              Номер издания в отсылке не указан. Сверьте источник по контексту.
            </p>
          )}
      </div>

      {targets.length > 0 && candidate.status !== 'confirmed' && (
        <label className="cross-references-target-picker">
          Возможная цель
          <select
            value={target?.page_id ?? ''}
            disabled={disabled}
            onChange={(event) => setChosenId(Number(event.target.value) || undefined)}
          >
            <option value="">Выбрать полосу…</option>
            {targets.map((location) => (
              <option key={location.page_id} value={location.page_id}>
                {workLabel(location)} · {printedLocation(location)}
                {location.closed && ' · закрыта'}
              </option>
            ))}
          </select>
        </label>
      )}

      <div className="cross-references-comparison">
        <PageComparison
          heading="Источник"
          edition={edition}
          location={candidate.source}
          text={sourceText}
        />
        {target ? (
          <PageComparison heading="Цель" edition={edition} location={target} text={targetText} />
        ) : (
          <section className="cross-references-page">
            <h3>Цель</h3>
            <p>
              {candidate.status === 'confirmed'
                ? 'Подтверждённая полоса недоступна.'
                : targets.length > 0
                  ? 'Выберите полосу для сравнения.'
                  : 'Нет доступной полосы для сравнения.'}
            </p>
          </section>
        )}
      </div>

      {candidate.status === 'pending' && (
        <div className="cross-references-actions">
          <button
            type="button"
            className="btn btn-primary"
            disabled={disabled || !canConfirm || candidate.issue === 'stale'}
            onClick={() =>
              target && onDecide({ decision: 'confirmed', target_page_id: target.page_id })
            }
          >
            Подтвердить
          </button>
          <button
            type="button"
            className="btn btn-secondary"
            disabled={disabled || candidate.issue === 'stale'}
            onClick={() => onDecide({ decision: 'rejected' })}
          >
            Отклонить
          </button>
        </div>
      )}
    </section>
  );
}

function VolumeQueue({ edition, workId }: { edition: Edition; workId: number }) {
  const [status, setStatus] = useState<CrossReferenceStatus>('pending');
  const [candidateId, setCandidateId] = useState<number>();
  const [reviewVersion, setReviewVersion] = useState(0);
  const [notice, setNotice] = useState('');
  const [failure, setFailure] = useState('');
  const client = useQueryClient();
  const queueKey = ['cross-references', edition.id, workId];
  const queue = useInfiniteQuery({
    queryKey: [...queueKey, status],
    initialPageParam: 0,
    queryFn: async ({ pageParam }) =>
      (await crossReferencesApi.list(edition.id, workId, status, pageParam)).data,
    getNextPageParam: (page) => page.next_after,
    retry: false,
  });
  const items = queue.data?.pages.flatMap((page) => page.items ?? []) ?? [];
  const candidate = items.find((item) => item.id === candidateId) ?? items[0];

  const scan = useMutation({
    mutationFn: () => crossReferencesApi.scan(edition.id, workId),
    onMutate: () => {
      setNotice('');
      setFailure('');
    },
    onSuccess: async ({ data }) => {
      await client.invalidateQueries({ queryKey: queueKey });
      setNotice(`Найдено ${data.detected}, добавлено ${data.added}.`);
    },
    onError: (err) => setFailure(apiErrorMessage(err, 'Не удалось найти отсылки.')),
  });
  const decision = useMutation({
    mutationFn: ({ id, data }: { id: number; data: CrossReferenceDecision }) =>
      crossReferencesApi.decide(edition.id, workId, id, data),
    onMutate: () => {
      setNotice('');
      setFailure('');
    },
    onSuccess: async (_response, { data }) => {
      await client.invalidateQueries({ queryKey: queueKey });
      setNotice(data.decision === 'confirmed' ? 'Отсылка подтверждена.' : 'Отсылка отклонена.');
    },
    onError: async (err) => {
      if (apiErrorStatus(err) === 409) {
        setReviewVersion((version) => version + 1);
        await Promise.all([
          client.invalidateQueries({ queryKey: queueKey }),
          client.invalidateQueries({ queryKey: ['cross-reference-page'] }),
        ]);
        setFailure('Решение не сохранено. Отсылка изменилась или уже рассмотрена.');
      } else {
        setFailure(apiErrorMessage(err, 'Не удалось сохранить решение.'));
      }
    },
  });

  const busy = scan.isPending || decision.isPending;

  return (
    <>
      <div className="cross-references-toolbar">
        <label>
          Статус
          <select
            value={status}
            disabled={busy}
            onChange={(event) => {
              setStatus(event.target.value as CrossReferenceStatus);
              setCandidateId(undefined);
              setFailure('');
            }}
          >
            {Object.entries(statuses).map(([key, label]) => (
              <option key={key} value={key}>
                {label}
              </option>
            ))}
          </select>
        </label>
        <button
          type="button"
          className="btn btn-secondary"
          disabled={busy}
          onClick={() => scan.mutate()}
        >
          {scan.isPending ? 'Идёт поиск…' : 'Найти отсылки'}
        </button>
      </div>
      {notice && <p role="status">{notice}</p>}
      {failure && (
        <p className="cross-references-error" role="alert">
          {failure}
        </p>
      )}
      {queue.isPending && <p role="status">Загрузка отсылок…</p>}
      {queue.isError && (
        <div>
          <p className="cross-references-error" role="alert">
            {apiErrorMessage(queue.error, 'Не удалось загрузить отсылки.')}
          </p>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            disabled={queue.isFetching}
            onClick={() => void queue.refetch()}
          >
            Повторить
          </button>
        </div>
      )}
      {!queue.isPending && !queue.isError && (
        <div className="cross-references-workspace">
          <div>
            {items.length > 0 ? (
              <ul className="cross-references-queue" aria-label="Отсылки">
                {items.map((item) => (
                  <li key={item.id}>
                    <button
                      type="button"
                      className="cross-references-row"
                      aria-pressed={candidate?.id === item.id}
                      disabled={busy}
                      onClick={() => {
                        setCandidateId(item.id);
                        setFailure('');
                      }}
                    >
                      <strong>{item.quote}</strong>
                      <span>{printedLocation(item.source)}</span>
                      {item.issue && (
                        <span className="cross-references-row-issue">
                          {issues[item.issue].label}
                        </span>
                      )}
                    </button>
                  </li>
                ))}
              </ul>
            ) : !queue.hasNextPage ? (
              <p>Нет отсылок с этим статусом.</p>
            ) : null}
            {queue.hasNextPage && (
              <button
                type="button"
                className="btn btn-secondary btn-sm"
                disabled={busy || queue.isFetching}
                onClick={() => void queue.fetchNextPage()}
              >
                {queue.isFetchingNextPage ? 'Загрузка…' : 'Показать ещё'}
              </button>
            )}
          </div>
          {candidate && (
            <ReviewCandidate
              key={`${candidate.id}-${reviewVersion}`}
              candidate={candidate}
              edition={edition}
              disabled={busy || queue.isFetching}
              onDecide={(data) => decision.mutate({ id: candidate.id, data })}
            />
          )}
        </div>
      )}
    </>
  );
}

function EditionQueue({ edition, volumes }: { edition: Edition; volumes: VolumeSummary[] }) {
  const [chosenWork, setChosenWork] = useState<number>();
  const work = volumes.find((volume) => volume.id === chosenWork) ?? volumes[0];

  if (!edition.cross_references_enabled) {
    return <p>Сверка отсылок для этого собрания недоступна.</p>;
  }

  if (!work) {
    return <p>В собрании пока нет томов для сверки.</p>;
  }

  return (
    <>
      <label className="cross-references-volume-picker">
        Исходный том
        <select value={work.id} onChange={(event) => setChosenWork(Number(event.target.value))}>
          {volumes.map((volume) => (
            <option key={volume.id} value={volume.id}>
              {volume.title}
              {(volume.closed || volume.closed_by) && ' · закрыт'}
            </option>
          ))}
        </select>
      </label>
      <VolumeQueue key={work.id} edition={edition} workId={work.id} />
    </>
  );
}

export function CrossReferences() {
  const { id: idParam } = useParams<{ id: string }>();
  const id = numericId(idParam);
  const query = useQuery({
    queryKey: ['cross-reference-edition', id],
    queryFn:
      id === null
        ? skipToken
        : async () => {
            const { data: edition } = await editionsApi.get(id);
            if (!edition.cross_references_enabled) return { edition, volumes: [] };
            const { data: volumes } = await editionsApi.volumes(id);
            return {
              edition,
              volumes: (volumes ?? []).filter(
                (volume) => volume.role === 'volume' && volume.volume_number != null,
              ),
            };
          },
    retry: false,
  });
  const edition = query.data?.edition;

  useDocumentTitle(edition ? `Сверка отсылок · ${edition.title}` : 'Сверка отсылок');

  return (
    <div className="cross-references">
      <Link
        className="back-link"
        to={edition ? editionPath(edition) : id === null ? '/' : editionPath({ id })}
      >
        ← {id === null ? 'В читальню' : 'К собранию'}
      </Link>
      <h1>Сверка отсылок</h1>
      {id === null ? (
        <p role="alert">Собрание не найдено.</p>
      ) : query.isPending ? (
        <p role="status">Загрузка собрания…</p>
      ) : query.isError ? (
        <div>
          <p role="alert" className="cross-references-error">
            {apiErrorMessage(query.error, 'Не удалось загрузить собрание.')}
          </p>
          <button
            type="button"
            className="btn btn-secondary btn-sm"
            onClick={() => void query.refetch()}
          >
            Повторить
          </button>
        </div>
      ) : (
        <>
          <p className="cross-references-edition-name">
            {query.data.edition.title} {query.data.edition.closed && <ClosedBadge />}
          </p>
          <EditionQueue key={query.data.edition.id} {...query.data} />
        </>
      )}
    </div>
  );
}
