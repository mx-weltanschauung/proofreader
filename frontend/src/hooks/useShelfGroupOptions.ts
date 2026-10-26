import { useEffect, useState } from 'react';
import { shelfApi } from '../services/api';

/**
 * Уже заведённые разделы главной — подсказка для поля «Раздел на главной»:
 * имя раздела вписывается руками, и опечатка завела бы второй раздел рядом с
 * первым. Берутся из той же полки, что рисует главная, у собраний и у
 * организаций; по алфавиту. Полка не прочиталась — подсказки нет, форма
 * работает без неё.
 */
export function useShelfGroupOptions(): string[] {
  const [options, setOptions] = useState<string[]>([]);

  useEffect(() => {
    let alive = true;
    shelfApi.get().then(
      ({ data }) => {
        const names = new Set<string>();
        for (const { edition } of data.editions) names.add((edition.shelf_group ?? '').trim());
        for (const org of data.organizations ?? []) names.add((org.shelf_group ?? '').trim());
        names.delete('');
        if (alive) setOptions([...names].sort((a, b) => a.localeCompare(b, 'ru')));
      },
      () => undefined,
    );
    return () => {
      alive = false;
    };
  }, []);

  return options;
}
