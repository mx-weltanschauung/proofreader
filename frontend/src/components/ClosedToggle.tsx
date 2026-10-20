import { useState } from 'react';

const CONFIRM =
  'Закрытый том увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.';

/** Откуда пришло закрытие работы; пусто и «work» — закрыта сама, её можно открыть здесь. */
const INHERITED: Record<string, string> = {
  edition: 'закрыт вместе с изданием',
  journal: 'закрыт вместе с журналом',
  parent: 'закрыт вместе с томом',
};

interface Props {
  closed: boolean;
  /** Только у работы: `''|'work'|'parent'|'edition'|'journal'`. */
  closedBy?: string;
  onToggle: (next: boolean) => Promise<void>;
}

export function ClosedToggle({ closed, closedBy = '', onToggle }: Props) {
  const [busy, setBusy] = useState(false);
  const inherited = closed ? INHERITED[closedBy] : undefined;

  const click = async () => {
    const next = !closed;
    if (next && !window.confirm(CONFIRM)) return;
    setBusy(true);
    try {
      await onToggle(next);
    } finally {
      setBusy(false);
    }
  };

  return (
    <span className="closed-toggle">
      <button type="button" onClick={() => void click()} disabled={busy || !!inherited}>
        {closed ? 'Открыть' : 'Закрыть'}
      </button>
      {inherited && <span className="closed-toggle-note">{inherited}</span>}
    </span>
  );
}
