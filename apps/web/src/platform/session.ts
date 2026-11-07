import type { StateStorage } from 'zustand/middleware'

export const sessionStore: StateStorage = {
  getItem: (name) => window.sessionStorage.getItem(name),
  setItem: (name, value) => window.sessionStorage.setItem(name, value),
  removeItem: (name) => window.sessionStorage.removeItem(name),
}
