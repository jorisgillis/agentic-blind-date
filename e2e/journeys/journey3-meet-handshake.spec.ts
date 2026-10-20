import { expect, test } from "../harness";
import { answerCurrentQuestion, awaitPersona, register } from "./helpers";

// Journey 3 (issue #144, stretch): two participants register, the host
// advances the evening to the social, and both confirm the meeting with each
// other's meet codes. The completed meeting appears on both personal pages.
test("two participants complete the meet handshake with meet codes", async ({ app, browser }) => {
  const alice = await browser.newContext();
  const bob = await browser.newContext();
  const alicePage = await alice.newPage();
  const bobPage = await bob.newPage();

  for (const [page, participant] of [
    [alicePage, { name: "Alice Stub", github: "octo" }],
    [bobPage, { name: "Bob Stub" }],
  ] as const) {
    await register(page, app, participant);
    for (let i = 0; i < 20 && !page.url().includes("/user/wait/"); i++) {
      await answerCurrentQuestion(page);
      await page.waitForTimeout(300);
    }
    await expect(page).toHaveURL(/\/user\/wait\//);
    // Wait for the persona pipeline to finish before advancing — with a
    // delayed LLM stub the persona lands after the wait page appears.
    await awaitPersona(page);
  }

  // The host advances the evening to the social: meeting confirmation opens,
  // and the personal pages' streams redirect both participants to their
  // match pages.
  const res = await alicePage.request.post(`${app.adminURL}/phase`, {
    form: { to: "social" },
  });
  expect(res.ok()).toBeTruthy();

  // Both match pages now show their meet code (the stream refreshes the
  // section; no reload needed — reloading races the stream's redirect).
  for (const page of [alicePage, bobPage]) {
    await expect(page.locator("#meet-status")).toContainText("Your meet code", {
      timeout: 15_000,
    });
  }
  const aliceCode = (await alicePage.locator("#meet-status .font-mono").first().innerText()).trim();
  const bobCode = (await bobPage.locator("#meet-status .font-mono").first().innerText()).trim();
  expect(aliceCode).toMatch(/^[A-Z2-9]{6}$/);
  expect(bobCode).toMatch(/^[A-Z2-9]{6}$/);
  expect(aliceCode).not.toBe(bobCode);

  // Alice confirms first: recorded, waiting for Bob. The meeting appears in
  // her meetings list — the stream refreshes the whole meet section, so the
  // transient #meet-result swap is not the durable place to assert.
  await alicePage.locator('#meet-status input[type="text"][name="code"]').fill(bobCode);
  await alicePage.locator('#meet-status form:has(input[type="text"][name="code"]) button[type="submit"]').click();
  await expect(alicePage.locator("#meet-status")).toContainText(
    "Pending — waiting for them to confirm",
    { timeout: 15_000 },
  );

  // Bob confirms back with the Confirm button his page now shows for the
  // awaiting meeting — the natural journey, no code typing needed.
  await expect(bobPage.locator("#meet-status")).toContainText("confirm back", {
    timeout: 15_000,
  });
  await bobPage.locator('#meet-status button:has-text("Confirm")').click();

  // The completed meeting shows on both match pages: the stream refreshes
  // the meet section when the meeting completes.
  await expect(alicePage.locator("#meet-status")).toContainText("You met!", {
    timeout: 15_000,
  });
  await expect(bobPage.locator("#meet-status")).toContainText("You met!", {
    timeout: 15_000,
  });

  await alice.close();
  await bob.close();
});
