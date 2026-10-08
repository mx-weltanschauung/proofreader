import { useCallback, useState } from 'react';
import { conceptsApi } from '../services/api';
import type { ConceptCutByBounds, ConceptCutInput } from '../types';
import { apiErrorMessage } from '../utils/apiError';

/** Границы одной вырезки в пределах одной страницы, в байтах. */
export interface CutDraft {
  start: number;
  end: number;
}

export interface CutEditorResult {
  /** Правимые вырезки текущей страницы — то, что показывает и меняет UI. */
  cuts: CutDraft[];
  /** Сколько чужих вырезок адреса (другие страницы, свои прежние статусы)
   *  уйдут вместе с сохранением, не изменившись. */
  foreignCount: number;
  addCut: (cut: CutDraft) => void;
  removeCut: (index: number) => void;
  save: () => Promise<void>;
  isSaving: boolean;
  error: string;
}

/**
 * Вырезка целиком лежит на данной странице — значит, правима на её развороте.
 * Ссылка по id (чужая или с исчезнувшими частями) никогда не правима: на
 * клиенте у неё нет номера страницы, чтобы это проверить, — такая вырезка
 * всегда уходит как чужая, без изменений.
 */
function isOnPage(cut: ConceptCutInput, pageNumber: number): cut is ConceptCutByBounds {
  return 'start_page' in cut && cut.start_page === pageNumber && cut.end_page === pageNumber;
}

/**
 * Делит все вырезки адреса на правимые (эта страница) и чужие (остальные,
 * включая вырезки через разрыв страниц). Чужие хранятся как пришли — со
 * своим статусом, — чтобы сохранение могло переслать их не тронув.
 */
function splitByPage(
  existing: ConceptCutInput[],
  pageNumber: number,
): { editable: CutDraft[]; foreign: ConceptCutInput[] } {
  const editable: CutDraft[] = [];
  const foreign: ConceptCutInput[] = [];
  for (const cut of existing) {
    if (isOnPage(cut, pageNumber)) {
      editable.push({ start: cut.start_offset, end: cut.end_offset });
    } else {
      foreign.push(cut);
    }
  }
  editable.sort((a, b) => a.start - b.start);
  return { editable, foreign };
}

/**
 * Правка границ вырезок одной страницы адреса.
 *
 * `existing` — ВСЕ вырезки адреса, а не только этой страницы: запись
 * заменяющая (`PUT .../cuts` целиком переписывает набор), и человек,
 * поправивший одну границу, не должен молча стирать вырезки на других
 * страницах адреса, вырезки, идущие через разрыв страницы, или вырезки с
 * исчезнувшими частями (`ConceptCut.parts: []` — якорь вне диапазона адреса,
 * на клиенте нет номера страницы, чтобы их описать границами). Такие вырезки
 * хук не показывает и не даёт редактировать — только пересылает как есть,
 * ссылкой по id.
 *
 * Правка на этой странице идёт по одной странице: вырезка через границу
 * страниц режется человеком редко, а UI на две страницы разом стоил бы
 * вдвое дороже. Такую вырезку правка страницы превратит в две — это
 * осознанная потеря, о ней сказано в спеке.
 */
export function useCutEditor(
  slug: string,
  referenceId: number,
  pageNumber: number,
  existing: ConceptCutInput[],
): CutEditorResult {
  // existing входит в ключ содержимым, не только referenceId/pageNumber:
  // save() делает на сервере DELETE+INSERT (ReplaceForReference), и у каждой
  // вырезки после успешного сохранения новый id. Без этого чужие вырезки
  // (state.foreign), пересылаемые ссылкой по id, застряли бы со старыми id
  // и на следующем сохранении получили бы 404 — рабочий адрес и страница те
  // же, а набор уже не тот. Безопасно пересчитывать при каждой смене
  // existing: replaceEntry (единственный источник новых existing после
  // монтирования) вызывается только после подтверждённого успешного
  // сохранения, не на каждый чих.
  const key = `${referenceId}|${pageNumber}|${JSON.stringify(existing)}`;
  const [state, setState] = useState(() => ({ key, ...splitByPage(existing, pageNumber) }));

  // Смена адреса, страницы или состава вырезок — новая порция правки,
  // прежние черновики к ней не относятся. Сравнение в теле хука, а не
  // setState в эффекте: последнее запрещено правилом
  // react-hooks/set-state-in-effect.
  if (state.key !== key) {
    setState({ key, ...splitByPage(existing, pageNumber) });
  }

  const [isSaving, setIsSaving] = useState(false);
  const [error, setError] = useState('');

  const addCut = useCallback((cut: CutDraft) => {
    setState((prev) => ({
      ...prev,
      editable: [...prev.editable, cut].sort((a, b) => a.start - b.start),
    }));
  }, []);

  const removeCut = useCallback((index: number) => {
    setState((prev) => ({ ...prev, editable: prev.editable.filter((_, i) => i !== index) }));
  }, []);

  const save = useCallback(async () => {
    setIsSaving(true);
    setError('');
    try {
      await conceptsApi.putCuts(slug, referenceId, {
        status: 'confirmed',
        cuts: [
          // Чужие — без изменений, со своим прежним статусом: человек эту
          // вырезку не видел и не трогал.
          ...state.foreign,
          // Правимые — человек посмотрел границы этой страницы и подтвердил
          // их, независимо от того, каким статусом они пришли.
          ...state.editable.map((c) => ({
            start_page: pageNumber,
            start_offset: c.start,
            end_page: pageNumber,
            end_offset: c.end,
            status: 'confirmed' as const,
          })),
        ],
      });
    } catch (err: unknown) {
      setError(apiErrorMessage(err, 'Не удалось сохранить границы'));
    } finally {
      setIsSaving(false);
    }
  }, [slug, referenceId, pageNumber, state.foreign, state.editable]);

  return {
    cuts: state.editable,
    foreignCount: state.foreign.length,
    addCut,
    removeCut,
    save,
    isSaving,
    error,
  };
}
