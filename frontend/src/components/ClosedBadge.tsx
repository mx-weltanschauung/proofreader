import './ClosedBadge.css';

const DATE = new Intl.DateTimeFormat('ru-RU', {
  timeZone: 'Europe/Moscow',
  day: '2-digit',
  month: '2-digit',
  year: 'numeric',
});

/** Последнее закрытие: дата всегда, причина — только если сервер её отдал (администратору). */
export interface BadgeClosure {
  at: string;
  reason: string;
}

/** Пометка «закрыт» — её видит только сотрудник: аноним закрытой записи не получит. */
export function ClosedBadge({ closure }: { closure?: BadgeClosure | null }) {
  const when = closure ? new Date(closure.at) : null;
  if (!closure || !when || Number.isNaN(when.getTime())) {
    return (
      <span className="closed-badge" title="Видят только редакторы">
        закрыт
      </span>
    );
  }
  return (
    <span className="closed-badge" title="Видят только редакторы">
      закрыто {DATE.format(when)}
      {closure.reason ? `: ${closure.reason}` : ''}
    </span>
  );
}
