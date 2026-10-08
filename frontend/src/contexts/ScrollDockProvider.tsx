import React, { useMemo, useState } from 'react';
import { ScrollDockContext, type DockAction } from './scrollDockContext';

export const ScrollDockProvider: React.FC<{ children: React.ReactNode }> = ({ children }) => {
  const [action, setAction] = useState<DockAction | null>(null);
  // setAction от useState стабилен, поэтому значение меняется только вместе
  // с самим действием — потребители не перерисовываются на каждый рендер
  // провайдера.
  const value = useMemo(() => ({ action, setAction }), [action]);
  return <ScrollDockContext.Provider value={value}>{children}</ScrollDockContext.Provider>;
};
