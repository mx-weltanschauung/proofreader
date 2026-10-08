import { useEffect, useRef, useState } from 'react';
import { Link } from 'react-router-dom';
import { ReadingSettings } from './ReadingSettings';
import { FeatureHint } from './FeatureHint';
import { CiteButton, type CitationContext } from './CiteButton';
import { PageJump, type PageJumpProps } from './PageJump';
import { ProgressTicks } from './ProgressTicks';
// Маршруты грузятся лениво, и Vite режет CSS по тем же чанкам: стили,
// объявленные в файле, который этот маршрут не импортирует, до него не
// доезжают. Разметка ниже держится на правилах ReadingSurface.css
// (.chapter-pages-content, .page-marker, пара подписей на кнопках панели) —
// поэтому импорт стоит здесь, а не подразумевается соседом. Сторож —
// readingStyles.guard.test.ts.
import './ReadingSurface.css';
import './ReadingStreamBar.css';

/**
 * Ниже этой отметки панель начинает прятаться. У самого верха прятать нечего:
 * читатель ещё не ушёл в текст.
 */
const HIDE_AFTER_PX = 120;

export interface ReadingStreamBarProps {
  /** Глава, накрывающая видимую страницу; null — пока неизвестна. */
  chapterTitle: string | null;
  /** Доля прочитанного по работе, 0..100. */
  progressPercent: number;
  /** Куда ведёт выход из чтения. */
  backHref: string;
  /** Показаны ли печатные номера страниц в тексте. */
  showPageNumbers: boolean;
  onTogglePageNumbers: () => void;
  /**
   * Куда ведёт «Предложить исправление» для видимой сейчас страницы;
   * null, пока она не определена (самый верх/низ потока — тот же случай,
   * что и в панели чтения главы).
   */
  suggestHref: string | null;
  /**
   * Куда ведёт «Скан страницы» для видимой сейчас полосы; null — пока она не
   * определена. Прежде на этот экран уводил сам маркер полосы, но он стал
   * якорем потока, и переход переехал сюда.
   */
  scanHref: string | null;
  /**
   * Данные для кнопки «Цитировать»/«Ссылка» — необязателен по тому же
   * правилу, что и у ReadingSurface (см. CitationContext): без автора,
   * названия работы и издания подпись цитаты собрать нечем.
   */
  citation?: CitationContext;
  /**
   * Контейнер текста потока — WorkRead вешает его на
   * `<article className="work-read-content">`. Выделение через два окна
   * потока лежит внутри него целиком, поэтому CiteButton читает его, а не
   * отдельное окно.
   */
  contentRef: React.RefObject<HTMLElement>;
  /**
   * Видимая сейчас страница потока — тот же источник, что строит
   * suggestHref/scanHref у потребителя. Ссылка «Ссылка» (без выделения)
   * ведёт на неё; null, пока она не определена (самый верх/низ потока).
   */
  visiblePage: number | null;
  /**
   * Переход к странице — встаёт на место доли прочитанного цифрой, как и в
   * панели главы (ReadingSurface). Без него панель печатает долю.
   */
  pageJump?: Omit<PageJumpProps, 'className'>;
}

/**
 * Панель экрана потокового чтения.
 *
 * Своя, а не общая шапка сайта: экран чтения её прячет. Отсюда же следует,
 * что настройки типографики монтируются здесь — иначе читатель до них не
 * доберётся, а живут они в скрытой шапке (Header.tsx).
 */
