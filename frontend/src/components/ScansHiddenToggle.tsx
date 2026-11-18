import { useState } from 'react';
import { isAdmin, useAuth } from '../hooks/useAuth';

type Kind = 'work' | 'edition' | 'journal' | 'chapter';

const KIND: Record<Kind, string> = {
  work: 'томе',
  edition: 'собрании',
  journal: 'журнале',
  chapter: 'главе',
};

const INHERITED: Record<string, string> = {
  work: 'Сканы скрыты у тома',
  parent: 'Сканы скрыты выше по иерархии',
  edition: 'Сканы скрыты у издания',
  journal: 'Сканы скрыты у журнала',
};

interface Props {
  kind: Kind;
  hidden: boolean;
  hiddenBy?: string;
  onToggle: (next: boolean, reason: string) => Promise<void>;
}

export function ScansHiddenToggle({ kind, hidden, hiddenBy = '', onToggle }: Props) {
  const { user } = useAuth();
  const [busy, setBusy] = useState(false);
  if (!isAdmin(user)) return null;

  const effective = hidden || !!hiddenBy;
  const own = kind === 'chapter' ? 'chapter' : kind === 'work' ? 'work' : '';
  const inherited = effective && hiddenBy && hiddenBy !== own ? INHERITED[hiddenBy] : '';

  const click = async () => {
    const next = !effective;
    setBusy(true);
    try {
      if (
        next &&
        !window.confirm(
          `Скрыть сканы в ${KIND[kind]}? Текст останется доступным. Ранее выданные ссылки и скачанные файлы могут продолжать работать. Если есть статический архив, обновите его.`,
        )
      )
        return;
      const reason = window.prompt('Причина изменения доступа к сканам (обязательно):')?.trim();
      if (!reason) return;
      await onToggle(next, reason);
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="closed-toggle">
      <button type="button" onClick={() => void click()} disabled={busy || !!inherited}>
        {effective ? 'Открыть сканы' : 'Скрыть сканы'}
      </button>
      {inherited && <span className="closed-toggle-note">{inherited}</span>}
    </span>
  );
}
