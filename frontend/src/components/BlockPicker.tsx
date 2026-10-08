import { useEffect, useState } from 'react';
import { pagesApi } from '../services/api';
import { apiErrorMessage } from '../utils/apiError';
import type { PageBlock } from '../types';
import './BlockPicker.css';

export interface BlockMark {
  pageNumber: number;
  /** Байтовое смещение в content_markdown полосы. */
  offset: number;
}

export interface BlockPickerProps {
  workId: number;
  pageId: number;
  pageNumber: number;
  onPick: (edge: 'start' | 'end', offset: number, pageNumber: number) => void;
  startMark?: BlockMark;
  endMark?: BlockMark;
}

/**
 * Выбор куска полосы БЛОКАМИ. Читателю сырой markdown показывать нельзя, а
 * отображать выделение с отрисованного HTML обратно в байты нечем: такой
 * карты в проекте нет. Поэтому обе стороны — границы и вёрстку — производит
 * один разрез на сервере (`GET /works/{id}/pages/{id}/blocks`), и
 * сопоставлять их не приходится.
 *
 * Цена решения названа в спеке: вклейка меньше абзаца не берётся — меньше
 * абзаца это цитата, и её уносит отдельная кнопка «Цитировать».
 *
 * Сервер уже отфильтровал блоки-сноски до ответа (`page_blocks.go`) — для
 * них нет вёрстки, а тип `PageBlock.kind` вида `'footnote'` вовсе не знает
 * (задача 9). Клиентского фильтра здесь поэтому нет: он был бы мёртвым
 * кодом по значению, которое никогда не приходит по проводу.
 *
 * Начало и конец ставятся ДВУМЯ отдельными жестами и могут лежать на разных
 * полосах одного тома: этим закрыта многостраничная вклейка, до которой
 * прежний подборщик (выделение в `<pre>`) не дотягивался, хотя API и рендер
 * умели её всегда.
 *
 * Замер на живом корпусе (5106 полос, 5 корпусов): семь полос из 5106 дают
 * ПУСТОЙ список блоков при непустом тексте — продолжение аппарата с
 * предыдущей полосы, а не поломка; для этого случая — отдельное сообщение
 * ниже, а не пустая область выбора.
 */
export const BlockPicker: React.FC<BlockPickerProps> = ({
  workId,
  pageId,
  pageNumber,
  onPick,
  startMark,
  endMark,
}) => {
  const [blocks, setBlocks] = useState<PageBlock[] | null>(null);
  const [error, setError] = useState<string | null>(null);

  // Смена полосы не должна на миг показать блоки прежней: ключ запроса
  // сравнивается прямо в теле рендера, без эффекта — тот же приём, что в
  // PageConcepts.tsx (react-hooks 7 не пускает setState внутри эффекта
  // ради простой подстройки под смену пропсов).
  const requestKey = `${workId}|${pageId}`;
  const [prevRequestKey, setPrevRequestKey] = useState(requestKey);
  if (prevRequestKey !== requestKey) {
    setPrevRequestKey(requestKey);
    setBlocks(null);
    setError(null);
  }

  useEffect(() => {
    let cancelled = false;
    pagesApi
      .blocks(workId, pageId)
      .then((res) => {
        if (!cancelled) setBlocks(res.data.blocks);
      })
      .catch((err: unknown) => {
        if (!cancelled) setError(apiErrorMessage(err, 'Не удалось разобрать полосу на блоки'));
      });
    return () => {
      cancelled = true;
    };
  }, [workId, pageId]);

  if (error) return <p className="block-picker-error">{error}</p>;
  if (!blocks) return <p className="block-picker-loading">Разбираем полосу…</p>;

  if (blocks.length === 0) {
    return (
      <p className="block-picker-empty">
        На этой полосе нечего вклеить: текст — продолжение аппарата с предыдущей страницы.
      </p>
    );
  }

  return (
    <ol className="block-picker">
      {blocks.map((block) => {
        const isStart = startMark?.pageNumber === pageNumber && startMark.offset === block.start;
        const isEnd = endMark?.pageNumber === pageNumber && endMark.offset === block.end;
        return (
          <li
            key={block.start}
            className={
              'block-picker-item' +
              (isStart ? ' block-picker-item--start' : '') +
              (isEnd ? ' block-picker-item--end' : '')
            }
          >
            <div
              className="block-picker-body"
              // HTML собран нашим же рендерером из нашего же корпуса — тем
              // самым, которым отрисована каждая страница чтения. Вид блока
              // (`block.kind`) не несётся в отдельном классе: он уже виден
              // в самой вёрстке (заголовок приезжает <h*>, цитата — <blockquote>
              // и т. д.) — класс без единого стиля для него читался бы как
              // потерянный стиль, а не как задел.
              dangerouslySetInnerHTML={{ __html: block.html }}
            />
            <div className="block-picker-actions">
              <button type="button" onClick={() => onPick('start', block.start, pageNumber)}>
                Начало вклейки
              </button>
              <button type="button" onClick={() => onPick('end', block.end, pageNumber)}>
                Конец вклейки
              </button>
            </div>
          </li>
        );
      })}
    </ol>
  );
};
