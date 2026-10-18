import { expect, test } from "../harness";
import { answerCurrentQuestion, awaitPersona, register } from "./helpers";

// Journey 2 (issue #144): registration with a GitHub handle. The app fetches
// the profile from GITHUB_BASE_URL — the local stub, not the real API — and
// the LLM stub generates the 3 custom questions, so the whole journey is
// hermetic. The interview must complete against the GitHub question set and
// end on the personal page with a persona.
test("GitHub registration via the stubbed GitHub API reaches the personal page", async ({ app, page }) => {
  await register(page, app, { name: "Grace Hopper", github: "octo" });

  // The GitHub question set: the fixed single-selects, one fixed free-text
  // ("What's your go-to language right now?") and the stub's 3 custom
  // questions (free-text). Drive whatever shape shows until the wait page.
  for (let i = 0; i < 20 && !page.url().includes("/user/wait/"); i++) {
    await answerCurrentQuestion(page);
    await page.waitForTimeout(300);
  }
  await expect(page).toHaveURL(/\/user\/wait\//);

  await awaitPersona(page);
});
