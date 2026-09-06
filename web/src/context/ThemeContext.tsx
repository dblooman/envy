import React, { createContext, useContext, useEffect, useState } from 'react'

type Theme = 'light' | 'dark' | 'system'

interface ThemeContextType {
  theme: Theme
  setTheme: (theme: Theme) => void
  isDark: boolean
}

const ThemeContext = createContext<ThemeContextType | undefined>(undefined)

const THEME_KEY = 'envy_ui_theme'

export function ThemeProvider({ children }: { children: React.ReactNode }) {
  const [theme, setTheme] = useState<Theme>(() => {
    const saved = localStorage.getItem(THEME_KEY) as Theme | null
    if (saved && (saved === 'light' || saved === 'dark' || saved === 'system')) {
      return saved
    }
    return 'light' // Default to crisp light mode
  })

  const [isDark, setIsDark] = useState(false)

  useEffect(() => {
    const root = document.documentElement
    const mediaQuery = window.matchMedia('(prefers-color-scheme: dark)')

    const applyTheme = () => {
      const systemDark = mediaQuery.matches
      const activeDark = theme === 'dark' || (theme === 'system' && systemDark)

      setIsDark(activeDark)
      if (activeDark) {
        root.classList.add('dark')
      } else {
        root.classList.remove('dark')
      }
    }

    applyTheme()
    localStorage.setItem(THEME_KEY, theme)

    const handler = () => {
      if (theme === 'system') {
        applyTheme()
      }
    }

    mediaQuery.addEventListener('change', handler)
    return () => mediaQuery.removeEventListener('change', handler)
  }, [theme])

  return (
    <ThemeContext.Provider value={{ theme, setTheme, isDark }}>
      {children}
    </ThemeContext.Provider>
  )
}

export function useTheme() {
  const ctx = useContext(ThemeContext)
  if (!ctx) {
    throw new Error('useTheme must be used within a ThemeProvider')
  }
  return ctx
}
