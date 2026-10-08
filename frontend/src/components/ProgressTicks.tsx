/** Отметки через каждые десять процентов — без краёв: у 0 и 100 их видно и так. */
const TICKS = [10, 20, 30, 40, 50, 60, 70, 80, 90];

/**
 * Засечки на полосе прогресса чтения.
 *
 * Долю цифрой панель больше не печатает — на её месте номер видимой полосы
 * (PageJump), — и прочитать долю теперь надо по самой полосе. Тонкая черта
 * без делений читается только как «больше/меньше половины»; засечки дают
 * десятки процентов на глаз. Украшение, а не данные: для чтения с экрана
 * долю называет `role="progressbar"` (у потока) или ничего — как и прежде.
 *
 * Стили — в ReadingSurface.css, рядом с полосой: оба места, где засечки
 * стоят, этот файл грузят.
 */
export const ProgressTicks: React.FC<{ className?: string }> = ({ className = '' }) => (
  <div className={`progress-ticks ${className}`} aria-hidden="true">
    {TICKS.map((percent) => (
      <span key={percent} className="progress-tick" style={{ left: `${percent}%` }} />
    ))}
  </div>
);

/**
 * Полоса прогресса главы и элемента подборки: сама полоса и засечки над ней.
 * Рисуется потребителем ReadingSurface соседом читалки, как и прежде.
 */
export const ReadingProgressBar: React.FC<{ percent: number }> = ({ percent }) => (
  <>
    <div className="reading-progress-bar" style={{ width: `${percent}%` }} />
    <ProgressTicks className="is-fixed" />
  </>
);
