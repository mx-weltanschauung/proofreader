import { defineConfig } from 'vitest/config';
import react from '@vitejs/plugin-react';

export default defineConfig({
  plugins: [react()],
  test: {
    environment: 'jsdom',
    globals: true,
    setupFiles: ['./src/test/setup.ts'],
    // Фиксирует часовой пояс прогона: без этого дата у полуночи UTC
    // (russianDate.test.ts, «Правлено:» в PageView.test.tsx) печаталась бы
    // соседним днём на машине западнее Гринвича — тест был зелёным только
    // случайно, по часовому поясу разработчика/CI.
    env: { TZ: 'UTC' },
  },
});
