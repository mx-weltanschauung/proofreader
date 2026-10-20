/**
 * Номер тома, который можно показать читателю.
 *
 * У собрания с условной нумерацией (`Edition.volume_numbering === 'order'`:
 * стенограммы пленумов разных лет без общей нумерации) номер тома выдуман
 * ради порядка на полке. Цитата «…, т. 24, с. 37» отправила бы читателя
 * искать в библиотеке том, которого нет в природе. Сервер отдаёт у такого
 * тома `volume_order_only: true`, а номер оставляет — по нему стоит полка и
 * его правит редактор.
 *
 * Все подписи тома для читателя («т. N», «Том N», клеймо на корешке) берут
 * номер отсюда, а не из `volume_number` напрямую: одна проверка вместо
 * десятка разбросанных. Порядок и уникальность по-прежнему читают
 * `volume_number`.
 */
export interface VolumeCoordinates {
  volume_number?: number | null;
  volume_part?: string | null;
  volume_order_only?: boolean;
}

/** Показываемый номер тома; `undefined` — номера нет или он условный. */
export function shownVolumeNumber(v: VolumeCoordinates): number | undefined {
  if (v.volume_order_only) return undefined;
  return v.volume_number ?? undefined;
}

/** Часть тома при показанном номере: без номера часть одна ничего не значит. */
export function shownVolumePart(v: VolumeCoordinates): string | undefined {
  if (shownVolumeNumber(v) === undefined) return undefined;
  return v.volume_part ?? undefined;
}
