import { test, expect } from "@playwright/test";
import { createConversationViaAPI } from "../helpers";

// Vue-only: the base Modal.vue is backed by PrimeVue Dialog in the Vue world
// (focus trap, role="dialog"/aria-modal, mask + Escape owned by PrimeVue) but
// is still a hand-rolled Teleport div in React, so the behaviours below
// diverge. See components/Modal.vue. The DOM/ARIA contract that the
// cross-world specs rely on (.modal-overlay mask, .modal panel, .modal-header
// .btn-icon close, .modal-body) is preserved via the #container slot and the
// mask passthrough; here we exercise the PrimeVue-specific guarantees.
//
// The Feature flags modal is the simplest consumer of the base Modal, and is
// reachable via the command palette (Cmd/Ctrl+K -> "feature").
test.describe("Base modal (PrimeVue Dialog)", () => {
  test("opens via palette, traps focus, and closes via X / Escape / backdrop", async ({
    page,
    request,
  }) => {
    test.setTimeout(60000);

    const slug = await createConversationViaAPI(request, "Hello");
    await page.goto(`/c/${slug}`);
    await page.waitForLoadState("domcontentloaded");

    const openFeatureFlags = async () => {
      // Cmd/Ctrl+K opens the command palette; type to filter to "Feature flags".
      await page.keyboard.press("ControlOrMeta+k");
      const search = page.locator(".command-palette-input");
      await expect(search).toBeVisible();
      await search.fill("feature");
      const item = page.locator(".command-palette-item").first();
      await expect(item).toBeVisible();
      await item.click();
    };

    // --- Open + contract / a11y ---
    await openFeatureFlags();

    const mask = page.locator(".modal-overlay");
    const panel = page.locator(".modal");
    await expect(mask).toBeVisible();
    await expect(panel).toBeVisible();
    await expect(page.locator(".modal-title")).toHaveText("Feature flags");

    // PrimeVue Dialog gives us proper dialog semantics on the wrapper around
    // our .modal panel.
    const dialog = page.getByRole("dialog");
    await expect(dialog).toHaveAttribute("aria-modal", "true");

    // The dialog has an accessible name: aria-labelledby must resolve to the
    // real .modal-title (not PrimeVue's default, never-rendered _header id).
    const labelledBy = await dialog.getAttribute("aria-labelledby");
    expect(labelledBy).toBeTruthy();
    await expect(page.locator(`#${labelledBy}`)).toHaveText("Feature flags");
    await expect(page.locator(`#${labelledBy}`)).toHaveClass(/modal-title/);

    // Focus moves into the dialog on open (so the focus trap engages).
    await expect
      .poll(() =>
        page.evaluate(() => document.querySelector(".modal")?.contains(document.activeElement)),
      )
      .toBe(true);

    // The mask must carry the legacy .modal-overlay class (passthrough), and
    // the close button must keep its aria-label contract.
    await expect(page.locator(".modal-header .btn-icon[aria-label='Close modal']")).toBeVisible();

    // --- Close via the X button ---
    await page.locator(".modal-header .btn-icon[aria-label='Close modal']").click();
    await expect(panel).toHaveCount(0);

    // --- Close via Escape (PrimeVue document-level handler) ---
    await openFeatureFlags();
    await expect(panel).toBeVisible();
    await page.keyboard.press("Escape");
    await expect(panel).toHaveCount(0);

    // --- Close via backdrop / dismissable mask click ---
    await openFeatureFlags();
    await expect(panel).toBeVisible();
    // Click the mask at a corner well outside the centered panel.
    await mask.click({ position: { x: 5, y: 5 } });
    await expect(panel).toHaveCount(0);
  });
});
