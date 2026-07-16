// Unit tests for coalesceMessages (ChatInterface message/tool splitting).
// Run with: tsx src/vue/components/coalesce.test.ts
import { coalesceMessages } from "./coalesce";
import type { Message, LLMContent, LLMMessage } from "../../types";

let passed = 0;
let failed = 0;
const failures: string[] = [];
function check(name: string, cond: boolean, detail?: unknown) {
  if (cond) {
    passed++;
  } else {
    failed++;
    failures.push(`✗ ${name}${detail !== undefined ? `\n   ${JSON.stringify(detail)}` : ""}`);
  }
}

let seq = 0;
function agentMessage(content: LLMContent[]): Message {
  seq++;
  const llm: LLMMessage = { Role: 1, Content: content };
  return {
    message_id: `m${seq}`,
    conversation_id: "c1",
    sequence_id: seq,
    type: "agent",
    generation: 1,
    llm_data: JSON.stringify(llm),
  } as unknown as Message;
}
function text(t: string): LLMContent {
  return { ID: "", Type: 2, Text: t } as LLMContent;
}
function thinking(t: string): LLMContent {
  return { ID: "", Type: 3, Text: t } as LLMContent;
}
function toolUse(id: string): LLMContent {
  return { ID: id, Type: 5, ToolName: "bash", ToolInput: { command: "true" } } as LLMContent;
}

// --- Text + tool use: one message item and one tool item ---
{
  const items = coalesceMessages([agentMessage([text("hello"), toolUse("t1")])]);
  check(
    "text+tool -> message and tool items",
    items.length === 2 && items[0].type === "message" && items[1].type === "tool",
    items,
  );
}

// --- Thinking-only turn with a tool call still renders the thinking ---
{
  const items = coalesceMessages([agentMessage([thinking("chain of thought"), toolUse("t2")])]);
  check(
    "thinking+tool -> message and tool items",
    items.length === 2 && items[0].type === "message" && items[1].type === "tool",
    items,
  );
}

// --- Thinking-only turn (no text, no tools) renders the thinking ---
{
  const items = coalesceMessages([agentMessage([thinking("just pondering")])]);
  check("thinking only -> message item", items.length === 1 && items[0].type === "message", items);
}

// --- Empty thinking (e.g. signature-only block) is not renderable ---
{
  const items = coalesceMessages([agentMessage([thinking(""), toolUse("t3")])]);
  check(
    "empty thinking+tool -> tool item only",
    items.length === 1 && items[0].type === "tool",
    items,
  );
}

// --- Tool-only turn produces no message item ---
{
  const items = coalesceMessages([agentMessage([toolUse("t4")])]);
  check("tool only -> tool item only", items.length === 1 && items[0].type === "tool", items);
}

console.log(`\ncoalesceMessages Tests: ${passed} passed, ${failed} failed\n`);
if (failures.length > 0) {
  for (const f of failures) console.log(f);
  process.exit(1);
}
console.log("All tests passed!");
process.exit(0);
