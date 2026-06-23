<!-- Renders a self-contained HTML string inside an open shadow root so any
     <style> the markup carries is scoped to it and cannot leak into the page.

     This exists for @pierre/diffs SSR output (PatchTool.vue): preloadPatchDiff /
     preloadDiffHTML return prerendered HTML that embeds its own <style> blocks
     (a :host theme layer plus a `pre, code { display: block }` reset in
     @layer base). React's SSR wrapper puts that HTML inside a
     `<template shadowrootmode="open">` (declarative shadow DOM), which the
     parser turns into a real shadow root that scopes those styles. Vue's
     v-html sets innerHTML, and innerHTML deliberately does NOT instantiate
     declarative shadow DOM, so the same markup would dump the diff's reset into
     the light DOM and break every <code> on the page (e.g. the git-info commit
     hash). Attaching the shadow root ourselves restores React parity. -->
<template>
  <div ref="hostEl"></div>
</template>

<script setup lang="ts">
import { onMounted, ref, watch } from "vue";

const props = defineProps<{ html: string }>();

const hostEl = ref<HTMLDivElement | null>(null);
let shadow: ShadowRoot | null = null;

function render() {
  if (!hostEl.value) return;
  // attachShadow throws if called twice; reuse the root once created.
  if (!shadow) shadow = hostEl.value.attachShadow({ mode: "open" });
  shadow.innerHTML = props.html;
}

onMounted(render);
watch(() => props.html, render);
</script>
