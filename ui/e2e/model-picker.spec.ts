import { test, expect } from "@playwright/test";

// The model picker (ChatStatusContent -> ModelPicker.vue) is built on PrimeVue
// <Select>. It renders on the new-conversation screen. Here we exercise the
// PrimeVue-specific open/select behavior, the pinned "Add / Remove Models..."
// and "Refresh Models" footer actions, and persistence of the chosen model to
// localStorage.
test.describe("Model picker (PrimeVue)", () => {
  test("opens, lists models, selecting one persists, footer opens manage modal", async ({
    page,
  }) => {
    test.setTimeout(60000);

    await page.goto("/new");
    await page.waitForLoadState("domcontentloaded");

    const picker = page.locator(".model-picker.p-select");
    await expect(picker).toBeVisible({ timeout: 10000 });

    // Open the overlay.
    await picker.click();
    const panel = page.locator(".model-picker-panel");
    await expect(panel).toBeVisible();

    // At least one model is offered and both footer actions are present.
    const options = panel.locator(".p-select-option");
    expect(await options.count()).toBeGreaterThanOrEqual(1);
    const manageBtn = panel.getByRole("button", { name: "Add / Remove Models..." });
    const refreshBtn = panel.getByRole("button", { name: "Refresh Models" });
    await expect(manageBtn).toBeVisible();
    await expect(refreshBtn).toBeVisible();

    // Pick the first model -> its label shows in the trigger and persists.
    const firstName = (await options
      .first()
      .locator(".model-picker-option-name")
      .textContent())!.trim();
    await options.first().click();
    await expect(panel).toBeHidden();
    await expect(picker.locator(".p-select-label")).toHaveText(firstName);
    expect(await page.evaluate(() => localStorage.getItem("shelley_selected_model"))).toBe(
      firstName,
    );

    // The footer action opens the manage-models modal.
    await picker.click();
    await expect(panel).toBeVisible();
    await panel.getByRole("button", { name: "Add / Remove Models..." }).click();
    await expect(page.getByRole("dialog")).toBeVisible();
  });
});
