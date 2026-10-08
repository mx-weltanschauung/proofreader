import React, { useMemo, useState } from 'react';
import type { VolumeSummary } from '../types';
import {
  COLLAPSED_VOLUMES_WIDE,
  collapseAt,
  mergeGapRuns,
  runCaption,
  shelfSlots,
} from '../utils/shelfGeometry';
import { plural } from '../utils/volumeLabel';
import { editionStats } from '../utils/editionStats';
import { VolumeSlab } from './VolumeSlab';
import { VolumeCard } from './VolumeCard';
import { useVolumePeek } from '../hooks/useVolumePeek';
import './VolumeShelf.css';

export { PEEK_DELAY_MS } from '../hooks/useVolumePeek';

interface Props {
  volumes: VolumeSummary[];
  currentWorkId?: number;
  /**
   * Сколько томов в собрании по плану издания. Задан — штабель идёт на всю
   * длину собрания, и ненаписанные тома в хвосте выглядят так же, как дыры
   * в середине. Не задан — штабель обрывается на последней книге.
   */
  volumesPlanned?: number;
  /**
   * Просьба свернуть длинное собрание и на широком экране. Главная рисует
   * пять штабелей подряд, и два из них — сорок пять и пятьдесят томов;
   * страница собрания (`EditionDetail`) этого не просит, потому что за
   * списком томов туда и приходят.
   */
  collapse?: boolean;
}

/**
 * id карточки. Внутри одного штабеля она одна, но `VolumeShelf` рисуется и
 * несколько раз на одной странице (главная — по штабелю на собрание), и у
 * каждого экземпляра свой `peek`: постоянство id держится на том, что мышь и
 * клавиатура не разъезжаются по разным штабелям одновременно. Если навести
 * мышь на книгу штабеля A, а потом увести фокус табом на книгу штабеля B, не
 * убрав указатель с A, — `mouseleave` у A не сработает, и оба штабеля
 * нарисуют элемент с этим id разом: два узла и неоднозначный
 * aria-describedby. Случай требует смешения мыши и клавиатуры на разных
 * штабелях, вероятность близка к нулю, цена — визуальный артефакт до первого
 * движения мыши.
 */
const CARD_ID = 'volume-card';

export const VolumeShelf: React.FC<Props> = ({
  volumes,
  currentWorkId,
  volumesPlanned,
  collapse = false,
}) => {
  // Штабель выкладывает не только книги: тому, которого в читальне нет,
  // остаётся его место — но не немой полосой задника, как прежде, а строкой,
  // которая называет пропущенные тома словами. На неполном собрании немые
  // полосы сливались в серую плиту: у Чернышевского четыре тома на пятнадцать
  // мест, у Плеханова четыре на двадцать четыре.
  const slots = useMemo(
    () => mergeGapRuns(shelfSlots(volumes, volumesPlanned)),
    [volumes, volumesPlanned],
  );
  // Место, по которое штабель показан на узком экране. Резать разметку по
  // нему нельзя: на широком экране собрание показывается целиком, а ширину
  // знает стиль, а не компонент. Поэтому скрытые места остаются в разметке и
  // помечаются классом — прячет их медиазапрос.
  const cut = useMemo(() => collapseAt(slots), [slots]);
  // Порог широкого экрана — второй и больший: одиннадцатая и двенадцатая
  // книги спрятаны на телефоне и показаны на мониторе, и одним классом это
  // не сказать. Считается тем же счётом по томам — и тем же правилом «ради
  // пары спрятанных книг кнопку не заводим», — поэтому у собрания в
  // тринадцать томов пороги расходятся: на телефоне оно свернётся, на
  // мониторе нет.
  const cutWide = useMemo(
    () => (collapse ? collapseAt(slots, COLLAPSED_VOLUMES_WIDE) : slots.length),
    [collapse, slots],
  );
  const [expanded, setExpanded] = useState(false);
  const { peek, handlePeek, pointerHandlers } = useVolumePeek();

  return (
    <div
      // Признак вешается не по просьбе, а по тому, есть ли на широком экране
      // что прятать: иначе у собрания, до порога не дотянувшего, на мониторе
      // стояла бы кнопка, которая ничего не раскрывает.
      className={`shelf${cutWide < slots.length ? ' shelf--compact' : ''}`}
      {...pointerHandlers}
    >
      <div
        className="shelf-stack"
        data-collapsed={cut < slots.length && !expanded ? '' : undefined}
      >
        {slots.map((slot, at) => {
          const hidden =
            (at >= cut ? ' is-overflow' : '') + (at >= cutWide ? ' is-overflow-wide' : '');
          if (slot.kind === 'volume') {
            return (
              <VolumeSlab
                key={slot.volume.id}
                volume={slot.volume}
                current={currentWorkId === slot.volume.id}
                onPeek={(target) => handlePeek(slot.volume, target)}
                describedBy={peek?.volume.id === slot.volume.id ? CARD_ID : undefined}
                className={hidden}
              />
            );
          }
          /* Пропуск, названный словами. От чтения с экрана не скрыт, в отличие
             от прежней немой полосы задника: счёт «4 из 15» в подписи собрания
             даёт итог, но не говорит, каких именно томов нет. */
          return (
            <span className={`shelf-run${hidden}`} key={`run-${slot.from}`}>
              {runCaption(slot.from, slot.to, slot.count)}
            </span>
          );
        })}
      </div>

      {/* Кнопка одна на оба порога: и узкому экрану, где штабель идёт в один
          столбец и сорок пять книг растянули бы собрание на добрую тысячу
          пикселей, и широкому — там, где собрание об этом попросило.
          Показывает её стиль, а не условие здесь: ширину знает медиазапрос, а
          не компонент, и решать это в JS значило бы завести второй источник
          правды о том же пороге. */}
      {cut < slots.length && (
        <button
          type="button"
          className="shelf-more"
          aria-expanded={expanded}
          onClick={() => setExpanded((was) => !was)}
        >
          {expanded
            ? 'Свернуть'
            : // Тем же счётом, что подпись собрания: книги-части одного тома —
              // один том. Без номеров вовсе — книги, как и в подписи.
              `Показать все ${plural(editionStats(volumes).volumes || volumes.length, [
                'том',
                'тома',
                'томов',
              ])}`}
        </button>
      )}

      {/* Карточка на штабеле одна: у книги overflow: hidden, и
          карточка-потомок была бы им обрезана — выносить её наружу пришлось бы
          в любом случае. */}
      {peek && <VolumeCard volume={peek.volume} anchor={peek.anchor} id={CARD_ID} />}
    </div>
  );
};
