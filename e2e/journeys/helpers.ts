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
  await page.click("#submit-multiselect");
  // Wait for the HTMX swap to replace the form before returning — otherwise
  // answerCurrentQuestion may see the stale form and wait for a submit button
  // that no longer exists.
  await expect(page.locator("#multiselect-form")).toHaveCount(0);
}

// answerCurrentQuestion drives whichever question shape is showing: the
// single-select buttons, the free-text input, or the multi-select form.
export async function answerCurrentQuestion(page: Page): Promise<void> {
  const area = page.locator("#pipeline-area");

  if (await page.locator("#multiselect-form").count()) {
    const boxes = page.locator("#multiselect-form input[type=checkbox]");
    const count = Math.min(await boxes.count(), 3);
    for (let i = 0; i < count; i++) {
      await boxes.nth(i).check();
    }
    await page.click("#submit-multiselect");
    return;
  }

  const optionButton = area.locator('form button[name="answer"]').first();
  if (await optionButton.count()) {
    await optionButton.click();
    return;
  }

  const input = area.locator('input[name="answer"]').first();
  await input.fill("A stubbed but sincere answer");
  await area.locator("form button").first().click();
}

// awaitPersona waits for the persona card to carry the stub's persona name —
// the card starts as a placeholder and refreshes when the pipeline finishes.
export async function awaitPersona(page: Page): Promise<void> {
  await expect(page.locator("#persona-card h2")).toHaveText("The Stubbed Soup Stone", {
    timeout: 30_000,
  });
}
