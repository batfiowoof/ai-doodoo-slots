"use client";

import { createContext, useContext, useState } from "react";

// A generated theme: 8 palette colors (index 0 is transparency) and 8
// sprites (16x16 hex-indexed grids), ordered common to rare to match the
// symbol tables. Chrome colors stay fixed so the cabinet reads as one
// machine across themes; only symbol colors change.
export interface ActiveTheme {
  id: number;
  name: string;
  palette: string[];
  sprites: { name: string; rows: string[] }[];
}

interface ThemeContextValue {
  active: ActiveTheme | null;
  setActive: (t: ActiveTheme | null) => void;
}

const ThemeContext = createContext<ThemeContextValue>({
  active: null,
  setActive: () => {},
});

export function useTheme(): ActiveTheme | null {
  return useContext(ThemeContext).active;
}

export function useSetTheme(): (t: ActiveTheme | null) => void {
  return useContext(ThemeContext).setActive;
}

/**
 * Holds the active theme above every symbol renderer. Themes are only ever
 * applied from server-validated responses — a malformed generation is
 * rejected before storage and never reaches here, so the machine keeps its
 * previous theme.
 */
export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [active, setActive] = useState<ActiveTheme | null>(null);
  return (
    <ThemeContext.Provider value={{ active, setActive }}>
      {children}
    </ThemeContext.Provider>
  );
}
