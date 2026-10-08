import React, { useCallback, useEffect, useState } from 'react';
import { ReadingPrefs, DEFAULT_PREFS, loadPrefs, savePrefs, resolveTheme } from './readingPrefs';
import { ReadingPreferencesContext } from './readingPrefsContext';

/** Everything except the theme maps straight to a data- attribute. */
const ATTRS: Array<[keyof ReadingPrefs, string]> = [
  ['fontFamily', 'data-reading-font'],
  ['lineHeight', 'data-reading-line'],
  ['measure', 'data-reading-measure'],
  ['spacing', 'data-reading-spacing'],
  ['paragraph', 'data-reading-paragraph'],
  ['align', 'data-reading-align'],
];

export const ReadingPreferencesProvider: React.FC<{ children: React.ReactNode }> = ({
  children,
}) => {
  const [prefs, setPrefs] = useState<ReadingPrefs>(() => loadPrefs(localStorage));

  const setPref = useCallback(<K extends keyof ReadingPrefs>(key: K, value: ReadingPrefs[K]) => {
    setPrefs((prev) => {
      const next = { ...prev, [key]: value };
      savePrefs(localStorage, next);
      return next;
    });
  }, []);

  const reset = useCallback(() => {
    savePrefs(localStorage, DEFAULT_PREFS);
    setPrefs(DEFAULT_PREFS);
  }, []);

  // The theme is the one preference that needs resolving against the OS,
  // and the one that must keep tracking it while set to 'auto'.
  useEffect(() => {
    const mql = window.matchMedia('(prefers-color-scheme: dark)');
    const apply = () =>
      document.documentElement.setAttribute('data-theme', resolveTheme(prefs.theme, mql.matches));
    apply();
    if (prefs.theme !== 'auto') return;
    mql.addEventListener('change', apply);
    return () => mql.removeEventListener('change', apply);
  }, [prefs.theme]);

  useEffect(() => {
    const root = document.documentElement;
    for (const [key, attr] of ATTRS) root.setAttribute(attr, String(prefs[key]));
    root.style.setProperty('--reading-size', `${prefs.fontSize}px`);
  }, [prefs]);

  return (
    <ReadingPreferencesContext.Provider value={{ prefs, setPref, reset }}>
      {children}
    </ReadingPreferencesContext.Provider>
  );
};
