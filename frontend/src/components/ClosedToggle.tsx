import { useState } from 'react';
import { useAuth, isAdmin } from '../hooks/useAuth';
import type { ChapterClosedPlan } from '../types';

const CONFIRM = {
  work: 'Закрытый том увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
  edition:
    'Закрытое собрание увидят только редакторы. Если оно есть в выложенном статическом архиве, пересоберите и выложите архив.',
  journal:
    'Закрытый журнал увидят только редакторы. Если он есть в выложенном статическом архиве, пересоберите и выложите архив.',
} as const;

const REASON_PROMPT = 'Причина (обязательно): письмо, от кого, о чём';

/** Откуда пришло закрытие работы; пусто и «work» — закрыта сама, её можно открыть здесь. */
const INHERITED: Record<string, string> = {
  edition: 'закрыт вместе с изданием',
  journal: 'закрыт вместе с журналом',
  parent: 'закрыт вместе с томом',
};

/** У главы «parent» — глава выше, а не том. */
const INHERITED_CHAPTER: Record<string, string> = {
  parent: 'закрыта вместе с главой выше',
};

function planText(plan: ChapterClosedPlan): string {
  let text = `Закроются: глав — ${plan.chapters}, полос — ${plan.pages}.`;
  const neighbors = [...new Set(plan.shared.flatMap((s) => s.neighbors))];
  if (neighbors.length > 0) {
    text += ` На общих полосах потеряют кусок: ${neighbors.join(', ')}.`;
  }
  return text;
}

interface Props {
  closed: boolean;
  /** Откуда закрытие: у работы `''|'work'|'parent'|'edition'|'journal'`, у главы `''|'chapter'|'parent'`. */
  closedBy?: string;
  onToggle: (next: boolean, reason: string) => Promise<void>;
  /** Что закрываем — от этого зависит текст подтверждения; по умолчанию том. */
  kind?: keyof typeof CONFIRM | 'chapter';
  /** Только для главы: план закрытия, который показывается перед вопросом о причине. */
  loadPlan?: () => Promise<ChapterClosedPlan>;
}

/** Кнопка «Закрыть»/«Открыть»; рисуется только администратору, причина обязательна. */
export function ClosedToggle({ closed, closedBy = '', onToggle, kind = 'work', loadPlan }: Props) {
  const { user } = useAuth();
  const [busy, setBusy] = useState(false);
  if (!isAdmin(user)) return null;

  const inherited = closed
    ? (kind === 'chapter' ? INHERITED_CHAPTER : INHERITED)[closedBy]
    : undefined;

  const click = async () => {
    const next = !closed;
    setBusy(true);
    try {
      if (next) {
        let question: string;
        if (kind === 'chapter') {
          if (!loadPlan) return;
          question = planText(await loadPlan());
        } else {
          question = CONFIRM[kind];
        }
        if (!window.confirm(question)) return;
      }
      const reason = window.prompt(REASON_PROMPT)?.trim();
      if (!reason) return;
      await onToggle(next, reason);
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
