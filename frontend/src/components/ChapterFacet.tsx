import type { SearchChapterFacet } from '../types';
import { plural } from '../utils/volumeLabel';
import './ChapterFacet.css';

interface ChapterFacetProps {
  /** До 30 глав тома, где нашлось, по убыванию попаданий — как прислал сервер. */
  chapters: SearchChapterFacet[];
  /** Сколько всего глав тома содержат попадания (может быть больше chapters.length). */
  total: number;
  /** Главы, уже выбранные в адресе (scope.chapters) — те же id, что и в чипах ScopeBar. */
  selected: number[];
  onToggle: (id: number) => void;
}

/**
 * Фасет «где нашлось» внутри тома — список глав-переключателей над полосами.
 * Список присылает сервер уже посчитанным по ВСЕМУ тому: не фильтруем его на
 * клиенте по уже выбранному (`selected` влияет только на aria-pressed), иначе
 * выбранная глава пропала бы из списка сама и добавить вторую было бы нечем.
 *
 * Одна-единственная глава в списке не рисуется вовсе: единственный
 * переключатель ничего не сужает, только занимает место. Остаток
 * (chapters_total минус показанные) называется числом, а не прячется —
 * читатель должен видеть, что список неполон, у ленинских писем это обычное
 * дело.
 */
export function ChapterFacet({ chapters, total, selected, onToggle }: ChapterFacetProps) {
  if (chapters.length <= 1) return null;

  const remainder = total - chapters.length;

  return (
    <div className="chapter-facet">
      {chapters.map((c) => (
        <button
          key={c.id}
          type="button"
          className="btn btn-secondary btn-sm chapter-facet-button"
          aria-pressed={selected.includes(c.id)}
          onClick={() => onToggle(c.id)}
        >
          {c.title} <span className="chapter-facet-hits">{c.hits}</span>
        </button>
      ))}
      {remainder > 0 && (
        <span className="chapter-facet-remainder">
          и ещё {plural(remainder, ['глава', 'главы', 'глав'])}
        </span>
      )}
    </div>
  );
}
