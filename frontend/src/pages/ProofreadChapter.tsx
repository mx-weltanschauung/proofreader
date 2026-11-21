import { useCallback, useEffect, useRef, useState } from 'react';
import { Link, Navigate, useLocation, useNavigate, useParams } from 'react-router-dom';
import { isAxiosError } from 'axios';
import {
  API_BASE_URL,
  proofreadAPI,
  type ProofreadDecision,
  type ProofreadFinding,
  type ProofreadPage,
  type ProofreadRun,
} from '../services/api';
import { MarkdownDiff } from '../components/MarkdownDiff';
import { ProofreadScan } from '../components/ProofreadScan';
import { useAuth } from '../hooks/useAuth';
import { joinPathFor } from '../utils/returnUrl';
import { chapterPath, pagePath } from '../utils/paths';
import './ProofreadChapter.css';

function proposal(page: ProofreadPage) {
  const runes = Array.from(page.base_markdown);
  const accepted = (page.findings ?? [])
    .filter((f) => f.decision === 'accepted')
    .sort((a, b) => a.start_rune - b.start_rune || a.end_rune - b.end_rune);
  let end = 0,
    previousStart = -1,
    previousEnd = -1;
  const chunks: string[] = [];
  for (const f of accepted) {
    if (f.start_rune < end || (previousStart === f.start_rune && previousEnd === f.end_rune))
      return null;
    chunks.push(runes.slice(end, f.start_rune).join(''), f.replacement);
    end = f.end_rune;
    previousStart = f.start_rune;
    previousEnd = end;
  }
  return chunks.join('') + runes.slice(end).join('');
}
const outcomes: Record<string, string> = {
  submitted: 'Предложение отправлено',
  already_submitted: 'Предложение уже отправлено',
  rate_limited: 'Достигнут часовой лимит. Повторите отправку позже.',
  refresh_required: 'Решения изменились. Проверьте обновлённые правки перед повторной отправкой.',
  stale: 'Исходный текст изменился. Нужна новая сверка.',
  failed: 'Не удалось отправить. Проверьте правки и повторите.',
};
function pageStatus(p: ProofreadPage, result?: string) {
  if (p.skip_reason) return p.skip_reason;
  if (p.submitted_at) return 'Предложение отправлено';
  if (p.stale) return outcomes.stale;
  if (result && outcomes[result]) return outcomes[result];
  if (p.analysis_status === 'failed') return 'Ошибка анализа';
  if (p.analysis_status === 'pending') return 'Ожидает анализа';
  if (!p.findings?.length) return 'Находок нет';
  const pending = p.findings.filter((f) => f.decision === 'pending').length;
  return pending ? `Ждут решения: ${pending}` : 'Находки разобраны';
}

