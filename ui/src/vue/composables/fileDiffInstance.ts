// Vue port of @pierre/diffs' React useFileDiffInstance hook.
//
// The React world renders diffs with <PatchDiff>/<MultiFileDiff>, which under
// the hood instantiate the framework-agnostic FileDiff renderer and hand it the
// shared WorkerPoolManager. When a pool is present, FileDiff tokenizes off the
// main thread and re-renders when the worker returns; without a pool it falls
// back to synchronous shiki highlighting on the main thread.
//
// The original Vue port used @pierre/diffs/ssr (preloadPatchDiff/
// preloadDiffHTML), which ALWAYS runs synchronous WASM tokenization on the main
// thread. In a large conversation (hundreds of diffs) that blocked the main
// thread for seconds in one unbroken task, freezing paint and the loading
// spinner. This composable restores parity with React: drive FileDiff directly
// with the shared worker pool so highlighting happens off-thread.
import { onBeforeUnmount, ref, watch, type Ref } from "vue";
import {
  FileDiff,
  DIFFS_TAG_NAME,
  type FileDiffMetadata,
  type FileDiffOptions,
} from "@pierre/diffs";
import { getDiffsWorkerPool } from "../../services/diffsWorkerPool";

// Importing FileDiff from @pierre/diffs registers the <diffs-container> custom
// element as a side effect (see components/web-components.ts). We create that
// element imperatively rather than in a Vue template so Vue never needs an
// isCustomElement compiler override.

export interface FileDiffInputs {
  fileDiff: FileDiffMetadata;
  options: FileDiffOptions<undefined>;
}

// useFileDiffInstance manages a FileDiff renderer bound to a host element.
// Pass a host element ref (a plain <div>) and a getter for the current
// fileDiff + options; the composable creates a <diffs-container> inside the
// host, hydrates a FileDiff instance against the shared worker pool, and
// re-renders whenever the inputs change.
export function useFileDiffInstance(
  hostEl: Ref<HTMLElement | null>,
  getInputs: () => FileDiffInputs | null,
) {
  let instance: FileDiff<undefined> | null = null;
  let container: HTMLElement | null = null;
  // True once we've successfully hydrated; controls whether the host's
  // fallback content (raw <pre>) should be hidden.
  const rendered = ref(false);

  function teardown() {
    if (instance) {
      instance.cleanUp();
      instance = null;
    }
    if (container) {
      container.remove();
      container = null;
    }
    rendered.value = false;
  }

  function mount(inputs: FileDiffInputs) {
    const host = hostEl.value;
    if (!host) return;
    // Fresh container per (re)mount, mirroring React's ref lifecycle where a
    // new <diffs-container> is created when the node mounts.
    container = document.createElement(DIFFS_TAG_NAME);
    host.appendChild(container);
    // isContainerManaged = true: the framework owns the container element, so
    // FileDiff won't remove it on cleanUp (we remove it ourselves in teardown).
    instance = new FileDiff<undefined>(inputs.options, getDiffsWorkerPool(), true);
    instance.hydrate({ fileDiff: inputs.fileDiff, fileContainer: container });
    rendered.value = true;
  }

  // Track both the host element and the inputs: the host is rendered behind a
  // v-if (collapsed/incomplete sections have no host yet), so it can appear or
  // disappear independently of the diff data. flush: "post" ensures the host
  // ref reflects the latest DOM before we (re)mount.
  watch(
    [hostEl, () => getInputs()] as const,
    ([host, inputs]) => {
      if (!inputs || !host) {
        teardown();
        return;
      }
      if (!instance) {
        mount(inputs);
        return;
      }
      // Existing instance: update options + re-render with the new fileDiff.
      instance.setOptions(inputs.options);
      instance.render({ forceRender: true, fileDiff: inputs.fileDiff });
    },
    { immediate: true, flush: "post" },
  );

  onBeforeUnmount(teardown);

  return { rendered };
}
