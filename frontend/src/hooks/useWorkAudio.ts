import { useCallback, useEffect, useRef, useState } from 'react';
import { audioApi } from '../services/api';
import type { WorkAudio } from '../types';

/** Звук тома одним запросом (спека: «один запрос на том»). Ошибка — null:
 *  звук необязателен, глава и том рисуются и без него. Но сотруднику «звука
 *  нет» и «звук не загрузился» — разное (пустой список зовёт прикрепить
 *  повторно то, что уже лежит), поэтому провал виден отдельно — failed.
 *  Упавшее ПЕРЕчитывание того же тома прежний звук не стирает: иначе панель
 *  с менеджером записей размонтировалась бы вместе со строками статуса пачки,
 *  ровно при обрыве связи, о котором они и сообщают.
 *
 *  Ответ помнит, чей он: ChapterView переходит между томами без
 *  размонтирования, и звук прежнего тома совпал бы с главой нового по номерам
 *  полос — до прихода нового ответа или навсегда, если прежний опоздает. */
export function useWorkAudio(workId: number | null): {
  audio: WorkAudio | null;
  failed: boolean;
  reload: () => Promise<void>;
} {
  const [loaded, setLoaded] = useState<{
    workId: number;
    audio: WorkAudio | null;
    failed: boolean;
  } | null>(null);
  const current = useRef(workId);

  useEffect(() => {
    current.current = workId;
  }, [workId]);

  const reload = useCallback(async () => {
    if (workId === null) return;
    let audio: WorkAudio | null;
    let failed = false;
    try {
      audio = (await audioApi.forWork(workId)).data;
    } catch {
      audio = null;
      failed = true;
    }
    if (current.current !== workId) return;
    setLoaded((prev) => ({
      workId,
      audio: failed && prev?.workId === workId ? prev.audio : audio,
      failed,
    }));
  }, [workId]);

  useEffect(() => {
    void reload();
  }, [reload]);

  const mine = loaded && loaded.workId === workId ? loaded : null;
  return { audio: mine?.audio ?? null, failed: mine?.failed ?? false, reload };
}
