import { useEffect, useLayoutEffect, useRef, useState } from 'react';
import { API_BASE_URL } from '../services/api';
import { FeatureHint } from './FeatureHint';
import './DownloadMenu.css';

/** Форматы в порядке убывания пользы для читателя. */
const FORMATS = [
  { id: 'epub', name: 'EPUB', hint: 'читалки и телефоны' },
  { id: 'fb2', name: 'FB2', hint: 'читалки, привычные по флибусте' },
  { id: 'md', name: 'Markdown', hint: 'исходный текст' },
  { id: 'html', name: 'HTML', hint: 'открыть в браузере' },
] as const;

const REMEMBERED_FORMAT = 'download-format';

interface DownloadMenuProps {
  /** Путь маршрута скачивания без параметра format. */
  href: string;
  label?: string;
  /** Есть звук — пятый пункт, плейлист .m3u. Запоминание формата на него
   *  не распространяется: следующее «Скачать» — снова книга. */
  audio?: boolean;
}

/** Прочитанный формат из localStorage — без записи ключа обратно и без падения в приватном режиме. */
function readRememberedFormat(): string | null {
  try {
    return localStorage.getItem(REMEMBERED_FORMAT);
  } catch {
    return null;
  }
}

export function DownloadMenu({ href, label = 'Скачать', audio = false }: DownloadMenuProps) {
  const [open, setOpen] = useState(false);
  const [lastFormat, setLastFormat] = useState<string | null>(readRememberedFormat);
  const root = useRef<HTMLDivElement>(null);
  const list = useRef<HTMLUListElement>(null);
  const toggle = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    if (!open) return;

    const onKey = (e: KeyboardEvent) => {
      if (e.key === 'Escape') setOpen(false);
    };
    const onClickOutside = (e: MouseEvent) => {
      if (root.current && !root.current.contains(e.target as Node)) setOpen(false);
    };

    document.addEventListener('keydown', onKey);
    document.addEventListener('mousedown', onClickOutside);
    return () => {
      document.removeEventListener('keydown', onKey);
      document.removeEventListener('mousedown', onClickOutside);
    };
  }, [open]);

  /* Кнопка стоит слева, поэтому список висит под её левым краем. В узком окне
     он упирается в правую границу — тогда привязка переворачивается на правый
     край кнопки. Замер идёт до отрисовки и правит класс на узле напрямую:
     состояние здесь дало бы лишний проход и скачок меню на глазах читателя. */
  useLayoutEffect(() => {
    const el = list.current;
    if (!open || !el) return;

    const place = () => {
      el.classList.remove('download-menu__list--flipped');
      const box = el.getBoundingClientRect();
      if (box.right > window.innerWidth - 8) {
        el.classList.add('download-menu__list--flipped');
      }
    };

    place();
    window.addEventListener('resize', place);
    return () => window.removeEventListener('resize', place);
  }, [open]);

  const remember = (format: string) => {
    try {
      localStorage.setItem(REMEMBERED_FORMAT, format);
    } catch {
      // Приватный режим запрещает запись — не повод ломать скачивание.
    }
    setLastFormat(format);
    setOpen(false);
  };

  return (
    <div className="download-menu" ref={root}>
      <button
        ref={toggle}
        type="button"
        className="download-menu__toggle"
        aria-expanded={open}
        aria-haspopup="menu"
        onClick={() => setOpen((v) => !v)}
      >
        {label}
        <svg
          className="download-menu__caret"
          width="10"
          height="6"
          viewBox="0 0 10 6"
          aria-hidden="true"
          focusable="false"
        >
          <path d="M1 1l4 4 4-4" fill="none" stroke="currentColor" strokeWidth="1.5" />
        </svg>
      </button>
      <FeatureHint id="download" anchorRef={toggle} />

      {open && (
        <ul className="download-menu__list" role="menu" ref={list}>
          {FORMATS.map((format) => {
            const isLast = format.id === lastFormat;
            return (
              <li key={format.id}>
                <a
                  role="menuitem"
                  className={
                    isLast
                      ? 'download-menu__item download-menu__item--last-used'
                      : 'download-menu__item'
                  }
                  aria-current={isLast ? 'true' : undefined}
                  href={`${API_BASE_URL}${href}?format=${format.id}`}
                  download
                  onClick={() => remember(format.id)}
                >
                  <span className="download-menu__name">{format.name}</span>
                  <span className="download-menu__hint">{format.hint}</span>
                </a>
              </li>
            );
          })}
          {audio && (
            <li>
              <a
                role="menuitem"
                className="download-menu__item"
                href={`${API_BASE_URL}${href}?format=m3u`}
                download
                onClick={() => setOpen(false)}
              >
                <span className="download-menu__name">Аудио — плейлист .m3u</span>
                <span className="download-menu__hint">для VLC и других плееров</span>
              </a>
            </li>
          )}
        </ul>
      )}
    </div>
  );
}
