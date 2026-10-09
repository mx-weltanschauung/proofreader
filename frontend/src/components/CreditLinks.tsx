import { Fragment } from 'react';
import { Link } from 'react-router-dom';
import type { ArticleCredit } from '../types';
import { authorPath } from '../utils/paths';

/** Подпись статьи: авторы через запятую (с человеком — ссылкой), затем «пер. …». */
export function CreditLinks({ credits }: { credits: ArticleCredit[] }) {
  const sorted = [...credits].sort((a, b) => a.position - b.position);
  const authors = sorted.filter((c) => c.role === 'author');
  const translators = sorted.filter((c) => c.role === 'translator');
  const name = (c: ArticleCredit) =>
    c.person_slug ? <Link to={authorPath({ slug: c.person_slug })}>{c.printed}</Link> : c.printed;
  return (
    <span className="credit-links">
      {authors.map((c, i) => (
        <Fragment key={c.position}>
          {i > 0 && ', '}
          {name(c)}
        </Fragment>
      ))}
      {translators.length > 0 && (
        <span className="credit-links-translators">
          {authors.length > 0 && '; '}пер.{' '}
          {translators.map((c, i) => (
            <Fragment key={c.position}>
              {i > 0 && ', '}
              {name(c)}
            </Fragment>
          ))}
        </span>
      )}
    </span>
  );
}
