import React from 'react';
import { Header } from './Header';
import { Footer } from './Footer';
import { PlayerBar } from './PlayerBar';
import { ScrollDock, TOP_FOCUS_ID } from './ScrollDock';
import { ScrollDockProvider } from '../contexts/ScrollDockProvider';
import { HintsProvider } from '../contexts/HintsProvider';
import './Layout.css';

interface LayoutProps {
  children: React.ReactNode;
}

export const Layout: React.FC<LayoutProps> = ({ children }) => {
  return (
    <HintsProvider>
      <ScrollDockProvider>
        <div className="app-layout">
          <Header />
          {/* tabIndex={-1}: сюда уходит фокус после прыжка наверх, программно —
              в обход по Tab <main> при этом не попадает. */}
          <main id={TOP_FOCUS_ID} tabIndex={-1} className="main-content">
            {children}
          </main>
          <Footer />
          {/* Вне .main-content — по смыслу, а не по вёрстке: блок принадлежит
              всей странице, а не её содержимому, и в оглавлении документа ему
              внутри <main> не место. Стилей, из-за которых он не мог бы там
              находиться, у .main-content нет. */}
          <ScrollDock />
          {/* Здесь, а не в странице главы: Layout переживает смену маршрута,
              и звук не обрывается при переходах по читальне. */}
          <PlayerBar />
        </div>
      </ScrollDockProvider>
    </HintsProvider>
  );
};
