import { ClosedBadge } from './ClosedBadge';
import type { EventOrganizationSummary } from '../types';
import type { HomeLayout } from '../utils/homeLayout';
import { organizationAnchor, splitAuthor } from '../utils/homeLayout';
import { volumeCountLabel } from '../utils/editionCaption';
import { plural } from '../utils/volumeLabel';
import './HomeToc.css';

interface Row {
  key: string;
  href: string;
  author: string;
  rest?: string;
  count?: string;
  closed?: boolean;
}

interface Column {
  key: string;
  title: string;
  rows: Row[];
}

const events = (n: number) => plural(n, ['событие', 'события', 'событий']);

const orgRow = (o: EventOrganizationSummary): Row => ({
  key: `org-${o.id}`,
  href: `#${organizationAnchor(o)}`,
  author: o.title,
  count: events(o.events_total),
});

/**
 * Оглавление главной: всё, что стоит на странице ниже, названо на первом
 * экране, и до любого собрания — один щелчок. Колонки идут в том же порядке,
 * что и блоки страницы, потому что строятся из той же раскладки.
 */
export function HomeToc({ layout }: { layout: HomeLayout }) {
  const columns: Column[] = layout.sections.map((section) => ({
    key: section.anchor,
    title: section.name,
    rows: [
      ...section.shelves.map(({ edition, volumes, anchor, shortTitle }) => {
        const { author, rest } = splitAuthor(shortTitle);
        return {
          key: `ed-${edition.id}`,
          href: `#${anchor}`,
          author,
          rest,
          count: volumeCountLabel(volumes),
          closed: edition.closed,
        };
      }),
      ...section.organizations.map(orgRow),
    ],
  }));
  if (layout.journals.length > 0) {
    columns.push({
      key: 'zhurnaly',
      title: 'Журналы',
      rows: layout.journals.map((j) => ({
        key: `j-${j.id}`,
        href: '#zhurnaly',
        author: j.title,
        count: plural(j.issues_total, ['номер', 'номера', 'номеров']),
        closed: j.closed,
      })),
    });
  }
  if (layout.looseOrganizations.length > 0) {
    columns.push({
      key: 'sezdy',
      title: 'Съезды и конгрессы',
      rows: layout.looseOrganizations.map(orgRow),
    });
  }
  if (layout.looseWorks.length > 0) {
    columns.push({
      key: 'vne-sobraniy',
      title: 'Вне собраний',
      rows: layout.looseWorks.map((w) => ({
        key: `w-${w.id}`,
        href: '#vne-sobraniy',
        author: w.title,
      })),
    });
  }
  if (columns.length === 0) return null;

  return (
    <nav className="home-toc" aria-label="Оглавление читальни">
      {columns.map((column) => (
        <div className="home-toc-col" key={column.key}>
          {/* Не заголовок: оглавление стоит выше разделов, и те же имена
              заголовками ниже обход по заголовкам прошёл бы дважды. */}
          <p className="home-toc-title" id={`toc-${column.key}`}>
            {column.title}
          </p>
          <ul aria-labelledby={`toc-${column.key}`}>
            {column.rows.map((row) => (
              <li key={row.key}>
                <a href={row.href}>
                  <b>{row.author}</b>
                  {row.rest && (
                    <>
                      {' '}
                      <span className="home-toc-rest">{row.rest}</span>
                    </>
                  )}
                </a>
                {row.closed && <ClosedBadge />}
                {row.count && <span className="home-toc-count">{row.count}</span>}
              </li>
            ))}
          </ul>
        </div>
      ))}
    </nav>
  );
}
