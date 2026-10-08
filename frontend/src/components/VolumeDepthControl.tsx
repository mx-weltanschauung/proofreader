import { useRef } from 'react';
import { SEGMENT_CAP, depthSegments, type OutlineMetrics } from '../utils/volumeOutline';
import { plural } from '../utils/volumeLabel';
import { FeatureHint } from './FeatureHint';
import './VolumeDepthControl.css';

interface Props {
  /** Мерки тома: считаны один раз выше и переданы сюда, а не пересчитаны. */
  metrics: OutlineMetrics;
  depth: number;
  /** Сколько строк видно сейчас. */
  shown: number;
  /**
   * Сколько строк в дереве всего. В поиске не передаётся: там показанное и
   * есть весь результат, и счётчик называет одну величину — «3 строки».
   * Прежде сюда клали то же число, что и в `shown`, и ветка «N из M» в
   * поиске была недостижима.
   */
  total?: number;
  /** Во время поиска глубина ни на что не влияет. */
  disabled: boolean;
  onPick: (depth: number) => void;
}

export const VolumeDepthControl: React.FC<Props> = ({
  metrics,
  depth,
  shown,
  total,
  disabled,
  onPick,
}) => {
  const segments = depthSegments(metrics, depth);
  const limit = metrics.max;
  const segRef = useRef<HTMLDivElement>(null);

  // «18 из 94 строк» стоит рядом с органом, чтобы цена шага вглубь была видна
  // до нажатия, а не после. В обороте «из N» существительное всегда в
  // родительном множественном («из 4 строк»), склонять его по числу не надо.
  const counter =
    total !== undefined && shown < total
      ? `${shown} из ${total} строк`
      : plural(shown, ['строка', 'строки', 'строк']);

  return (
    <>
      {segments.length > 0 && (
        <div className="vol-toc-depth">
          <span className="vol-toc-depth-label" id="vol-toc-depth-label">
            глубина
          </span>
          <div
            className="vol-toc-depth-seg"
            ref={segRef}
            role="group"
            aria-labelledby="vol-toc-depth-label"
          >
            {segments.map((value) => {
              // Подпись — один раз на видимый текст и на доступное имя:
              // иначе для «всё» они расходятся, и голосовое управление по
              // видимому слову кнопку не находит (WCAG 2.5.3).
              const label = value === limit && limit > SEGMENT_CAP ? 'всё' : String(value);
              return (
                <button
                  key={value}
                  type="button"
                  className={`vol-toc-depth-btn${value === depth ? ' is-on' : ''}`}
                  aria-pressed={value === depth}
                  aria-label={`Глубина ${label}`}
                  disabled={disabled}
                  onClick={() => onPick(value)}
                >
                  {label}
                </button>
              );
            })}
          </div>
          <FeatureHint id="outline-depth" anchorRef={segRef} />
        </div>
      )}
      <p className="vol-toc-count">{counter}</p>
    </>
  );
};
