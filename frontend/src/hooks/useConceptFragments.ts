import { useCallback, useEffect, useState } from 'react';
import { conceptsApi } from '../services/api';
import type { ConceptEntry, ConceptOrder } from '../types';
import { apiErrorMessage } from '../utils/apiError';
import { DEFAULT_CONCEPT_ORDER } from '../utils/conceptOrder';
import { encodeRubricPath } from '../utils/rubricPathParam';
import { parseVolumeFilter } from '../utils/volumeFilter';

/** Размер порции. Двадцать записей — примерно два экрана чтения. */
const BATCH = 20;

export interface ConceptFragmentsResult {
  entries: ConceptEntry[];
  total: number;
  /** Первая порция ещё летит. */
  isLoading: boolean;
  /** Догрузка следующей порции. */
  isLoadingMore: boolean;
  error: string;
  hasMore: boolean;
  loadMore: () => void;
  retry: () => void;
  /**
   * Перезапрашивает одну запись по адресу и подставляет её на место прежней,
   * сохраняя позицию и не трогая соседей, offset или total. Единственный
   * способ увидеть результат сохранения правки границ без оптимистичного
   * обновления (тело вырезки рендерит сервер) и без перезагрузки всей порции
   * (та сбросила бы прокрутку и развороты соседних записей).
   */
  replaceEntry: (referenceId: number) => Promise<void>;
}

// Одно состояние вместо связки флагов: у «грузим первую», «грузим ещё»,
// «ошибка» и «всё пришло» нет промежуточных комбинаций, а рассинхронизировать
// раздельные setState — вопрос времени (та же причина, что в usePageByNumber).
interface StreamState {
  key: string;
  entries: ConceptEntry[];
  total: number;
  loaded: boolean;
  pending: boolean;
  error: string;
  offset: number;
  attempt: number;
}

function streamKey(
  slug: string | undefined,
  rubric: string,
  rubricPath: string,
  volume: string,
  order: ConceptOrder,
): string {
  return `${slug ?? ''}|${rubric}|${rubricPath}|${volume}|${order}`;
}

function initial(key: string, pending: boolean): StreamState {
  return {
    key,
    entries: [],
    total: 0,
    loaded: false,
    pending,
    error: '',
    offset: 0,
    attempt: 0,
  };
}

/**
 * Есть ли ещё неспрошенные слоты за уже сделанным запросом. Сервер
 * пагинирует по слотам (разрешённым адресам), а не по фактически пришедшим
 * записям: адрес, чьи страницы пропали из базы, слот занимает, но записью не
 * становится. Считать offset числом пришедших записей значит на пропуске
 * либо переспросить уже показанный слот (сдвиг и дубли), либо никогда не
 * закрыть hasMore, если пропуск пришёлся на последний слот порции.
 */
function hasMoreSlots(offset: number, total: number): boolean {
  return offset + BATCH < total;
}

function requestParams(
  rubric: string,
  rubricPath: string,
  volume: string,
  order: ConceptOrder,
  offset: number,
) {
  const parsed = parseVolumeFilter(volume);
  return {
    ...(rubric ? { rubric } : {}),
    ...(rubricPath ? { rubric_path: rubricPath } : {}),
    ...(parsed ? { volume: parsed.volume } : {}),
    ...(parsed?.part ? { volume_part: parsed.part } : {}),
    // Дефолтный порядок не посылаем: запрос остаётся тем же, каким был до
    // появления параметра, и URL в логах не обрастает шумом.
    ...(order !== DEFAULT_CONCEPT_ORDER ? { order } : {}),
    limit: BATCH,
    offset,
  };
}

/**
 * Параметры точечного запроса одной записи (replaceEntry). Текущие rubric,
 * rubricPath и volume несём и сюда: если, пока человек правил границы, фильтр
 * сменился и адрес под него больше не подходит, сервер честно вернёт пустой
 * список — и запись молча не появится под фильтром, которому больше не
 * соответствует. Offset и limit: BATCH сюда не идут — точечная замена не
 * участвует в пагинации потока.
 */
function pointRequestParams(
  rubric: string,
  rubricPath: string,
  volume: string,
  referenceId: number,
) {
  const parsed = parseVolumeFilter(volume);
  return {
    ...(rubric ? { rubric } : {}),
    ...(rubricPath ? { rubric_path: rubricPath } : {}),
    ...(parsed ? { volume: parsed.volume } : {}),
    ...(parsed?.part ? { volume_part: parsed.part } : {}),
    reference_id: referenceId,
    limit: 1,
  };
}