function Review({ id }: { id: number }) {
  const [run, setRun] = useState<ProofreadRun | null>(null);
  const [pageID, select] = useState<number>();
  const [active, setActive] = useState<number>();
  const [editing, setEditing] = useState<number>();
  const [draft, setDraft] = useState('');
  const acting = useRef(false);
  const [busy, setBusy] = useState(false);
  const [error, setError] = useState('');
  const [code, setCode] = useState('');
  const [results, setResults] = useState<Record<number, string>>({});
  const mark = useRef<HTMLElement>(null);
  const navigate = useNavigate();
  const reload = useCallback(async () => {
    try {
      const fresh = await proofreadAPI.get(id);
      setRun(fresh);
    } catch (e) {
      if (isAxiosError(e) && [403, 404].includes(e.response?.status ?? 0)) setRun(null);
      throw e;
    }
  }, [id]);
  useEffect(() => {
    let live = true;
    proofreadAPI
      .get(id)
      .then((fresh) => {
        if (live) setRun(fresh);
      })
      .catch(() => {
        if (live) setError('Не удалось открыть работу. Проверьте доступ к главе.');
      });
    return () => {
      live = false;
    };
  }, [id]);
  const page = run?.pages.find((p) => p.page_id === pageID) ?? run?.pages[0];
  const findings = page?.findings ?? [];
  const selected = findings.find((f) => f.id === active);
  useEffect(() => {
    mark.current?.scrollIntoView?.({ block: 'nearest' });
  }, [active]);
  async function act(fn: () => Promise<void>) {
    if (acting.current) return;
    acting.current = true;
    setBusy(true);
    setError('');
    try {
      await fn();
    } catch (e) {
      setError(
        isAxiosError(e) && e.response?.status === 429
          ? 'Достигнут часовой лимит. Повторите отправку позже.'
          : 'Не удалось выполнить действие. Обновите работу и проверьте сохранённые решения.',
      );
    } finally {
      acting.current = false;
      setBusy(false);
    }
  }
  async function submitReady() {
    try {
      const pages = ready.map((p) => ({
        page_id: p.page_id,
        accepted: Object.fromEntries(
          (p.findings ?? [])
            .filter((f) => f.decision === 'accepted')
            .map((f) => [f.id, f.replacement]),
        ),
      }));
      const response = await proofreadAPI.submit(id, pages);
      setResults(Object.fromEntries(response.pages.map((p) => [p.page_id, p.status])));
    } catch (e) {
      if (!isAxiosError(e) || e.response) throw e;
      await reload();
      setError('Ответ на отправку не получен. Состояния полос обновлены по данным сервера.');
      return;
    }
    await reload();
  }
  async function decide(f: ProofreadFinding, decision: ProofreadDecision, replacement: string) {
    await act(async () => {
      await proofreadAPI.decide(id, f.id, decision, replacement);
      setRun(
        (prev) =>
          prev && {
            ...prev,
            pages: prev.pages.map((p) =>
              p.page_id !== page?.page_id
                ? p
                : {
                    ...p,
                    findings:
                      p.findings?.map((item) =>
                        item.id === f.id ? { ...item, decision, replacement } : item,
                      ) ?? null,
                  },
            ),
          },
      );
      setEditing(undefined);
    });
  }
  const ready = (run?.pages ?? []).filter(
    (p) =>
      !p.skip_reason &&
      !p.submitted_at &&
      !p.stale &&
      !['submitted', 'already_submitted', 'stale'].includes(results[p.page_id]) &&
      p.analysis_status === 'analyzed' &&
      p.findings?.some((f) => f.decision === 'accepted') &&
      p.findings.every((f) => f.decision !== 'pending') &&
      proposal(p) !== null,
  );
  const original = Array.from(page?.base_markdown ?? '');
  const proposed = page && proposal(page);
  const locked =
    !!page?.submitted_at ||
    !!page?.stale ||
    ['stale', 'submitted', 'already_submitted'].includes(results[page?.page_id ?? 0]);
  return (
    <main className="proof-review">
      <h1>Проверка находок по главе</h1>
      {run && (
        <Link to={chapterPath({ id: run.work_id }, { id: run.chapter_id })}>Вернуться к главе</Link>
      )}
      {error && <p role="alert">{error}</p>}
      <button
        disabled={busy || (!run && !error) || editing !== undefined}
        onClick={() => act(reload)}
      >
        Обновить работу
      </button>
      {run && (
        <>
          <details className="proof-helper">
            <summary>Подключить помощник для анализа</summary>
            <p>
              Запустите установленный proofread-assist на своём компьютере. Получите одноразовый код
              и выполните команду подключения.
            </p>
            <button
              disabled={busy}
              onClick={() => act(async () => setCode((await proofreadAPI.pair(id)).code))}
            >
              Получить одноразовый код
            </button>
            {code && (
              <pre>
                proofread-assist pair {code} --site-url {API_BASE_URL || window.location.origin}
              </pre>
            )}
            <p>
              Укажите адрес сервера модели и её имя. Повторите команду, чтобы продолжить
              незавершённый анализ, затем обновите работу.
            </p>
            <pre>proofread-assist run {id} --model-url URL --model MODEL</pre>
          </details>
          <p>
            Анализ завершён для{' '}
            {run.pages.filter((p) => p.analysis_status === 'analyzed' && !p.skip_reason).length} из{' '}
            {run.pages.length} полос.
          </p>
          <div className="proof-layout">
            <nav aria-label="Полосы главы">
              <h2>Полосы</h2>
              {run.pages.map((p) => (
                <button
                  key={p.page_id}
                  disabled={busy || editing !== undefined}
                  aria-current={p === page ? 'page' : undefined}
                  onClick={() => {
                    select(p.page_id);
                    setActive(undefined);
                  }}
                >
                  <strong>Полоса {p.page_number}</strong>
                  <span>{pageStatus(p, results[p.page_id])}</span>
                  <small>Находок: {p.findings?.length ?? 0}</small>
                </button>
              ))}
            </nav>
            {page &&
              (page.skip_reason ? (
                <p role="status">Пропущена · {page.skip_reason}</p>
              ) : (
                <>
                  {page.scan_url && (
                    <ProofreadScan
                      key={page.page_id + '-' + page.scan_url}
                      number={page.page_number}
                      url={page.scan_url}
                      box={selected?.scan_bbox}
                      refreshDisabled={busy || editing !== undefined}
                      refresh={() => act(reload)}
                    />
                  )}
                  <section className="proof-text">
                    <h2>Текст полосы {page.page_number}</h2>
                    <p>{pageStatus(page, results[page.page_id])}</p>
                    {page.analysis_error && <p>{page.analysis_error}</p>}
                    {!page.submitted_at && (page.stale || results[page.page_id] === 'stale') && (
                      <>
                        <Link to={pagePath({ id: run.work_id }, page.page_number)}>
                          Посмотреть текущий текст полосы
                        </Link>
                        <button
                          disabled={busy}
                          onClick={() =>
                            act(async () => {
                              const next = await proofreadAPI.create(run.work_id, run.chapter_id);
                              navigate(`/proofread/runs/${next.id}`);
                            })
                          }
                        >
                          Начать новую сверку главы
                        </button>
                      </>
                    )}
                    <h3>Исходный Markdown</h3>
                    <pre>
                      {selected ? (
                        <>
                          {original.slice(0, selected.start_rune).join('')}
                          <mark ref={mark}>{selected.old || '▏'}</mark>
                          {original.slice(selected.end_rune).join('')}
                        </>
                      ) : (
                        page.base_markdown
                      )}
                    </pre>
                    <h3>Предлагаемый Markdown</h3>
                    {proposed === null ? (
                      <p role="alert">Принятые правки пересекаются. Отклоните одну из них.</p>
                    ) : (
                      <>
                        <pre aria-label="Предлагаемый Markdown">{proposed}</pre>
                        {proposed !== page.base_markdown && (
                          <MarkdownDiff before={page.base_markdown} after={proposed ?? ''} />
                        )}
                      </>
                    )}
                    <h3>Предложенные правки</h3>
                    {findings.map((f) => (
                      <article key={f.id}>
                        <button aria-pressed={active === f.id} onClick={() => setActive(f.id)}>
                          {f.old || 'Вставка'} → {f.new || 'Удаление'}
                        </button>
                        <p>{f.reason}</p>
                        <small>
                          {f.source} ·{' '}
                          {f.scan_bbox ? 'Есть координаты на скане' : 'Без точной привязки к скану'}{' '}
                          ·{' '}
                          {
                            { pending: 'Ждёт решения', accepted: 'Принята', rejected: 'Отклонена' }[
                              f.decision
                            ]
                          }
                        </small>
                        {editing === f.id ? (
                          <form
                            onSubmit={(e) => {
                              e.preventDefault();
                              void decide(f, 'accepted', draft);
                            }}
                          >
                            <label>
                              Текст замены
                              <textarea value={draft} onChange={(e) => setDraft(e.target.value)} />
                            </label>
                            <button disabled={busy}>Сохранить и принять</button>
                            <button
                              type="button"
                              disabled={busy}
                              onClick={() => setEditing(undefined)}
                            >
                              Отмена
                            </button>
                          </form>
                        ) : (
                          <fieldset disabled={busy || locked || editing !== undefined}>
                            <button
                              onClick={() =>
                                decide(
                                  f,
                                  'accepted',
                                  f.decision === 'accepted' ? f.replacement : f.new,
                                )
                              }
                            >
                              Принять
                            </button>
                            <button onClick={() => decide(f, 'rejected', f.new)}>Отклонить</button>
                            <button
                              onClick={() => {
                                setActive(f.id);
                                setEditing(f.id);
                                setDraft(f.decision === 'accepted' ? f.replacement : f.new);
                              }}
                            >
                              Изменить
                            </button>
                          </fieldset>
                        )}
                      </article>
                    ))}
                  </section>
                </>
              ))}
          </div>
          <section className="proof-batch">
            <h2>Отправка редактору</h2>
            <p>
              Готовы к отправке: {ready.length}. В предложение войдут принятые правки каждой полосы.
            </p>
            {ready.map((p) => (
              <details open key={p.page_id}>
                <summary>Итоговые изменения · полоса {p.page_number}</summary>
                <pre>{proposal(p)}</pre>
                <MarkdownDiff before={p.base_markdown} after={proposal(p) ?? ''} />
              </details>
            ))}
            <button
              className="btn btn-primary"
              disabled={!ready.length || busy || editing !== undefined}
              onClick={() => act(submitReady)}
            >
              Отправить проверенные полосы
            </button>
          </section>
        </>
      )}
    </main>
  );
}
export default function ProofreadChapter() {
  const { runId } = useParams();
  const user = useAuth((s) => s.user);
  const location = useLocation();
  if (!user) return <Navigate replace to={joinPathFor(location.pathname, location.search)} />;
  if (user.role !== 'reader') return <p>Для проверки нужен аккаунт читателя.</p>;
  return <Review key={user.id + '-' + runId} id={Number(runId)} />;
}
