/** Numeric journal labels need №; written-out labels are complete already. */
export function issueDesignation(label: string): string {
  const trimmed = label.trim();
  return /^\p{Nd}/u.test(trimmed) ? `№ ${trimmed}` : trimmed;
}
