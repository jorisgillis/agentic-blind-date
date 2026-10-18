import type { Page } from "@playwright/test";
import { expect } from "../harness";
import type { App } from "../harness";

// register walks the landing form: with a GitHub handle, or with the
// "no GitHub" checkbox. Returns once the onboard page has loaded.
export async function register(
  page: Page,
  app: App,
  participant: { name: string; github?: string },
): Promise<void> {
  await page.goto(app.url);
  const githubInput = page.locator('input[name="github"]');
  const nameInput = page.locator('input[name="name"]');
  await expect(nameInput).toBeVisible();
  if (participant.github) {
    await githubInput.fill(participant.github);
  } else {
    await page.check('#noGitHubCheckbox');
  }
  await nameInput.fill(participant.name);
  await Promise.all([
    page.waitForURL(/\/user\/onboard\//),
    page.click('#joinForm button[type="submit"]'),
  ]);
}

// completeInterview answers every question the pipeline asks, in order,
// until the app redirects to the personal (wait) page. It needs no knowledge
// of the question set: it drives whatever shape the fragment renders.
export async function completeInterview(page: Page): Promise<void> {
  await page.waitForURL(/\/user\/wait\//, { timeout: 60_000 });
}

// The language multi-select of the no-GitHub interview: every option is a
// labelled checkbox, the counter ticks to n/max, and Continue submits.
export async function answerLanguageMultiSelect(page: Page, pick: string[]): Promise<void> {
  await expect(page.locator("#multiselect-form")).toBeVisible();

  for (const label of pick) {
    await page
      .locator(`#multiselect-form label:has(span:text-is("${label}")) input`)
      .check();
  }
  await expect(page.locator("#selection-counter")).toHaveText(
    `${pick.length}/${pick.length}`,
  );
  await Promise.all([
    page.click("#submit-multiselect"),
    page.waitForResponse(
      (r) => r.url().includes("/user/answer") && r.request().method() === "POST",
    ),
  ]);
  // The answer POST swaps #pipeline-area (outerHTML); wait for the form to be
  // gone so callers never decide against a stale fragment (#188).
  await expect(page.locator("#multiselect-form")).toHaveCount(0);
}

// answerCurrentQuestion drives whichever question shape is showing: the
// single-select buttons, the free-text input, or the multi-select form.
// The pipeline fragment swaps after each answer, and under load a swap can
// land between the shape decision and the action — so the decide-and-answer
// cycle is retried when the DOM moves underneath it, and gives up quietly
// when the interview ends mid-decision (#188).
export async function answerCurrentQuestion(page: Page): Promise<void> {
  for (let attempt = 0; ; attempt++) {
    const area = page.locator("#pipeline-area");

    if (await page.locator("#multiselect-form").count()) {
      if (await tryAnswerMultiSelect(page, attempt)) return;
      continue; // the fragment swapped underneath; re-decide
    }

    const optionButton = area.locator('form button[name="answer"]').first();
    if (await optionButton.count()) {
      try {
        await optionButton.click();
        return;
      } catch {
        if (attempt >= 2) throw new Error("single-select answer never landed");
        continue;
      }
    }

    const input = area.locator('input[name="answer"]').first();
    if (await input.count()) {
      try {
        await input.fill("A stubbed but sincere answer");
        await area.locator("form button").first().click();
        return;
      } catch {
        if (attempt >= 2) throw new Error("free-text answer never landed");
        continue;
      }
    }

    // Nothing recognizable: the fragment is mid-swap, or the interview just
    // ended and the page is redirecting to the wait page.
    if (page.url().includes("/user/wait/")) return;
    if (attempt >= 4) throw new Error("no recognizable question shape appeared");
    await page.waitForTimeout(200);
  }
}

// tryAnswerMultiSelect checks up to three boxes and submits; false means the
// form was swapped out mid-answer and the caller should re-decide.
async function tryAnswerMultiSelect(page: Page, attempt: number): Promise<boolean> {
  const boxes = page.locator("#multiselect-form input[type=checkbox]");
  const count = Math.min(await boxes.count(), 3);
  try {
    for (let i = 0; i < count; i++) {
      await boxes.nth(i).check();
    }
    await page.click("#submit-multiselect");
    return true;
  } catch {
    if (attempt >= 2) throw new Error("multi-select answer never landed");
    return false;
  }
}

// awaitPersona waits for the persona card to carry the stub's persona name —
// the card starts as a placeholder and refreshes when the pipeline finishes.
export async function awaitPersona(page: Page): Promise<void> {
  await expect(page.locator("#persona-card h2")).toHaveText("The Stubbed Soup Stone", {
    timeout: 30_000,
  });
}
