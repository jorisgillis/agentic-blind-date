import { defineConfig } from "@playwright/test";

export default defineConfig({
  testDir: "./journeys",
  timeout: 60_000,
  // Each test gets its own app instance from the harness fixture (fresh
  // database and stubs), so workers can run in parallel safely.
  workers: process.env.CI ? 2 : undefined,
  fullyParallel: true,
  reporter: process.env.CI ? "line" : "list",
  use: {
    headless: true,
    // Generous waits: the app's agent pipeline is background work driven by
    // SSE streams, not instant redirects.
    actionTimeout: 10_000,
    navigationTimeout: 15_000,
  },
});
