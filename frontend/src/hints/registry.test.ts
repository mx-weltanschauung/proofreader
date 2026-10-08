import { describe, it, expect } from 'vitest';
import { HINTS, HINT_IDS, HINT_APPEAR_MS, HINT_COUNT_MS, HINT_HIDE_MS } from './registry';

describe('реестр подсказок', () => {
  it('ключ карты совпадает с полем id — иначе выноска ищется под чужим именем', () => {
    for (const id of HINT_IDS) expect(HINTS[id].id).toBe(id);
  });

  it('приоритеты различны: порядок показа не должен зависеть от порядка ключей', () => {
    const priorities = HINT_IDS.map((id) => HINTS[id].priority);
    expect(new Set(priorities).size).toBe(priorities.length);
  });

  it('тексты непустые и умещаются в пузырёк', () => {
    for (const id of HINT_IDS) {
      expect(HINTS[id].text.length, id).toBeGreaterThan(10);
      expect(HINTS[id].text.length, id).toBeLessThanOrEqual(90);
    }
  });

  // Показ засчитывается по времени жизни выноски: порог обязан лежать между
  // появлением и автоскрытием, иначе он либо недостижим, либо бессмыслен.
  it('пороги времени согласованы', () => {
    expect(HINT_APPEAR_MS).toBeGreaterThan(0);
    expect(HINT_COUNT_MS).toBeLessThan(HINT_HIDE_MS);
  });
});
