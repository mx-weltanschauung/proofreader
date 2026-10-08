import { Fragment, useRef } from 'react';

import type { ConceptOrder } from '../types';
import type { ConceptFragmentsResult } from '../hooks/useConceptFragments';
import { ConceptFragmentBlock } from './ConceptFragmentBlock';
import { useInfiniteSentinel } from '../hooks/useInfiniteSentinel';
import { changedLevels } from '../utils/conceptEntries';
import './ConceptFragmentStream.css';

interface ConceptFragmentStreamProps {
  stream: ConceptFragmentsResult;
  slug: string;
  order: ConceptOrder;
  onOrderChange: (order: ConceptOrder) => void;
}

/**
 * Поток записей понятия: записи подряд, следующая порция подгружается, когда
 * сентинел под последней записью попадает в кадр.
 */
export const ConceptFragmentStream: React.FC<ConceptFragmentStreamProps> = ({
  stream,
  slug,
  order,
  onOrderChange,
}) => {
  const sentinelRef = useRef<HTMLDivElement>(null);
  const { hasMore, loadMore } = stream;

  useInfiniteSentinel(sentinelRef, hasMore, loadMore);

  // Тумблер стоит над потоком, а не в блоке фильтров панели: фильтры сужают и
  // панель, и поток, а порядок касается только потока.
  const orderToggle = (
    <div className="concept-stream-order" role="group" aria-label="Порядок фрагментов">
      <span className="concept-stream-order-label">порядок:</span>
      <button
        type="button"
        aria-pressed={order === 'rubric'}
        disabled={order === 'rubric'}
        onClick={() => onOrderChange('rubric')}
      >
        по рубрикам
      </button>
      <button
        type="button"
        aria-pressed={order === 'page'}
        disabled={order === 'page'}
        onClick={() => onOrderChange('page')}
      >
        по томам
      </button>
    </div>
  );

  if (stream.isLoading) {
    return (
      <div className="concept-stream">
        {orderToggle}
        <div className="concept-stream-note">Загружаем страницы понятия…</div>
      </div>
    );
  }

  return (
    <div className="concept-stream">
      {orderToggle}
      {stream.entries.length === 0 && stream.error === '' && (
        <p className="concept-stream-note">
          Ни один том с этими адресами пока не загружен — адреса перечислены в статье указателя.
        </p>
      )}

      {stream.entries.map((entry, i) => {
        // Заголовки рисуются на смене ЛЮБОГО уровня пути, каждый своим
        // уровнем разметки: длинное имя съезда (до 110 знаков) печатается раз
        // на раздел, а не в каждом из 35 заголовков аспектов.
        const headings =
          order === 'rubric'
            ? changedLevels(entry.rubric_path, stream.entries[i - 1]?.rubric_path)
            : [];

        return (
          <Fragment key={entry.reference_id}>
            {headings.map((heading) =>
              heading.level === 0 ? (
                <h2 key={heading.key} className="concept-stream-rubric">
                  {heading.title}
                </h2>
              ) : (
                <h3 key={heading.key} className="concept-stream-subrubric">
                  {heading.title}
                </h3>
              ),
            )}
            <ConceptFragmentBlock
              entry={entry}
              slug={slug}
              replaceEntry={stream.replaceEntry}
              showRubric={order === 'page'}
            />
          </Fragment>
        );
      })}

      {stream.isLoadingMore && <div className="concept-stream-note">Загружаем ещё…</div>}

      {stream.error !== '' && (
        <div className="concept-stream-error">
          <span>{stream.error}</span>{' '}
          <button type="button" className="concept-stream-retry" onClick={stream.retry}>
            повторить
          </button>
        </div>
      )}

      {/* Сентинел живёт вне ветки hasMore, чтобы наблюдатель находил его сразу
          после дорисовки порции, а не на следующем кадре. */}
      <div ref={sentinelRef} className="concept-stream-sentinel" aria-hidden="true" />
    </div>
  );
};