export const ReadingStreamBar: React.FC<ReadingStreamBarProps> = ({
  chapterTitle,
  progressPercent,
  backHref,
  showPageNumbers,
  onTogglePageNumbers,
  suggestHref,
  scanHref,
  citation,
  contentRef,
  visiblePage,
  pageJump,
}) => {
  const [hidden, setHidden] = useState(false);
  // Прошлая позиция живёт в ref, а не в состоянии: она нужна только для
  // сравнения на следующем событии и перерисовки не стоит.
  const lastYRef = useRef(0);
  const numbersRef = useRef<HTMLButtonElement>(null);

  useEffect(() => {
    let frame = 0;
    const onScroll = () => {
      if (frame) return;
      frame = window.requestAnimationFrame(() => {
        frame = 0;
        const y = window.scrollY;
        const goingDown = y > lastYRef.current;
        lastYRef.current = y;
        setHidden(goingDown && y > HIDE_AFTER_PX);
      });
    };
    window.addEventListener('scroll', onScroll, { passive: true });
    onScroll();
    return () => {
      window.removeEventListener('scroll', onScroll);
      if (frame) cancelAnimationFrame(frame);
    };
  }, []);

  return (
    <div className={`reading-stream-bar ${hidden ? 'is-hidden' : ''}`}>
      <div
        className="reading-stream-progress"
        style={{ width: `${progressPercent}%` }}
        role="progressbar"
        aria-valuenow={progressPercent}
        aria-valuemin={0}
        aria-valuemax={100}
        aria-label="Прочитано"
      />
      <ProgressTicks className="is-stream" />
      <div className="reading-stream-bar-row">
        <Link to={backHref} className="back-link">
          ← Выйти из чтения
        </Link>
        {chapterTitle && <span className="reading-stream-chapter">{chapterTitle}</span>}
        {/* То же, что в панели чтения главы (ReadingSurface): номер видимой
            полосы, он же переход к другой, а без перехода — доля цифрой.
            Режимы чтения не должны расходиться в том, что показывают. Класс
            кнопки — соседский, по той же причине, что у «№» ниже. */}
        {pageJump ? (
          <PageJump {...pageJump} className="reading-settings-toggle" />
        ) : (
          <span className="reading-percent">{progressPercent} %</span>
        )}
        {/* Опечатку замечают в потоке, а не на отдельной полосе: ссылка
            ведёт на правку видимой сейчас страницы. Пока видимая страница
            не определена (самый верх/низ потока), рисовать её не на что.
            Класс — тот же, что у соседней кнопки настроек: панель не
            сжимает элементы при прокрутке (только прячет целиком), но вид
            и размер соседних органов управления обязаны совпадать. */}
        {/* Скан уходит отдельным окном (как и в панели главы, ReadingSurface):
            место в потоке при сверке с оригиналом не теряется. Стрелка у
            надписи говорит об этом заранее. */}
        {/* Пара подписей у каждой ссылки — тот же приём, что у кнопок панели
            главы: на телефоне видна короткая, полное имя держит aria-label
            (скрытая display:none подпись в доступное имя не входит). Вдвоём
            полные надписи в узкую панель уже не влезают. */}
        {scanHref !== null && (
          <Link
            to={scanHref}
            target="_blank"
            rel="noopener noreferrer"
            className="reading-settings-toggle reading-stream-suggest-link"
            aria-label="Скан страницы"
          >
            <span className="toolbar-label-full">Скан страницы</span>
            <span className="toolbar-label-short">Скан</span>
            <span className="toolbar-label-external" aria-hidden="true">
              ↗
            </span>
          </Link>
        )}
        {suggestHref !== null && (
          <Link
            to={suggestHref}
            className="reading-settings-toggle reading-stream-suggest-link"
            aria-label="Предложить исправление"
          >
            <span className="toolbar-label-full">Предложить исправление</span>
            <span className="toolbar-label-short">Исправить</span>
          </Link>
        )}
        {citation && (
          <CiteButton
            contentRef={contentRef}
            visiblePage={visiblePage}
            className="reading-settings-toggle"
            {...citation}
          />
        )}
        {/* Класс взят у соседней кнопки настроек намеренно: две кнопки стоят
            в одной панели вплотную и обязаны совпадать по размеру и виду, а
            своя копия тех же двадцати строк разъехалась бы при первой же
            правке. Надпись — знак номера, поэтому имя кнопке даёт
            aria-label. */}
        <button
          ref={numbersRef}
          type="button"
          className="reading-settings-toggle"
          aria-label="Номера страниц"
          aria-pressed={showPageNumbers}
          onClick={onTogglePageNumbers}
        >
          <span aria-hidden="true">№</span>
        </button>
        <FeatureHint id="page-numbers" anchorRef={numbersRef} />
        {/* withHint: эта копия ReadingSettings — единственная с видимым
            якорем на экране чтения (шапка сайта здесь скрыта CSS, но не
            размонтирована). См. комментарий у ReadingSettingsProps. */}
        <ReadingSettings />
      </div>
    </div>
  );
};
