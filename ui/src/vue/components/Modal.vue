<!-- Vue port of components/Modal.tsx, now backed by PrimeVue Dialog for focus
     trapping, mask/stacking, and accessible role="dialog" semantics. The
     #container slot reproduces the legacy markup so the DOM/ARIA contract is
     unchanged: classes (.modal-overlay on the mask, .modal, .modal-header,
     .modal-title, .modal-title-right, .modal-body, .btn-icon) and the
     aria-label "Close modal". PrimeVue owns Escape + backdrop-click closing.
     Use the #title-right slot for titleRight. -->
<template>
  <Dialog
    :visible="isOpen"
    modal
    :dismissable-mask="true"
    :close-on-escape="true"
    :show-header="false"
    append-to="body"
    :pt="{ root: { class: 'modal-dialog-root' }, mask: { class: 'modal-overlay' } }"
    :aria-labelledby="titleId"
    @update:visible="onVisibleChange"
    @show="onShow"
  >
    <template #container>
      <div ref="panelRef" :class="['modal', className]" role="document" tabindex="-1">
        <div class="modal-header">
          <h2 :id="titleId" class="modal-title">{{ title }}</h2>
          <div v-if="$slots['title-right']" class="modal-title-right">
            <slot name="title-right" />
          </div>
          <button class="btn-icon" aria-label="Close modal" @click="emit('close')">
            <svg fill="none" stroke="currentColor" viewBox="0 0 24 24">
              <path
                stroke-linecap="round"
                stroke-linejoin="round"
                stroke-width="2"
                d="M6 18L18 6M6 6l12 12"
              />
            </svg>
          </button>
        </div>
        <div class="modal-body">
          <slot />
        </div>
      </div>
    </template>
  </Dialog>
</template>

<script setup lang="ts">
import { nextTick, ref, useId } from "vue";
import Dialog from "primevue/dialog";

defineProps<{
  isOpen: boolean;
  title: string;
  className?: string;
}>();
const emit = defineEmits<{ (e: "close"): void }>();

const panelRef = ref<HTMLDivElement | null>(null);

// Give the dialog an accessible name. Without an explicit aria-labelledby,
// PrimeVue defaults it to a generated "<id>_header" element that the #container
// slot replaces and never renders, leaving the dialog unnamed. Point it at the
// real .modal-title instead.
const titleId = useId();

// PrimeVue drives visibility internally; relay every request to close (Escape,
// dismissable mask click) up to the parent, which owns isOpen.
function onVisibleChange(visible: boolean) {
  if (!visible) emit("close");
}

// The #container slot replaces PrimeVue's default header/content, so its
// built-in focus() has nothing to target. Move focus into the panel ourselves
// so the FocusTrap directive engages and screen readers announce the dialog.
function onShow() {
  nextTick(() => panelRef.value?.focus());
}
</script>
