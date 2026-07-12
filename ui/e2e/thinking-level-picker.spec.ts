import { test, expect } from "@playwright/test";

// The reasoning-effort picker (ChatStatusContent -> ThinkingLevelPicker.vue)
// is built on PrimeVue <Select>. It renders on the new-conversation screen.
// Here we exercise the PrimeVue-specific open/select behavior and confirm the
// choice persists to localStorage (shelley.thinkingLevel.v2).
test.describe("Thinking level picker (PrimeVue)", () => {
  test("opens, lists levels, and selecting one persists the choice", async ({ page }) => {
    test.setTimeout(60000);

    await page.goto("/new");
    await page.waitForLoadState("domcontentloaded");

    // Reset persisted level so assertions are deterministic across workers.
    await page.evaluate(() => localStorage.removeItem("shelley.thinkingLevel.v2"));
    await page.reload();
    await page.waitForLoadState("domcontentloaded");

    const picker = page.locator(".thinking-level-picker.p-select");
    await expect(picker).toBeVisible({ timeout: 10000 });

    // Open the PrimeVue Select overlay.
    await picker.click();
    const panel = page.locator(".thinking-level-picker-panel");
    await expect(panel).toBeVisible();

    // Default plus all six explicit reasoning levels are offered.
    const options = panel.locator(".p-select-option");
    await expect(options).toHaveCount(7);

    // Pick "high" -> label updates and choice is persisted.
    await panel
      .locator(".p-select-option")
      .filter({ hasText: /^high$/ })
      .click();
    await expect(panel).toBeHidden();
    await expect(picker.locator(".p-select-label")).toHaveText("high");
    expect(await page.evaluate(() => localStorage.getItem("shelley.thinkingLevel.v2"))).toBe("high");
  });
});
