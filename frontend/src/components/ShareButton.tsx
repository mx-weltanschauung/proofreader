import toast from 'react-hot-toast';
import { citationLinkHtml, citationLinkMarkdown } from '../utils/citation';

interface ShareButtonProps {
  /** Одна человеческая строка: что это за страница («Том 6 — Ленин В. И.»). */
  title: string;
  /**
   * Канонический путь страницы — из построителей адресов (`workPath`,
   * `documentPath`…), а не `location.href`: строка браузера несёт `?q=`
   * подсветки поиска, хэш полосы и прочее, что получателю ни к чему.
   */
  path: string;
  className?: string;
}

/**
 * «Поделиться» — ссылка на страницу целиком: том, собрание, главу, понятие,
 * подборку, разбор. Описание получатель увидит в карточке превью (её отдаёт
 * мессенджеру `/seo`), поэтому кнопка везёт только подпись и адрес.
 *
 * Не двойник «Ссылки» (CiteButton): та ведёт на видимую сейчас полосу и
 * подписывает её библиографически, эта — на страницу и подписывает её так,
 * как назвал бы человек.
 *
 * Меню системы (`navigator.share`) — где оно есть: на телефоне это прямой
 * путь в чат, без «скопировать — уйти — вставить». Где нет (Chrome и Firefox
 * на Linux) — буфер теми же двумя гранями, что у «Ссылки».
 */
export function ShareButton({ title, path, className }: ShareButtonProps) {
  const onClick = async () => {
    const url = `${window.location.origin}${path}`;
    const data = { title, text: title, url };

    if (typeof navigator.share === 'function' && (navigator.canShare?.(data) ?? true)) {
      try {
        await navigator.share(data);
      } catch (err) {
        // Закрытое меню — решение читателя, а не сбой. Запасного буфера после
        // отказа нет намеренно: `await share` уже израсходовал жест
        // пользователя, и запись в буфер браузер отклонил бы тоже.
        if (!(err instanceof DOMException && err.name === 'AbortError')) {
          toast.error('Не удалось поделиться');
        }
      }
      return;
    }

    try {
      // До записи ни одного await — тот же довод, что у CiteButton.
      await navigator.clipboard.write([
        new ClipboardItem({
          'text/plain': new Blob([citationLinkMarkdown(title, url)], { type: 'text/plain' }),
          'text/html': new Blob([citationLinkHtml(title, url)], { type: 'text/html' }),
        }),
      ]);
      toast.success('Ссылка скопирована');
    } catch {
      toast.error('Не удалось скопировать');
    }
  };

  return (
    <button
      type="button"
      className={className ?? 'btn btn-secondary'}
      onClick={() => void onClick()}
    >
      Поделиться
    </button>
  );
}
