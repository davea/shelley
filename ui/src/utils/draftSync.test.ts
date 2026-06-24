import { decideDraftSync, NO_SESSION, type DraftSyncState } from "./draftSync";

function assert(cond: boolean, msg: string): void {
  if (!cond) throw new Error(`Assertion failed: ${msg}`);
}
function run(name: string, fn: () => void): void {
  try {
    fn();
    console.log(`\u2713 ${name}`);
  } catch (err) {
    console.error(`\u2717 ${name}`);
    throw err;
  }
}

const base: DraftSyncState = {
  conversationId: null,
  lazyDraftId: null,
  isDraft: false,
  serverDraft: "",
  initializedFor: NO_SESSION,
};

run("seeds an empty draft when entering the new-conversation view", () => {
  const d = decideDraftSync({ ...base });
  assert(d.adopt === true && d.value === "", "adopts empty for new view");
});

run("seeds the server draft text when first entering an existing draft", () => {
  const d = decideDraftSync({
    ...base,
    conversationId: "c1",
    isDraft: true,
    serverDraft: "hello world",
  });
  assert(d.adopt === true && d.value === "hello world", "adopts server draft on entry");
});

run("does NOT clobber local edits with a stale autosave echo (the bug)", () => {
  // 1. Enter draft c1 with server text "hello".
  const first = decideDraftSync({
    ...base,
    conversationId: "c1",
    isDraft: true,
    serverDraft: "hello",
  });
  assert(first.adopt === true && first.value === "hello", "seeded on entry");

  // 2. The user keeps typing locally to "hello there is more". Meanwhile a
  //    PUT autosave round-trips slowly and the server re-emits the row over the
  //    list-patch stream carrying a STALE snapshot ("hello the"). This must be
  //    ignored — otherwise the textarea reverts and deletes the typed tail.
  const echo = decideDraftSync({
    conversationId: "c1",
    lazyDraftId: null,
    isDraft: true,
    serverDraft: "hello the",
    initializedFor: first.initializedFor,
  });
  assert(echo.adopt === false, "stale echo for the same draft is NOT adopted");
});

run("adopts again only on a genuine switch to a different draft", () => {
  const first = decideDraftSync({
    ...base,
    conversationId: "c1",
    isDraft: true,
    serverDraft: "alpha",
  });
  const switched = decideDraftSync({
    conversationId: "c2",
    lazyDraftId: null,
    isDraft: true,
    serverDraft: "beta",
    initializedFor: first.initializedFor,
  });
  assert(switched.adopt === true && switched.value === "beta", "re-seeds on real switch");
});

run("ignores the lazy-draft create snapshot (caret/keystroke preservation)", () => {
  // New view → user types → lazy draft "c9" created → conversationId flips to
  // c9 but the composer is NOT remounted (lazyDraftId === conversationId). The
  // create-time snapshot is stale; must not be adopted.
  const newView = decideDraftSync({ ...base });
  const lazy = decideDraftSync({
    conversationId: "c9",
    lazyDraftId: "c9",
    isDraft: true,
    serverDraft: "hi", // stale create-time snapshot
    initializedFor: newView.initializedFor,
  });
  assert(lazy.adopt === false, "lazy-draft snapshot not adopted");

  // And a later echo for that same lazy draft is also ignored.
  const lazyEcho = decideDraftSync({
    conversationId: "c9",
    lazyDraftId: "c9",
    isDraft: true,
    serverDraft: "hi the",
    initializedFor: lazy.initializedFor,
  });
  assert(lazyEcho.adopt === false, "later lazy echo not adopted");
});

run("entering a non-draft conversation adopts nothing (composer remounts)", () => {
  const d = decideDraftSync({
    ...base,
    conversationId: "c1",
    isDraft: false,
  });
  assert(d.adopt === false, "non-draft entry adopts nothing");
});

console.log("\nAll draftSync tests passed.");
