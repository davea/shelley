package server

import "shelley.exe.dev/featureflags"

// FlagToolPills toggles the iOS-style pill rendering of tool bursts in the
// web conversation UI. When false (the default), each tool call renders as
// a full-width CoalescedToolCall card as before. When true, consecutive
// non-auto-expand tool calls collapse into a wrapped row of compact pills;
// tapping a pill opens the full card in a modal.
//
// Auto-expand tools (patch, screenshot, read_image, output_iframe) are
// unaffected — they continue to render inline regardless of this flag.
var FlagToolPills = featureflags.Register(featureflags.Flag{
	Name:        "tool-pills",
	Description: "Render bursts of tool calls as compact pills (iOS-style). Click a pill to open the full tool card in a modal.",
	Default:     false,
})

// FlagTokenCostGraph adds a stacked area graph to the context usage popup:
// the x axis is LLM calls (messages with usage data), the y axis is
// cumulative tokens of each type (input, cache write, cache read, output)
// weighted by models.dev pricing for the model that served each call.
var FlagTokenCostGraph = featureflags.Register(featureflags.Flag{
	Name:        "token-cost-graph",
	Description: "Show a stacked cumulative token-cost graph (models.dev pricing) in the context usage popup.",
	Default:     false,
})
