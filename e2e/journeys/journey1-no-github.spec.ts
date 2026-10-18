import { expect, test } from "../harness";
import {
  answerCurrentQuestion,
  answerLanguageMultiSelect,
  awaitPersona,
  register,
} from "./helpers";

// Journey 1 (issue #144): the no-GitHub registration, at browser level — the
// #141 regression class. The first question must show all 10 language options
// as labelled checkboxes; the counter ticks to 3/3; Continue works; the
// remaining shapes (single-select, free-text) all submit; the journey ends on
// the personal page with a persona.
test("no-GitHub registration reaches the personal page with a persona", async ({ app, page }) => {
  await register(page, app, { name: "Ada NoGH" });

  // The first question: the language multi-select with all 10 options.
  const languages = [
    "Go", "Python", "JavaScript", "TypeScript", "Java",
    "C#", "C++", "PHP", "Rust", "Swift",
  ];
  await expect(page.locator("#pipeline-area")).toContainText(
    "top 3 programming languages",
    { timeout: 15_000 },
  );
  const checkboxes = page.locator("#multiselect-form input[type=checkbox]");
  await expect(checkboxes).toHaveCount(10);
  for (const lang of languages) {
    await expect(
      page.locator(`#multiselect-form label:has(span:text-is("${lang}"))`),
    ).toBeVisible();
  }

  // Selecting 3 ticks the counter to 3/3 and enables Continue — the #141 bug
  // was a form that rendered but could not be used.
  await answerLanguageMultiSelect(page, ["Go", "Rust", "Swift"]);

  // The remaining questions: single-select and free-text shapes, in whatever
  // order the question set asks them, until the wait page takes over.
  for (let i = 0; i < 10 && !page.url().includes("/user/wait/"); i++) {
    await answerCurrentQuestion(page);
    await page.waitForTimeout(300); // the HTMX swap settles before the next look
  }
  await expect(page).toHaveURL(/\/user\/wait\//);

  await awaitPersona(page);
});
