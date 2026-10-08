import React from 'react';
import { Link } from 'react-router-dom';
import type { VolumeSummary } from '../types';
import {
  volumeLabel,
  volumeLabelParts,
  spineNumber,
  spineNumberParts,
  isMostlyCaps,
} from '../utils/volumeLabel';
import { useClippedFlag } from '../utils/useClippedFlag';
import { workPath } from '../utils/paths';
import './VolumeSlab.css';

interface Props {
  volume: VolumeSummary;
  /** Том, на котором читатель остановился. */
  current: boolean;
  /**
   * Наведение и фокус. Штабель показывает по нему карточку тома и сам решает,
   * с какой задержкой: книга только сообщает, на чём стоит указатель.
   * Уход отдаётся как null.
   */
  onPeek?: (target: HTMLElement | null) => void;
  /** id карточки, пока она описывает именно эту книгу. */
  describedBy?: string;
  /** Добавка к классу: штабель метит ею книги, скрытые под кнопкой. */
  className?: string;
}

/** Одна строка подписи: работа тома. Замер обрыва ведётся построчно. */
const SlabLine: React.FC<{ text: string }> = ({ text }) => {
  // Влезла строка в отведённую ширину или оборвалась — знает только движок.
  const ref = useClippedFlag<HTMLSpanElement>(text);
  return (
    <span
      className="volume-slab-label"
      ref={ref}
      // Капс из содержания оптически тяжелее строчных при том же кегле, и
      // сорок две подписи из девяноста пяти набраны им. Текст не трогаем —
      // приведение сломает «Х СЪЕЗД РКП(б)», — гасим кеглем по этой метке.
      data-caps={isMostlyCaps(text) ? '' : undefined}
      aria-hidden="true"
    >
      {text}
    </span>
  );
};

/**
 * Книга, лежащая в пазу штабеля: клеймо с номером тома, за ним подпись —
 * по строке на каждую из работ, которыми том называется.
 *
 * Толщины у книги нет: все тома одной мерки. Привязка толщины к объёму тома
 * здесь была дважды и оба раза снята, и замер корпуса объясняет почему —
 * 56 томов из 95 лежат в полосе 500—700 полос, p10—p90 это 434—795. Толщина
 * по объёму дала бы ±15% шума вместо различия. Объём читатель узнаёт из
 * карточки по наведению и со страницы самого тома.
 *
 * Порядок на книге: номер, затем подпись. Прежде номер стоял в хвосте, чтобы
 * сорок пять цифр не сложились в графу таблицы, — при подписи в одну строку
 * это было верно. Строк теперь до двух, и номер в хвосте повисал бы между
 * ними, не принадлежа ни одной.
 */
export const VolumeSlab: React.FC<Props> = ({
  volume,
  current,
  onPeek,
  describedBy,
  className = '',
}) => {
  const lines = volumeLabelParts(volume);
  const { number, part } = spineNumberParts(volume);
  // Чтению с экрана и подпись, и номер достаются одной строкой: ярусы и
  // строки — приём вёрстки, а не перечисление. Номер берётся из spineNumber, а
  // не из `number` выше: там уже отделена часть, и «Том 26 III» прочлось бы
  // двумя разными числами вместо одного тома.
  const label = volumeLabel(volume);
  const coordinate = spineNumber(volume);
  const name = label ? `Том ${coordinate}. ${label}` : `Том ${coordinate}`;

  return (
    <Link
      to={workPath(volume)}
      className={`volume-slab${current ? ' is-current' : ''}${className}`}
      aria-label={name}
      aria-describedby={describedBy}
      onMouseEnter={(e) => onPeek?.(e.currentTarget)}
      onMouseLeave={() => onPeek?.(null)}
      onFocus={(e) => onPeek?.(e.currentTarget)}
      onBlur={() => onPeek?.(null)}
    >
      {/* Клеймо: номер тома, а под ним — часть, если том вышел в нескольких
          книгах. Вторым ярусом, а не в строку: «26·III» одной строкой при
          кегле клейма занимает 65.4px против 41.6px колонки и вылезает влево,
          за заднюю стенку. Расширять колонку ради четырёх таких томов значило
          бы отнять ширину у подписи всех девяноста пяти. */}
      <span className="volume-slab-plate" aria-hidden="true">
        <span className="volume-slab-plate-number">{number}</span>
        {part && <span className="volume-slab-plate-part">{part}</span>}
      </span>
      <span className="volume-slab-lines">
        {lines.map((line) => (
          <SlabLine key={line} text={line} />
        ))}
      </span>
    </Link>
  );
};
