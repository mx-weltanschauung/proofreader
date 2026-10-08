export type VolumeView = 'spines' | 'list';

const KEY = 'volume-view';

/**
 * Вид списка томов. Ключ один на все собрания: выбор — про привычку читателя.
 *
 * Обращение к хранилищу — внутри try, как и в useReadingProgress: в приватном
 * окне и во вложенном контексте с запретом на хранилище getItem бросает.
 * Чтение идёт из инициализатора состояния страницы собрания, то есть во время
 * рендера, — исключение отсюда оставило бы карточку собрания пустой.
 */
export function readVolumeView(): VolumeView {
  try {
    return localStorage.getItem(KEY) === 'list' ? 'list' : 'spines';
  } catch {
    return 'spines';
  }
}

export function writeVolumeView(view: VolumeView): void {
  try {
    localStorage.setItem(KEY, view);
  } catch {
    // Хранилище недоступно или переполнено. Запомненный вид — удобство,
    // ради которого не стоит ронять переключение.
  }
}
