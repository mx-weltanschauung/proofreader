import { useEffect, useState } from 'react';
import { Link, useParams } from 'react-router-dom';
import toast from 'react-hot-toast';
import { personsApi } from '../services/api';
import type { Person, PersonDetail } from '../types';
import { useAuth, canEdit as canEditUser } from '../hooks/useAuth';
import { useDocumentTitle } from '../hooks/useDocumentTitle';
import { apiErrorMessage } from '../utils/apiError';
import { articleKindLabel } from '../utils/credits';
import { chapterPath, journalPath } from '../utils/paths';
import './AuthorDetail.css';

export function AuthorDetail() {
  const { slug = '' } = useParams<{ slug: string }>();
  const { user } = useAuth();
  const canEdit = canEditUser(user);
  const [detail, setDetail] = useState<PersonDetail | null>(null);
  const [error, setError] = useState('');
  const [query, setQuery] = useState('');
  const [found, setFound] = useState<Person[]>([]);
  const [name, setName] = useState('');
  useDocumentTitle(detail?.person.name);

  const [reloadKey, setReloadKey] = useState(0);
  const reload = () => setReloadKey((k) => k + 1);

  useEffect(() => {
    let alive = true;
    const load = async () => {
      try {
        const { data } = await personsApi.get(slug);
        if (!alive) return;
        setDetail(data);
        setName(data.person.name);
        setError('');
      } catch (err: unknown) {
        if (alive) setError(apiErrorMessage(err, 'Автор не найден'));
      }
    };
    void load();
    return () => {
      alive = false;
    };
  }, [slug, reloadKey]);

  const searching = canEdit && query.trim().length >= 2;
  useEffect(() => {
    if (!searching) return;
    let alive = true;
    void personsApi.search(query).then(
      ({ data }) => alive && setFound(data),
      () => alive && setFound([]),
    );
    return () => {
      alive = false;
    };
  }, [query, searching]);
  const candidates = searching ? found.filter((p) => p.id !== detail?.person.id) : [];

  const mergeHere = async (other: Person) => {
    if (!detail) return;
    if (
      !window.confirm(`Слить «${other.name}» в «${detail.person.name}»? Его подписи переедут сюда.`)
    )
      return;
    try {
      await personsApi.merge(detail.person.id, other.id);
      toast.success('Авторы слиты');
      setQuery('');
      reload();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось слить авторов'));
    }
  };

  const rename = async () => {
    if (!detail) return;
    const next = name.trim();
    if (!next || next === detail.person.name) return;
    try {
      await personsApi.update(detail.person.id, { name: next });
      toast.success('Имя сохранено');
      reload();
    } catch (err: unknown) {
      toast.error(apiErrorMessage(err, 'Не удалось сохранить имя'));
    }
  };

  if (error) {
    return (
      <div className="error-state">
        <div>{error}</div>
        <Link to="/" className="back-link">
          ← В читальню
        </Link>
      </div>
    );
  }
  if (!detail) return <div className="loading-state">Загрузка автора…</div>;

  // Группы: журнал -> статьи в порядке ответа (сервер уже сортирует).
  const groups: { slug: string; title: string; items: typeof detail.articles }[] = [];
  for (const a of detail.articles) {
    const last = groups[groups.length - 1];
    if (last && last.slug === a.journal_slug) last.items.push(a);
    else groups.push({ slug: a.journal_slug, title: a.journal_title, items: [a] });
  }

  return (
    <div className="author-page">
      <Link to="/" className="back-link">
        ← В читальню
      </Link>
      <h1 className="author-name">{detail.person.name}</h1>
      {groups.length === 0 && <div className="empty-state">Статей в читальне нет</div>}
      {groups.map((g) => (
        <section key={g.slug} className="author-journal">
          <h2 className="author-journal-title">
            <Link to={journalPath({ slug: g.slug })}>{g.title}</Link>
          </h2>
          <ul className="author-articles">
            {g.items.map((a) => (
              <li key={`${a.chapter_id}-${a.role}`} className="author-article">
                <Link
                  to={chapterPath(
                    { id: a.work_id, slug: a.work_slug },
                    { id: a.chapter_id, slug: a.chapter_slug },
                  )}
                >
                  {a.title}
                </Link>
                <span className="author-article-meta">
                  {[
                    // Статья на одной полосе — «с. 5», а не «с. 5—5».
                    `${a.year}, № ${a.label}, с. ${
                      a.start_page === a.end_page ? a.start_page : `${a.start_page}—${a.end_page}`
                    }`,
                    articleKindLabel(a.article_kind),
                    a.role === 'translator' ? 'перевод' : '',
                  ]
                    .filter(Boolean)
                    .join(' · ')}
                </span>
              </li>
            ))}
          </ul>
        </section>
      ))}
      {canEdit && (
        <section className="author-edit">
          <div className="author-field">
            <label htmlFor="author-name-input">Имя автора</label>
            <input id="author-name-input" value={name} onChange={(e) => setName(e.target.value)} />
            <button
              type="button"
              className="btn btn-secondary btn-sm"
              onClick={() => void rename()}
            >
              Сохранить имя
            </button>
          </div>
          <div className="author-field">
            <label htmlFor="author-merge-query">Найти автора для слияния</label>
            <input
              id="author-merge-query"
              value={query}
              onChange={(e) => setQuery(e.target.value)}
            />
          </div>
          <ul className="author-merge-results">
            {candidates.map((p) => (
              <li key={p.id}>
                <button
                  type="button"
                  className="btn btn-secondary btn-sm"
                  onClick={() => void mergeHere(p)}
                >
                  Слить «{p.name}» сюда
                </button>
              </li>
            ))}
          </ul>
        </section>
      )}
    </div>
  );
}
