import { useState } from 'react';

const CONFIRM = {
  work: 'Закрытый том увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
  edition:
    'Закрытое собрание увидят только редакторы. Если оно есть в выложенном статическом архиве, пересоберите и выложите архив.',
  journal:
    'Закрытый журнал увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
} as const;

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
  /** Что закрываем — от этого зависит текст подтверждения; по умолчанию том. */
  kind?: keyof typeof CONFIRM;
}

export function ClosedToggle({ closed, closedBy = '', onToggle, kind = 'work' }: Props) {
  const [busy, setBusy] = useState(false);
  const inherited = closed ? INHERITED[closedBy] : undefined;

  const click = async () => {
    const next = !closed;
    if (next && !window.confirm(CONFIRM[kind])) return;
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
