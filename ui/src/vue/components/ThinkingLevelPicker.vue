<!-- Reasoning-effort picker, migrated from a hand-rolled dropdown to PrimeVue
     <Select>. PrimeVue owns the open/close, outside-click, Escape and viewport
     placement (flip) that the old component reimplemented by hand. Styling is
     entirely PrimeVue's own token system: size="small" for the compact look
     plus the shared statusPickerDt token map (our theme vars). Emits "change"
     with the new level (unchanged prop/event contract with ChatStatusContent). -->
<template>
  <Select
    :model-value="value"
    :options="THINKING_LEVELS"
    option-label="label"
    option-value="value"
    :disabled="disabled"
    fluid
    size="small"
    :dt="statusPickerDt"
    scroll-height="22rem"
    class="thinking-level-picker"
    :aria-label="`Reasoning effort: ${current.label}`"
    append-to="self"
    :pt="{ overlay: { class: 'thinking-level-picker-panel' } }"
    @update:model-value="select"
  />
</template>

<script setup lang="ts">
import { computed } from "vue";
import Select from "primevue/select";
import { statusPickerDt } from "./statusPickerDt";
import { THINKING_LEVELS, DEFAULT_THINKING_LEVEL, type ThinkingLevel } from "./thinkingLevel";

const props = withDefaults(
  defineProps<{
    value: ThinkingLevel;
    disabled?: boolean;
  }>(),
  { disabled: false },
);
const emit = defineEmits<{ (e: "change", level: ThinkingLevel): void }>();

const current = computed(
  () =>
    THINKING_LEVELS.find((l) => l.value === props.value) ||
    THINKING_LEVELS.find((l) => l.value === DEFAULT_THINKING_LEVEL)!,
);

function select(level: ThinkingLevel) {
  emit("change", level);
}
</script>
