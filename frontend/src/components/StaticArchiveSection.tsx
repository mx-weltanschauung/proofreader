import { useEffect, useState } from 'react';
import { API_BASE_URL, staticArchiveApi } from '../services/api';
import type { StaticArchive } from '../types';
import { archiveDate, archiveSize } from '../utils/staticArchive';
import { CopyMcpUrl } from './CopyMcpUrl';

type State = { kind: 'loading' } | { kind: 'ready'; archive: StaticArchive } | { kind: 'absent' };

/**
 * Раздел справки /help#offline: вся читальня одним архивом
 * (scripts/static-publish.sh). Сведения — из /api/static-archive. Без архива
 * раздел не прячется, а говорит, что архив пересобирается: на него ведёт
 * ссылка из подвала.
 */
export function StaticArchiveSection() {
  const [state, setState] = useState<State>({ kind: 'loading' });

  useEffect(() => {
    let alive = true;
    staticArchiveApi.get().then(
      (r) => {
        if (alive) setState({ kind: 'ready', archive: r.data });
      },
      () => {
        if (alive) setState({ kind: 'absent' });
      },
    );
    return () => {
      alive = false;
    };
  }, []);

  const archive = state.kind === 'ready' ? state.archive : null;

  return (
    <section>
      <h2 id="offline">Вся читальня одним архивом</h2>
      <p>
        Весь текст читальни — собрания, тома с примечаниями, предметный указатель и подборки — можно
        скачать одним архивом и читать без интернета. Сканов в архиве нет, только текст.
      </p>
      {archive && (
        <p className="help-archive-get">
          <a className="btn btn-primary" href={`${API_BASE_URL}/api/static-archive/download`}>
            Скачать архив — {archiveSize(archive.size)}
          </a>
          <span>
            ZIP, собран {archiveDate(archive.date)}; томов и работ — {archive.works}
          </span>
        </p>
      )}
      {state.kind === 'absent' && <p>Архив сейчас пересобирается — загляните позже.</p>}
      <p>
        <strong>Как открыть.</strong> Распакуйте архив и дважды щёлкните <code>index.html</code> в
        папке читальни: работают чтение, оглавления, указатель и поиск по заглавиям. Для поиска по
        тексту запустите из той же папки программу <code>serve</code> для своей системы (Windows,
        macOS или Linux) — она откроет читальню в браузере. Подробно — в файле{' '}
        <code>PROCHTI.txt</code> внутри архива.
      </p>
      <p>
        Папку читальни можно выложить зеркалом на любой хостинг как есть: все ссылки в ней
        относительные.
      </p>
      {archive && (
        <>
          <p className="help-archive-long">
            <strong>Проверить скачанное.</strong> Контрольная сумма SHA-256:{' '}
            <code>{archive.sha256}</code>{' '}
            <CopyMcpUrl url={archive.sha256} done="Сумма скопирована" />
          </p>
          <p className="help-archive-long">
            Прямой адрес для <code>wget</code> и менеджеров загрузок: <code>{archive.url}</code>
          </p>
        </>
      )}
      <p>
        Архив пересобирается по мере того, как в читальне появляются новые тома; дата сборки стоит в
        имени файла.
      </p>
    </section>
  );
}
