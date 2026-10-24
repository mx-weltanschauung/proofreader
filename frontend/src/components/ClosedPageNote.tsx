/** Плашка на полосе, накрытой закрытой главой, — её видит только сотрудник. */
export function ClosedPageNote({ chapter }: { chapter?: string }) {
  if (!chapter) return null;
  return <div className="closed-page-note">Полоса закрыта вместе с главой «{chapter}»</div>;
}
