import { useId, useMemo, useState } from 'react';
import { filterOptions, type ComboOption } from '../utils/filterOptions';
import './FilterCombobox.css';

interface Props {
  /** id поля ввода — к нему привязывается `<label htmlFor>`. */
  id: string;
  options: ComboOption[];
  value: number | '';
  onChange: (id: number | '') => void;
  placeholder?: string;
  disabled?: boolean;
}

/**
 * Выбор из длинного списка с поиском: поле ввода, под ним отфильтрованный
 * список. Пока поле не в работе, в нём стоит подпись выбранного; начатый ввод
 * превращает его в запрос. Родной `<select>` не годится: в корпусе больше
 * двухсот томов, а у тома — сотни глав.
 */
export type { ComboOption };

export const FilterCombobox: React.FC<Props> = ({
  id,
  options,
  value,
  onChange,
  placeholder,
  disabled,
}) => {
  const listId = useId();
  // null — поле не редактируется и показывает выбранное.
  const [query, setQuery] = useState<string | null>(null);
  const [open, setOpen] = useState(false);
  const [active, setActive] = useState(0);

  const selected = options.find((o) => o.id === value);
  const visible = useMemo(() => filterOptions(options, query ?? ''), [options, query]);

  const openList = () => {
    setOpen(true);
    const at = visible.findIndex((o) => o.id === value);
    setActive(at >= 0 ? at : 0);
  };

  const close = () => {
    setOpen(false);
    setQuery(null);
  };

  const pick = (option: ComboOption) => {
    onChange(option.id);
    close();
  };

  const handleKeyDown = (e: React.KeyboardEvent<HTMLInputElement>) => {
    if (e.key === 'ArrowDown' || e.key === 'ArrowUp') {
      e.preventDefault();
      if (!open) {
        openList();
        return;
      }
      if (visible.length === 0) return;
      const step = e.key === 'ArrowDown' ? 1 : -1;
      const next = (active + step + visible.length) % visible.length;
      setActive(next);
      document.getElementById(`${listId}-${next}`)?.scrollIntoView?.({ block: 'nearest' });
    } else if (e.key === 'Enter') {
      if (open && visible[active]) {
        e.preventDefault();
        pick(visible[active]);
      }
    } else if (e.key === 'Escape') {
      if (open) {
        e.preventDefault();
        close();
      }
    }
  };

  return (
    <div className="filter-combobox">
      <input
        id={id}
        type="text"
        role="combobox"
        autoComplete="off"
        aria-autocomplete="list"
        aria-expanded={open}
        aria-controls={listId}
        aria-activedescendant={open && visible[active] ? `${listId}-${active}` : undefined}
        value={query ?? selected?.label ?? ''}
        placeholder={placeholder}
        disabled={disabled}
        onFocus={(e) => {
          // Первое же нажатие заменяет подпись выбранного запросом.
          e.target.select();
          openList();
        }}
        onClick={() => {
          if (!open) openList();
        }}
        onBlur={close}
        onChange={(e) => {
          setQuery(e.target.value);
          setOpen(true);
          setActive(0);
        }}
        onKeyDown={handleKeyDown}
      />
      {open && (
        <ul id={listId} role="listbox" className="filter-combobox-list">
          {visible.length === 0 && (
            <li role="presentation" className="filter-combobox-empty">
              Ничего не найдено
            </li>
          )}
          {visible.map((option, index) => {
            const header =
              option.group !== undefined && option.group !== visible[index - 1]?.group
                ? option.group
                : null;
            return [
              header !== null && (
                <li key={`g-${option.group}`} role="presentation" className="filter-combobox-group">
                  {header}
                </li>
              ),
              <li
                key={option.id}
                id={`${listId}-${index}`}
                role="option"
                aria-selected={option.id === value}
                className={
                  index === active
                    ? 'filter-combobox-option filter-combobox-option-active'
                    : 'filter-combobox-option'
                }
                style={{ paddingLeft: `${0.75 + (option.depth ?? 0) * 1.1}rem` }}
                // mousedown раньше blur: без этого поле закрыло бы список до клика.
                onMouseDown={(e) => e.preventDefault()}
                onMouseEnter={() => setActive(index)}
                onClick={() => pick(option)}
              >
                {option.label}
              </li>,
            ];
          })}
        </ul>
      )}
    </div>
  );
};
