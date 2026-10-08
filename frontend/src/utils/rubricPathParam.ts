/**
 * Путь подрубрик в значение параметра `?rubric_path=` и обратно.
 *
 * Звенья кодируются каждое по отдельности и склеиваются двоеточием.
 * Разделитель «:» безопасен ровно потому, что encodeURIComponent кодирует его
 * всегда (`%3A`), а дефис — никогда (он в списке символов, которые
 * encodeURIComponent оставляет как есть, вместе с `_ . ! ~ * ' ( )`): путь
 * ["а-б"] из одного звена и путь ["а","б"] из двух иначе дали бы одно и то же
 * значение. Тот же довод и тот же выбор, что у `rubricAnchor` в
 * ConceptArticleBlock.tsx; куплен он тремя подрубриками корпуса, несущими
 * двоеточие в заголовке.
 *
 * Значение уезжает через URLSearchParams и axios, и те кодируют его ЕЩЁ раз
 * (`%3A` -> `%253A`). Сервер раскодирует дважды: строку запроса целиком
 * (`r.URL.Query()`) и каждое звено (`parseRubricPath` в
 * internal/api/concept_stream.go). Двойное кодирование — не лишний слой, а
 * то, на чём держится разделитель; проверено сквозным прогоном пары
 * axios -> net/url.
 */
export function encodeRubricPath(path: readonly string[]): string {
  return path.map(encodeURIComponent).join(':');
}

/**
 * Обратный разбор. Звено, которое не раскодировалось или пусто, роняет ВЕСЬ
 * путь, а не пропускается: путь с выпавшим звеном — другой путь, и сузить им
 * выдачу значило бы показать читателю не то, что он просил. То же правило,
 * что у parseRubricPath на сервере.
 */
export function decodeRubricPath(raw: string): string[] {
  if (raw === '') return [];
  const out: string[] = [];
  for (const part of raw.split(':')) {
    let decoded: string;
    try {
      decoded = decodeURIComponent(part);
    } catch {
      return [];
    }
    if (decoded === '') return [];
    out.push(decoded);
  }
  return out;
}
