import { createContext, useContext } from 'react';
import type { ReadingPrefs } from './readingPrefs';

export interface Ctx {
  prefs: ReadingPrefs;
  setPref: <K extends keyof ReadingPrefs>(key: K, value: ReadingPrefs[K]) => void;
  reset: () => void;
}

export const ReadingPreferencesContext = createContext<Ctx | undefined>(undefined);

export const useReadingPrefs = (): Ctx => {
  const ctx = useContext(ReadingPreferencesContext);
  if (!ctx) throw new Error('useReadingPrefs must be used within ReadingPreferencesProvider');
  return ctx;
};
