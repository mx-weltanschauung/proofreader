import './ClosedBadge.css';

/** Пометка «закрыт» — её видит только сотрудник: аноним закрытой записи не получит. */
export function ClosedBadge() {
  return (
    <span className="closed-badge" title="Видят только редакторы">
      закрыт
    </span>
  );
}
