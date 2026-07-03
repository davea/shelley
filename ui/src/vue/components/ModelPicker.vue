<!-- Model dropdown, migrated from a hand-rolled dropdown to PrimeVue <Select>.
     PrimeVue owns open/close, outside-click, Escape and viewport-aware flip
     placement (which this component previously reimplemented by hand). The
     option template keeps the source sub-label + "not ready" badge; a #footer
     slot carries the "Add / Remove Models..." action. Styling uses PrimeVue's
     own token system (size="small" + the shared statusPickerDt map), matching
     the sibling ThinkingLevelPicker. Emits the same selectModel / manageModels
     events. -->
<template>
  <Select
    ref="selectRef"
    :model-value="selectedModel"
    :options="models"
    :option-label="(m: Model) => m.display_name || m.id"
    option-value="id"
    :option-disabled="(m: Model) => !m.ready"
    :disabled="disabled"
    fluid
    size="small"
    :dt="statusPickerDt"
    scroll-height="22rem"
    class="model-picker"
    :aria-label="`Model: ${displayWithSource}`"
    append-to="self"
    :pt="{ overlay: { class: 'model-picker-panel' } }"
    @update:model-value="handleSelect"
  >
    <template #value>{{ displayName }}</template>
    <template #option="{ option }">
      <div class="model-picker-option-content">
        <span class="model-picker-option-name">{{ option.display_name || option.id }}</span>
        <span v-if="option.source" class="model-picker-option-source">{{ option.source }}</span>
      </div>
      <span v-if="!option.ready" class="model-picker-option-badge">not ready</span>
    </template>
    <template #footer>
      <div class="model-picker-divider" />
      <button class="model-picker-manage" type="button" @click="handleManageModels">
        <svg
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M12 4v16m-8-8h16" />
        </svg>
        <span>Add / Remove Models...</span>
      </button>
      <button
        class="model-picker-manage"
        type="button"
        :disabled="refreshing"
        @click="handleRefreshModels"
      >
        <svg
          :class="`model-picker-refresh-icon ${refreshing ? 'spinning' : ''}`"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M21 12a9 9 0 1 1-2.64-6.36M21 3v6h-6" />
        </svg>
        <span>{{ refreshing ? "Refreshing Models..." : "Refresh Models" }}</span>
      </button>
    </template>
  </Select>
</template>

<script setup lang="ts">
import { computed, ref } from "vue";
import Select from "primevue/select";
import { statusPickerDt } from "./statusPickerDt";
import type { Model } from "../../types";

const props = withDefaults(
  defineProps<{
    models: Model[];
    selectedModel: string;
    disabled?: boolean;
    refreshing?: boolean;
  }>(),
  { disabled: false, refreshing: false },
);
const emit = defineEmits<{
  (e: "selectModel", modelId: string): void;
  (e: "manageModels"): void;
  (e: "refreshModels"): void;
}>();

const selectRef = ref<InstanceType<typeof Select> | null>(null);

const selectedModelObj = computed(() => props.models.find((m) => m.id === props.selectedModel));
const displayName = computed(() => selectedModelObj.value?.display_name || props.selectedModel);
const displayWithSource = computed(() =>
  selectedModelObj.value?.source && selectedModelObj.value.source !== "custom"
    ? `${displayName.value} (${selectedModelObj.value.source})`
    : displayName.value,
);

function handleSelect(modelId: string) {
  emit("selectModel", modelId);
}

function handleManageModels() {
  selectRef.value?.hide();
  emit("manageModels");
}

function handleRefreshModels() {
  emit("refreshModels");
}
</script>
