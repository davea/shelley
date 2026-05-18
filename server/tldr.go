package server

import (
	"context"
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"shelley.exe.dev/db"
	"shelley.exe.dev/db/generated"
	"shelley.exe.dev/llm"
)

// TL;DRs are short (1-2 sentence) summaries we attach to long end-of-turn
// agent messages so the user can scan them quickly. They live in the
// message's user_data under the "tldr" key.

const tldrUserDataKey = "tldr"

const tldrPrompt = `Write a 1-2 sentence TL;DR of the following assistant message.

Requirements:
- 1 or 2 sentences, no more.
- Succinct, visually scannable, sounds natural when read aloud.
- Capture the most important outcome or upshot.
- No preamble, no "TL;DR:" prefix, no quotes, no markdown.
- Plain prose. No bullet points. No emoji.

Message:
---
%s
---

Reply with only the TL;DR, nothing else.`

// tldrMinChars is the message length above which we attach a TL;DR.
const tldrMinChars = 240

// needsTLDR returns true when a message is long enough to deserve a TL;DR.
func needsTLDR(text string) bool {
	return len(strings.TrimSpace(text)) > tldrMinChars
}

// generateTLDR calls a slug-tagged LLM to produce a TL;DR of text.
// Reuses the slug-generation model selection because we want something
// fast and cheap; the actual prompt is different.
func (s *Server) generateTLDR(ctx context.Context, text string) (string, error) {
	svc, modelID, err := s.pickTLDRService()
	if err != nil {
		return "", err
	}
	ctx, cancel := context.WithTimeout(ctx, 30*time.Second)
	defer cancel()

	resp, err := svc.Do(ctx, &llm.Request{
		Messages: []llm.Message{{
			Role: llm.MessageRoleUser,
			Content: []llm.Content{
				{Type: llm.ContentTypeText, Text: fmt.Sprintf(tldrPrompt, text)},
			},
		}},
	})
	if err != nil {
		return "", fmt.Errorf("tldr LLM call failed (model=%s): %w", modelID, err)
	}
	if len(resp.Content) == 0 {
		return "", fmt.Errorf("empty tldr response from model %s", modelID)
	}
	out := strings.TrimSpace(resp.Content[0].Text)
	if out == "" {
		return "", fmt.Errorf("empty tldr text from model %s", modelID)
	}
	return out, nil
}

// pickTLDRService picks a model service for TL;DR generation. Prefers
// models tagged "slug" then "slug-backup"; falls back to "predictable"
// in test setups.
func (s *Server) pickTLDRService() (llm.Service, string, error) {
	for _, tag := range []string{"slug", "slug-backup"} {
		for _, id := range s.llmManager.GetAvailableModels() {
			info := s.llmManager.GetModelInfo(id)
			if info == nil || !tldrHasTag(info.Tags, tag) {
				continue
			}
			svc, err := s.llmManager.GetService(id)
			if err == nil {
				return svc, id, nil
			}
		}
	}
	// Last resort: predictable, useful in tests / predictable-only mode.
	if svc, err := s.llmManager.GetService("predictable"); err == nil {
		return svc, "predictable", nil
	}
	return nil, "", fmt.Errorf("no suitable model available for tldr generation")
}

func tldrHasTag(tags, tag string) bool {
	for _, t := range strings.Split(tags, ",") {
		if strings.TrimSpace(t) == tag {
			return true
		}
	}
	return false
}

// pickTLDRTarget walks agent messages since the last user message
// (newest first, as returned by ListAgentMessagesSinceLastUser) and
// returns the newest one with non-empty text content plus that text.
// Returns (nil, "") if none qualifies.
func pickTLDRTarget(messages []generated.Message) (*generated.Message, string) {
	for i := range messages {
		m := messages[i]
		if m.Type != string(db.MessageTypeAgent) || m.LlmData == nil {
			continue
		}
		var llmMsg llm.Message
		if err := json.Unmarshal([]byte(*m.LlmData), &llmMsg); err != nil {
			continue
		}
		if text := lastTextContent(llmMsg); text != "" {
			return &m, text
		}
	}
	return nil, ""
}

// attachTLDR stores tldr in the message's user_data (merging with any
// existing JSON object) and broadcasts the update.
func (s *Server) attachTLDR(ctx context.Context, conversationID, messageID, tldr string) error {
	msg, err := s.db.GetMessageByID(ctx, messageID)
	if err != nil {
		return fmt.Errorf("get message: %w", err)
	}
	data := map[string]any{}
	if msg.UserData != nil && *msg.UserData != "" {
		// Best-effort: if existing user_data isn't an object, overwrite.
		if err := json.Unmarshal([]byte(*msg.UserData), &data); err != nil {
			data = map[string]any{}
		}
	}
	data[tldrUserDataKey] = tldr
	encoded, err := json.Marshal(data)
	if err != nil {
		return fmt.Errorf("marshal user_data: %w", err)
	}
	str := string(encoded)
	if err := s.db.UpdateMessageUserData(ctx, messageID, &str); err != nil {
		return fmt.Errorf("update user_data: %w", err)
	}
	updated, err := s.db.GetMessageByID(ctx, messageID)
	if err != nil {
		return fmt.Errorf("refetch message: %w", err)
	}
	s.broadcastMessageUpdate(ctx, conversationID, updated)
	return nil
}

// maybeGenerateEndOfTurnTLDR fires after a top-level (non-subagent) turn
// ends. It finds the final user-visible agent text, decides whether a
// TL;DR is warranted, generates one, and persists+broadcasts it.
// Errors are logged and swallowed: a missing TL;DR is never fatal.
func (s *Server) maybeGenerateEndOfTurnTLDR(ctx context.Context, conversationID string) {
	msgs, err := s.db.ListAgentMessagesSinceLastUser(ctx, conversationID)
	if err != nil {
		s.logger.Warn("tldr: failed to list agent messages", "conversationID", conversationID, "error", err)
		return
	}
	target, text := pickTLDRTarget(msgs)
	if target == nil {
		return
	}
	if !needsTLDR(text) {
		return
	}
	// Skip if this message already has a tldr.
	if target.UserData != nil && *target.UserData != "" {
		var existing map[string]any
		if err := json.Unmarshal([]byte(*target.UserData), &existing); err == nil {
			if v, ok := existing[tldrUserDataKey].(string); ok && v != "" {
				return
			}
		}
	}
	tldr, err := s.generateTLDR(ctx, text)
	if err != nil {
		s.logger.Warn("tldr: generation failed", "conversationID", conversationID, "error", err)
		return
	}
	if err := s.attachTLDR(ctx, conversationID, target.MessageID, tldr); err != nil {
		s.logger.Warn("tldr: attach failed", "conversationID", conversationID, "messageID", target.MessageID, "error", err)
		return
	}
	s.logger.Info("tldr: attached", "conversationID", conversationID, "messageID", target.MessageID)
}