/**
 * Поток записей понятия порциями. Фильтры принимаются строками ровно в том
 * виде, в каком лежат в URL: примитивы в зависимостях эффекта не создают
 * новую ссылку на каждый рендер, в отличие от объекта фильтра. `rubricPath` —
 * единственное исключение на входе (массив, как в URLSearchParams после
 * decodeRubricPath), но в ключ и в зависимости эффекта попадает уже строкой
 * (`pathParam`), тем же приёмом, что и остальные фильтры.
 */
export function useConceptFragments(
  slug: string | undefined,
  rubric: string,
  rubricPath: string[],
  volume: string,
  order: ConceptOrder,
): ConceptFragmentsResult {
  // Путь входит в ключ и в зависимости эффекта СТРОКОЙ: массив даёт новую
  // ссылку на каждый рендер, и эффект перезапрашивал бы поток без конца.
  const pathParam = encodeRubricPath(rubricPath);
  const key = streamKey(slug, rubric, pathParam, volume, order);
  const [state, setState] = useState<StreamState>(() => initial(key, Boolean(slug)));

  // Смена понятия или фильтра должна сразу вернуть поток к пустому — иначе
  // экран покажет фрагменты, которых в новом наборе нет. Сравнение в теле
  // компонента, а не setState в эффекте: последнее запрещает
  // react-hooks/set-state-in-effect.
  const [prevKey, setPrevKey] = useState(key);
  if (prevKey !== key) {
    setPrevKey(key);
    setState(initial(key, Boolean(slug)));
  }

  const { offset, attempt } = state;

  useEffect(() => {
    if (!slug) return;
    let cancelled = false;

    conceptsApi
      .fragments(slug, requestParams(rubric, pathParam, volume, order, offset))
      .then((res) => {
        if (cancelled) return;
        setState((prev) => {
          if (prev.key !== key) return prev;
          return {
            ...prev,
            entries: offset === 0 ? res.data.entries : [...prev.entries, ...res.data.entries],
            total: res.data.total,
            loaded: true,
            pending: false,
            error: '',
          };
        });
      })
      .catch((err: unknown) => {
        if (cancelled) return;
        setState((prev) => {
          if (prev.key !== key) return prev;
          return {
            ...prev,
            pending: false,
            error: apiErrorMessage(err, 'Не удалось загрузить записи понятия'),
          };
        });
      });

    return () => {
      cancelled = true;
    };
    // key покрывает slug/rubric/rubricPath/volume/order целиком; offset и
    // attempt задают, какая именно порция запрошена.
  }, [slug, rubric, pathParam, volume, order, key, offset, attempt]);

  const loadMore = useCallback(() => {
    setState((prev) => {
      if (prev.pending || prev.error !== '' || !hasMoreSlots(prev.offset, prev.total)) return prev;
      return { ...prev, pending: true, offset: prev.offset + BATCH };
    });
  }, []);

  const retry = useCallback(() => {
    setState((prev) =>
      prev.error === '' ? prev : { ...prev, pending: true, error: '', attempt: prev.attempt + 1 },
    );
  }, []);

  // Точечная замена одной записи после сохранения правки границ (задача
  // 13a). Оптимистичному обновлению взяться неоткуда — тело вырезки рендерит
  // сервер, — а перезагрузка всей порции сбросила бы прокрутку и развороты
  // соседних записей сразу после действия человека. Сбой запроса не должен
  // портить уже показанное: правка уже сохранена на сервере, и ронять из-за
  // неудачного обновления нечего показывать заново незачем — молча остаёмся
  // при прежнем состоянии.
  const replaceEntry = useCallback(
    async (referenceId: number) => {
      if (!slug) return;
      let replacement: ConceptEntry | undefined;
      try {
        const res = await conceptsApi.fragments(
          slug,
          pointRequestParams(rubric, pathParam, volume, referenceId),
        );
        replacement = res.data.entries[0];
      } catch {
        return;
      }
      if (!replacement) return;
      setState((prev) => {
        // Фильтр сменился, пока летел запрос: список уже не тот, к которому
        // относится ответ — подставлять в него нечего.
        if (prev.key !== key) return prev;
        const index = prev.entries.findIndex((e) => e.reference_id === referenceId);
        if (index === -1) return prev;
        const entries = prev.entries.slice();
        entries[index] = replacement;
        return { ...prev, entries };
      });
    },
    [slug, rubric, pathParam, volume, key],
  );

  return {
    entries: state.entries,
    total: state.total,
    isLoading: state.pending && !state.loaded,
    isLoadingMore: state.pending && state.loaded,
    error: state.error,
    hasMore: state.loaded && hasMoreSlots(state.offset, state.total),
    loadMore,
    retry,
    replaceEntry,
  };
}
