import { useEffect, useId, useRef, useState } from 'react';
import { SearchPanel } from './SearchPanel';
import { EMPTY_SCOPE, type SearchScope } from '../utils/searchScope';
import {
  registerSearchHotkeyCandidate,
  unregisterSearchHotkeyCandidate,
} from '../utils/searchHotkey';
import './SearchTrigger.css';

interface SearchTriggerProps {
  /** Сужение: том, собрание. Без него — весь корпус. */
  scope?: SearchScope;
  initialQuery?: string;
  /** Надпись затравки и её accessible-имя; без неё — общее «Поиск по читальне». */
  label?: string;
  /** Шапка: узкая затравка, на тесном экране — одна лупа. */
  compact?: boolean;
}

const DEFAULT_LABEL = 'Поиск по читальне';

/**
 * Затравка вместо формы (задача 12): кнопка, открывающая SearchPanel, а не
 * собственное поле ввода. Кнопкой, а не input — намеренно: символы, набранные
 * между фокусом и монтированием портала панели, у input пропали бы (эффект
 * монтирования срабатывает уже после keydown), а клик по кнопке просто
 * открывает уже смонтированную панель.
 */
export const SearchTrigger: React.FC<SearchTriggerProps> = ({
  scope = EMPTY_SCOPE,
  initialQuery = '',
  label,
  compact = false,
}) => {
  const [open, setOpen] = useState(false);
  const toggleRef = useRef<HTMLButtonElement>(null);
  const hotkeyId = useId();

  // Мирроринг ChapterTocDrawer.close: фокус после закрытия возвращается на
  // затравку, а не теряется на странице (второе предупреждение из рецензии
  // панели).
  const close = () => {
    setOpen(false);
    toggleRef.current?.focus();
  };

  // Находка 1 итоговой рецензии: раньше каждая затравка вешала свой
  // слушатель keydown на document — на странице их бывает по две-три сразу
  // (шапка + карточка тома/собрания + строка области на /search), и «/»
  // открывал столько же панелей одновременно. Регистрация в общем
  // держателе (searchHotkey.ts) вместо своего слушателя: сработавшее «/»
  // достаётся ровно одному кандидату. Компактная затравка (только в шапке) —
  // самый низкий приоритет: читатель, нажавший «/» на карточке тома или
  // собрания, ждёт панель с этой областью, а не пустую из шапки.
  useEffect(() => {
    registerSearchHotkeyCandidate(hotkeyId, compact ? 0 : 1, () => setOpen(true));
    return () => unregisterSearchHotkeyCandidate(hotkeyId);
  }, [hotkeyId, compact]);

  const accessibleLabel = label ?? DEFAULT_LABEL;
  const placeholder = compact ? 'Поиск' : 'Слово, имя или «фраза в кавычках»';
  // Затравка показывает то, что читателю сейчас интереснее всего: живой
  // запрос страницы выдачи, иначе подсказку области (том/собрание), иначе
  // общий плейсхолдер. aria-label остаётся постоянным (DEFAULT_LABEL или
  // переданный label) специально — доступное имя не должно прыгать вместе с
  // запросом, иначе запросы по имени кнопки в тестах и скринридере теряют
  // стабильную точку опоры.
  const visibleText = initialQuery || label || placeholder;

  return (
    <>
      <button
        ref={toggleRef}
        type="button"
        className={`search-trigger${compact ? ' is-compact' : ''}`}
        aria-haspopup="dialog"
        aria-expanded={open}
        aria-label={accessibleLabel}
        onClick={() => setOpen(true)}
      >
        <span className="search-trigger-icon" aria-hidden="true">
          🔍
        </span>
        <span className="search-trigger-text">{visibleText}</span>
      </button>
      {/* Круг правок 1, находка 2: панель монтировалась один раз и жила
          дальше без размонтирования — её useState(initialQuery/initialScope)
          инициализаторы срабатывают ровно один раз, поэтому смена адреса,
          пока панель закрыта (переход по обычной ссылке в обход неё самой),
          на следующем открытии показывала прежние значения. Монтирование
          только при open=true — тот самый пересоздающий инициализаторы
          случай, никакого эффекта с setState не понадобилось. */}
      {open && (
        <SearchPanel
          open={open}
          initialQuery={initialQuery}
          initialScope={scope}
          onClose={close}
          toggleRef={toggleRef}
        />
      )}
    </>
  );
};
