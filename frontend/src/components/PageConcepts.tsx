import { useState } from 'react';
import { Link } from 'react-router-dom';
import { usePageConcepts } from '../hooks/usePageConcepts';
import { groupBySlug, pagesLabel, rubricLabel } from '../utils/pageConceptGroups';
import './PageConcepts.css';

interface PageConceptsProps {
  workId: number | undefined;
  pageId: number | undefined;
}

// Замер на будущем ленинском корпусе: медиана — 13 понятий на полосу, у 7037
// полос больше двадцати, рекорд — 96. Сервер отдаёт всё; потолок только
// экранный, иначе счётчик «ещё N» врал бы.
const SHOWN = 5;

export const PageConcepts: React.FC<PageConceptsProps> = ({ workId, pageId }) => {
  const { backlinks, isLoading, failed } = usePageConcepts(workId, pageId);
  const [expanded, setExpanded] = useState(false);

  // Компонент не перемонтируется при листании тома — меняются только
  // пропсы, — а useState сам по себе от них не зависит. Без этого сброса
  // раскрытие с предыдущей полосы наследовалось бы следующей. Тот же приём,
  // что в usePageConcepts: сравнение ключа запроса прямо в теле рендера,
  // без эффекта — react-hooks 7 не допускает setState внутри эффекта.
  const requestKey = `${workId ?? ''}|${pageId ?? ''}`;
  const [prevRequestKey, setPrevRequestKey] = useState(requestKey);
  if (prevRequestKey !== requestKey) {
    setPrevRequestKey(requestKey);
    setExpanded(false);
  }

  // Пустой список — состояние всех работ без координат тома, то есть сегодня
  // почти всех. Заголовок «Понятия указателя (0)» на каждой странице проекта
  // не нужен никому.
  if (isLoading || (!failed && backlinks.length === 0)) return null;

  const groups = groupBySlug(backlinks);
  const shown = expanded ? groups : groups.slice(0, SHOWN);
  const hidden = groups.length - shown.length;

  return (
    <div className="page-concepts">
      {/* Счётчик рядом с признанием «не знаю» читается как «нашли 0» —
          в ветке ошибки заголовок его не показывает. Число здесь — счёт
          ВСЕХ понятий полосы, экранный потолок его не касается. */}
      <h2>Понятия указателя{failed ? '' : ` (${groups.length})`}</h2>

      {failed ? (
        <p className="page-concepts-error">Не удалось загрузить понятия этой страницы</p>
      ) : (
        <>
          <ul className="page-concepts-list">
            {shown.map((group) => (
              <li key={group.slug}>
                <Link to={`/concepts/${group.slug}`}>{group.title}</Link>
                <ul className="page-concepts-rubrics">
                  {group.entries.map((entry, i) => (
                    <li key={`${entry.concept_id}-${entry.rubric}-${i}`}>
                      {rubricLabel(entry)} · {pagesLabel(entry)}
                    </li>
                  ))}
                </ul>
              </li>
            ))}
          </ul>
          {hidden > 0 && (
            <button type="button" className="page-concepts-more" onClick={() => setExpanded(true)}>
              ещё {hidden}
            </button>
          )}
        </>
      )}
    </div>
  );
};
