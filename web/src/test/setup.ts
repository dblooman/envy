// Use browser storage even when Node exposes its own localStorage accessor.
const storage = (
  globalThis as typeof globalThis & { jsdom: { window: Window } }
).jsdom.window.localStorage;
Object.defineProperty(globalThis, "localStorage", {
  configurable: true,
  value: storage,
  writable: true,
});
