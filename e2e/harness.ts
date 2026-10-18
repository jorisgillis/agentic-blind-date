import { execFile, spawn } from "node:child_process";
import { mkdtempSync, rmSync } from "node:fs";
import http from "node:http";
import { tmpdir } from "node:os";
import { join } from "node:path";
import { test as base, expect } from "@playwright/test";
import {
  close,
  createGitHubStub,
  createLLMStub,
  serverURL,
} from "./stubs";

// The app under test: its base URL and the admin handle the journeys use to
// advance the event.
export interface App {
  url: string;
  adminURL: string;
}

const repoRoot = join(__dirname, "..");

// Build the app binary once per test run (memoized: the first test to need it
// builds, the rest reuse). The fixture starts a fresh instance per test: a
// temp SQLite database, a random port, and the GitHub and LLM stubs — so
// every journey runs hermetically, in any worker order.
let buildPromise: Promise<string> | undefined;

export function buildApp(): Promise<string> {
  buildPromise ??= new Promise((resolve, reject) => {
    const dir = mkdtempSync(join(tmpdir(), "abd-e2e-bin-"));
    const bin = join(dir, "agentic-blind-date");
    execFile("go", ["build", "-o", bin, "."], { cwd: repoRoot }, (err) =>
      err ? reject(err) : resolve(bin),
    );
  });
  return buildPromise;
}

function freePort(): Promise<number> {
  return new Promise((resolve, reject) => {
    const srv = http.createServer();
    srv.listen(0, "127.0.0.1", () => {
      const addr = srv.address();
      if (addr === null || typeof addr === "string") {
        reject(new Error("no free port"));
        return;
      }
      const port = addr.port;
      srv.close(() => resolve(port));
    });
  });
}

async function startApp(): Promise<{ app: App; stop: () => Promise<void> }> {
  const binaryPath = await buildApp();
  const githubStub = await createGitHubStub();
  const llmStub = await createLLMStub();
  const port = await freePort();
  const workDir = mkdtempSync(join(tmpdir(), "abd-e2e-"));

  const child = spawn(binaryPath, [], {
    cwd: repoRoot, // the app loads templates/ relative to its working directory
    env: {
      ...process.env,
      PORT: String(port),
      DB_PATH: join(workDir, "blind_date.db"),
      GITHUB_BASE_URL: serverURL(githubStub),
      GITHUB_TOKEN: "e2e-stub-token",
      LLM_PROVIDER: "mistral",
      LLM_API_KEY: "e2e-stub-key",
      LLM_BASE_URL: serverURL(llmStub) + "/v1",
      LOG_LEVEL: process.env.E2E_APP_LOG_LEVEL ?? "debug",
    },
    stdio: ["ignore", "ignore", "pipe"],
  });
  // Surface the app's leveled logs in the Playwright output: E2E failures
  // often live in what the app logged while the browser waited.
  child.stderr?.on("data", (chunk: Buffer) => {
    console.log(`[app:${port}] ${chunk.toString().trimEnd()}`);
  });

  const url = `http://127.0.0.1:${port}`;
  await waitUntilUp(url);

  const stop = async () => {
    child.kill("SIGTERM");
    await new Promise((resolve) => {
      child.once("exit", resolve);
      setTimeout(resolve, 2000).unref?.();
    });
    await close(githubStub);
    await close(llmStub);
    rmSync(workDir, { recursive: true, force: true });
  };

  return {
    app: { url, adminURL: `${url}/admin` },
    stop,
  };
}

async function waitUntilUp(url: string): Promise<void> {
  const deadline = Date.now() + 15_000;
  while (Date.now() < deadline) {
    try {
      const res = await fetch(`${url}/data/state`);
      if (res.ok) return;
    } catch {
      // not up yet
    }
    await new Promise((resolve) => setTimeout(resolve, 200));
  }
  throw new Error(`app did not come up at ${url}`);
}

// The per-test harness fixture: a fresh app, stubs and database for every
// journey, torn down afterwards. Tests get the app's base URL; the app never
// leaks state between journeys.
export const test = base.extend<{ app: App }>({
  app: async ({}, use) => {
    const { app, stop } = await startApp();
    await use(app);
    await stop();
  },
});

export { expect };
