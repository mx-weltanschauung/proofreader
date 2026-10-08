import { Fragment, useId, useRef, useState, type RefObject } from 'react';
import { createPortal } from 'react-dom';
import { useNavigate } from 'react-router-dom';
import { useDrawerChrome } from './useDrawerChrome';
import { SEARCH_HINTS } from '../utils/searchHints';
import { readRecentQueries, rememberQuery, clearRecentQueries } from '../utils/recentQueries';
import { isTooShortQuery, normalizeQuery, TOO_SHORT_MESSAGE } from '../utils/searchQuery';
import { searchPath, type SearchScope } from '../utils/searchScope';
import { ScopePicker } from './ScopePicker';
import './SearchPanel.css';

export interface SearchPanelProps {
  open: boolean;
  initialQuery: string;
  initialScope: SearchScope;
  onClose: () => void;
  /**
   * Кнопка-затравка, рисует которую вызывающая сторона (задача 12). Без неё
   * «клик мимо» не отличит клик по самой затравке от клика вне панели: свой
   * onClick затравки панель бы уже открыл, а тот же клик, дойдя до
   * document-обработчика useDrawerChrome, тут же закрыл бы её обратно.
   */
  toggleRef?: RefObject<HTMLButtonElement>;
}

/**
 * Панель поиска поверх страницы: выбор области (собрания, тома галочками),
 * крупное поле запроса, приёмы запроса, недавние запросы читателя.
 */
export const SearchPanel: React.FC<SearchPanelProps> = ({
  open,
  initialQuery,
  initialScope,
  onClose,
  toggleRef: toggleRefProp,
}) => {
  const navigate = useNavigate();
  const panelRef = useRef<HTMLDivElement>(null);
  const inputRef = useRef<HTMLInputElement>(null);
  // Без переданного ref (например, тест самой панели без затравки) —
  // собственный пустой: «клик по несуществующей кнопке» просто не наступит.
  const ownToggleRef = useRef<HTMLButtonElement>(null);
  const toggleRef = toggleRefProp ?? ownToggleRef;
  const titleId = useId();
  const inputId = `${titleId}-input`;

  const [draft, setDraft] = useState(initialQuery);
  const [scope, setScope] = useState<SearchScope>(initialScope);
  const [tooShort, setTooShort] = useState(false);
  // Ленивый инициализатор: чтение localStorage не должно повторяться на
  // каждый рендер, а список должен уметь опустеть после «Очистить» без
  // повторного похода в хранилище.
  const [recent, setRecent] = useState(readRecentQueries);

  useDrawerChrome({
    open,
    panelRef,
    toggleRef,
    initialFocusRef: inputRef,
    onEscape: onClose,
    onOutsideClick: onClose,
  });

  if (!open) return null;

  const submit = (event: React.FormEvent) => {
    event.preventDefault();
    const q = normalizeQuery(draft);
    // Пустое поле — молча: читатель нажал Enter, ничего не набрав, и сказать
    // ему нечего, кроме того, что он и так видит.
    if (!q) return;
    if (isTooShortQuery(q)) {
      setTooShort(true);
      return;
    }
    setTooShort(false);
    rememberQuery(q);
    navigate(searchPath(q, scope));
    onClose();
  };

  const pickRecent = (q: string) => {
    setDraft(q);
    inputRef.current?.focus();
  };

  const clearRecent = () => {
    clearRecentQueries();
    setRecent([]);
  };

  // Портал на body по той же причине, что и у остальных выезжающих панелей:
  // липкая шапка — свой контекст наложения, внутри неё панель оказалась бы
  // под полосой прогресса чтения.
  return createPortal(
    <>
      <div className="search-panel-backdrop" aria-hidden="true" />
      <div
        ref={panelRef}
        className="search-panel"
        role="dialog"
        aria-modal="true"
        aria-labelledby={titleId}
      >
        <div className="search-panel-head">
          <h2 className="search-panel-title" id={titleId}>
            Поиск по читальне
          </h2>
          <button
            type="button"
            className="search-panel-close"
            aria-label="Закрыть"
            onClick={onClose}
          >
            <span aria-hidden="true">✕</span>
          </button>
        </div>

        <ScopePicker value={scope} onChange={setScope} />

        <form className="search-panel-form" onSubmit={submit}>
          <label className="search-panel-label" htmlFor={inputId}>
            Слово, имя или «фраза в кавычках»
          </label>
          <div className="search-panel-row">
            <input
              ref={inputRef}
              id={inputId}
              type="search"
              className="search-panel-input"
              value={draft}
              onChange={(event) => {
                setDraft(event.target.value);
                if (tooShort) setTooShort(false);
              }}
              autoComplete="off"
            />
            <button type="submit" className="btn btn-secondary search-panel-submit">
              Найти
            </button>
          </div>
          {tooShort && (
            <p className="search-panel-error" role="alert">
              {TOO_SHORT_MESSAGE}
            </p>
          )}
        </form>

        {recent.length > 0 && (
          <div className="search-panel-recent">
            <div className="search-panel-recent-head">
              <h3 className="search-panel-subtitle">Недавние запросы</h3>
              <button type="button" className="search-panel-clear" onClick={clearRecent}>
                Очистить
              </button>
            </div>
            <ul className="search-panel-recent-list">
              {recent.map((q) => (
                <li key={q}>
                  <button
                    type="button"
                    className="search-panel-recent-item"
                    onClick={() => pickRecent(q)}
                  >
                    {q}
                  </button>
                </li>
              ))}
            </ul>
          </div>
        )}

        <dl className="search-panel-hints">
          {SEARCH_HINTS.map((hint) => (
            <Fragment key={hint.query}>
              <dt>{hint.query}</dt>
              <dd>{hint.meaning}</dd>
            </Fragment>
          ))}
        </dl>
      </div>
    </>,
    document.body,
  );
};
