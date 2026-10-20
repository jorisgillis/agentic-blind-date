import http from "node:http";

// The GitHub API stub: one canned user (any handle) with repos and mutual
// follows, so Journey 2 registers against a hermetic local upstream.
const user = {
  login: "octo",
  name: "Octo Cat",
  bio: "Tentacles in the code",
  company: "@github",
  location: "The Sea",
  created_at: "2014-01-01T00:00:00Z",
  public_repos: 7,
  followers: 42,
};

const repos = [
  {
    name: "gopher",
    description: "Go things",
    stargazers_count: 10,
    language: "Go",
    fork: false,
    topics: ["cli", "go"],
  },
  {
    name: "crab",
    description: "Rust things",
    stargazers_count: 5,
    language: "Rust",
    fork: false,
    topics: ["cli"],
  },
];

export function createGitHubStub(): Promise<http.Server> {
  const server = http.createServer((req, res) => {
    const path = req.url ?? "";
    if (path.startsWith("/users/") && path.endsWith("/repos")) {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(repos));
      return;
    }
    if (path.startsWith("/users/") && path.includes("/following/")) {
      res.writeHead(204); // everyone follows everyone in the stub
      res.end();
      return;
    }
    if (path.startsWith("/users/")) {
      res.writeHead(200, { "Content-Type": "application/json" });
      res.end(JSON.stringify(user));
      return;
    }
    res.writeHead(404);
    res.end("not found");
  });
  return listen(server);
}

// The LLM stub classifies each chat request by its system prompt and answers
// with the canned JSON that caller validates. Unknown callers (the Mission
// Agent, the show & tell host) get `{}`, which fails validation and falls
// back — a 200 with a bad payload is a permanent error, so nothing retries.
function cannedReply(system: string): string {
  if (system.includes("personality generator")) {
    return JSON.stringify({
      name: "The Stubbed Soup Stone",
      tagline: "Canned but committed.",
    });
  }
  if (system.includes("interviewer")) {
    return JSON.stringify({
      questions: [
        "Stubbed question one?",
        "Stubbed question two?",
        "Stubbed question three?",
      ],
    });
  }
  if (system.includes("matchmaker")) {
    return JSON.stringify({
      score: 77,
      reason: "Both stubs, same vibe.",
      red_flags: [],
      green_flags: ["You are both here tonight"],
      icebreakers: ["Stubbed icebreaker?"],
    });
  }
  return "{}";
}

// A small delay per LLM call so the agent pipeline is observable in E2E:
// without it the stubs answer instantly and the pipeline finishes before
// the browser can render the onboarding spinner.
const LLM_DELAY_MS = 500;

export function createLLMStub(): Promise<http.Server> {
  const server = http.createServer((req, res) => {
    if (req.method !== "POST" || !(req.url ?? "").endsWith("/chat/completions")) {
      res.writeHead(404);
      res.end("not found");
      return;
    }
    let body = "";
    req.on("data", (chunk) => (body += chunk));
    req.on("end", () => {
      let system = "";
      try {
        const parsed = JSON.parse(body);
        const msg = parsed.messages?.find((m: { role: string }) => m.role === "system");
        system = msg?.content ?? "";
      } catch {
        system = "";
      }
      setTimeout(() => {
        res.writeHead(200, { "Content-Type": "application/json" });
        res.end(
          JSON.stringify({
            choices: [{ message: { content: cannedReply(system) } }],
          }),
        );
      }, LLM_DELAY_MS);
    });
  });
  return listen(server);
}

export function listen(server: http.Server): Promise<http.Server> {
  return new Promise((resolve) => server.listen(0, "127.0.0.1", () => resolve(server)));
}

export function serverURL(server: http.Server): string {
  const addr = server.address();
  if (addr === null || typeof addr === "string") {
    throw new Error("stub server has no port");
  }
  return `http://127.0.0.1:${addr.port}`;
}

export function close(server: http.Server): Promise<void> {
  return new Promise((resolve) => server.close(() => resolve()));
}
