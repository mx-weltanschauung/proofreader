/** Заливка файла записи по подписанной ссылке. XHR, а не fetch: у fetch нет
 *  прогресса отправки. Content-Type — ровно подписанный: PresignPut включает
 *  его в подпись, иной хранилище отвергнет. */
export function putWithProgress(
  url: string,
  file: Blob,
  contentType: string,
  onProgress: (loaded: number, total: number) => void,
  makeXhr: () => XMLHttpRequest = () => new XMLHttpRequest(),
): Promise<void> {
  return new Promise((resolve, reject) => {
    const xhr = makeXhr();
    xhr.open('PUT', url);
    xhr.setRequestHeader('Content-Type', contentType);
    xhr.upload.onprogress = (e) => {
      if (e.lengthComputable) onProgress(e.loaded, e.total);
    };
    xhr.onload = () =>
      xhr.status >= 200 && xhr.status < 300
        ? resolve()
        : reject(new Error(`хранилище ответило ${xhr.status}`));
    xhr.onerror = () => reject(new Error('заливка оборвалась'));
    xhr.send(file);
  });
}

/** Сколько ждать метаданных. iOS Safari не грузит media без жеста
 *  пользователя и не шлёт ни loadedmetadata, ни error — у второго файла
 *  пачки жеста уже нет; без потолка пачка висела бы с запертым полем. Отсюда
 *  и совет в отказе: первый файл пачки замеряется ещё внутри жеста. */
export const PROBE_TIMEOUT_MS = 15_000;

/** Длительность локального файла в секундах через <audio> (спека:
 *  «длительность через <audio> на локальном файле»). Браузер, не читающий
 *  формат, даёт ошибку или NaN — это отказ: сервер требует duration_ms > 0. */
export function probeDuration(
  file: Blob,
  makeAudio: () => HTMLAudioElement = () => document.createElement('audio'),
  timeoutMs: number = PROBE_TIMEOUT_MS,
): Promise<number> {
  return new Promise((resolve, reject) => {
    const url = URL.createObjectURL(file);
    const el = makeAudio();
    const finish = () => {
      clearTimeout(timer);
      el.onloadedmetadata = null;
      el.onerror = null;
      el.removeAttribute('src');
      URL.revokeObjectURL(url);
    };
    const timer = setTimeout(() => {
      finish();
      reject(new Error('браузер не открыл файл за отведённое время — прикрепите его отдельно'));
    }, timeoutMs);
    el.preload = 'metadata';
    el.onloadedmetadata = () => {
      const d = el.duration;
      finish();
      if (Number.isFinite(d) && d > 0) resolve(d);
      else reject(new Error('браузер не определил длительность файла'));
    };
    el.onerror = () => {
      finish();
      reject(new Error('браузер не читает этот файл'));
    };
    el.src = url;
  });
}
