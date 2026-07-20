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
    :options="visibleModels"
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
    filter
    :filter-fields="['display_name', 'id', 'source']"
    filter-placeholder="Search models"
    empty-filter-message="No models found"
    reset-filter-on-hide
    auto-filter-focus
    @update:model-value="handleSelect"
    @filter="onFilter"
    @hide="filterValue = ''"
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
      <button
        v-if="tier2Models.length > 0 && !filterValue"
        class="model-picker-manage model-picker-more"
        type="button"
        @click="toggleMore"
      >
        <svg
          :class="`model-picker-more-icon ${showMore ? 'expanded' : ''}`"
          width="14"
          height="14"
          viewBox="0 0 24 24"
          fill="none"
          stroke="currentColor"
          stroke-width="2"
        >
          <path d="M6 9l6 6 6-6" />
        </svg>
        <span>{{ showMore ? "Fewer models" : `More models (${tier2Models.length})` }}</span>
      </button>
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

// Tier 2 models are overshadowed by a better available sibling; they're kept
// behind a "more models" toggle so the common case stays uncluttered. Absent
// tier is treated as tier 1 (backward compatible with older servers).
const isTier2 = (m: Model) => m.tier === 2;
const tier1Models = computed(() => props.models.filter((m) => !isTier2(m)));
const tier2Models = computed(() => props.models.filter(isTier2));

const showMore = ref(false);
const filterValue = ref("");

function onFilter(event: { value: string }) {
  filterValue.value = event.value ?? "";
}

// When collapsed, only show tier-1 models — plus the currently selected model
// if it happens to be a tier-2 one, so the selection always renders. While a
// filter is active, search across all models (including tier-2) so hidden
// models remain findable without expanding "More models".
const visibleModels = computed(() => {
  if (showMore.value || filterValue.value) return props.models;
  const base = tier1Models.value;
  const selected = props.models.find((m) => m.id === props.selectedModel);
  if (selected && isTier2(selected) && !base.includes(selected)) {
    return [...base, selected];
  }
  return base;
});

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

function toggleMore() {
  showMore.value = !showMore.value;
}

function handleManageModels() {
  selectRef.value?.hide();
  emit("manageModels");
}

function handleRefreshModels() {
  emit("refreshModels");
}
</script>
