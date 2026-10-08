import { Link } from 'react-router-dom';

import type { Concept } from '../types';
import { ConceptArticleBlock } from './ConceptArticleBlock';
import './ConceptAddressPanel.css';

interface ConceptAddressPanelProps {
  concept: Concept;
  rubric: string;
  rubricPath: string[];
  volume: string;
  onFilterChange: (next: { rubric: string; rubricPath: string[]; volume: string }) => void;
  /**
   * Идентификаторы адресов (`reference_id`), уже пришедших в поток. Определяет,
   * ведёт ли разрешённый адрес на якорь записи внутри документа или на
   * страницу тома — иначе клик по адресу вне загруженного окна упирался бы в
   * несуществующий якорь.
   */
  loadedRefs: ReadonlySet<number>;
}

/**
 * Левая панель понятия: блок на каждую статью (издание, адреса, печатный
 * текст, исходящие отсылки) и входящие отсылки понятия — те каталожные, не
 * статейные, поэтому рисуются здесь, а не в блоке статьи.
 */
export const ConceptAddressPanel: React.FC<ConceptAddressPanelProps> = ({
  concept,
  rubric,
  rubricPath,
  volume,
  onFilterChange,
  loadedRefs,
}) => {
  const incoming = concept.incoming_links ?? [];
  // Метка издания не нужна, пока статья одна — заголовок страницы её и так
  // называет; появляется только когда указателей о понятии несколько.
  const showEdition = concept.articles.length > 1;

  return (
    <aside className="concept-panel">
      {concept.articles.map((article) => (
        <ConceptArticleBlock
          key={article.id}
          article={article}
          rubric={rubric}
          rubricPath={rubricPath}
          volume={volume}
          onFilterChange={onFilterChange}
          loadedRefs={loadedRefs}
          showEdition={showEdition}
        />
      ))}

      {incoming.length > 0 && (
        <section className="concept-panel-section">
          <h2>Ссылаются сюда</h2>
          <ul className="concept-link-list">
            {incoming.map((link) => (
              <li key={link.slug}>
                <span className="concept-link-kind">
                  {link.kind === 'see' ? 'см.' : 'см. также'}
                </span>{' '}
                <Link to={`/concepts/${link.slug}`}>{link.title}</Link>
              </li>
            ))}
          </ul>
        </section>
      )}
    </aside>
  );
};
