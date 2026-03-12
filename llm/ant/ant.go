package ant

import (
	"bufio"
	"bytes"
	"cmp"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"math/rand/v2"
	"net/http"
	"os"
	"strconv"
	"strings"
	"sync"
	"time"

	"shelley.exe.dev/llm"
)

const (
	DefaultModel = Claude45Sonnet
	APIKeyEnv    = "ANTHROPIC_API_KEY"
	DefaultURL   = "https://api.anthropic.com/v1/messages"

	// OAuth token constants
	oauthTokenPrefix  = "sk-ant-oat"
	oauthSystemPrefix = "You are Claude Code, Anthropic's official CLI for Claude."
	oauthBetaFeatures = "claude-code-20250219,oauth-2025-04-20,fine-grained-tool-streaming-2025-05-14,interleaved-thinking-2025-05-14"
	oauthUserAgent    = "claude-cli/2.1.2 (external, cli)"

	// OAuth refresh constants
	oauthRefreshURL   = "https://platform.claude.com/v1/oauth/token"
	oauthClientID     = "9d1c250a-e61b-44d9-88ed-5944d1962f5e"
	oauthScope        = "user:inference user:mcp_servers user:profile user:sessions:claude_code"
	oauthRefreshAgent = "axios/1.8.4"
)

const (
	Claude45Haiku  = "claude-haiku-4-5-20251001"
	Claude4Sonnet  = "claude-sonnet-4-20250514"
	Claude45Sonnet = "claude-sonnet-4-5-20250929"
	Claude45Opus   = "claude-opus-4-5-20251101"
	Claude46Opus   = "claude-opus-4-6"
	Claude46Sonnet = "claude-sonnet-4-6"
)

// modelMaxOutputTokens maps model names to their maximum output token limits.
// See https://docs.anthropic.com/en/docs/about-claude/models/all-models
var modelMaxOutputTokens = map[string]int{
	Claude46Opus:   128000,
	Claude45Opus:   128000,
	Claude46Sonnet: 64000,
	Claude45Sonnet: 64000,
	Claude4Sonnet:  64000,
	Claude45Haiku:  64000,
}

// defaultMaxOutputTokens is used for unrecognized models.
const defaultMaxOutputTokens = 64000

// maxOutputTokens returns the max output token limit for a model.
func maxOutputTokens(model string) int {
	if n, ok := modelMaxOutputTokens[model]; ok {
		return n
	}
	return defaultMaxOutputTokens
}

// oauthToken represents a parsed OAuth token with refresh capability.
type oauthToken struct {
	AccessToken  string
	ExpiryTime   time.Time
	RefreshToken string
}

// parseOAuthToken parses a composite OAuth token string.
// Format: "<access_token>;<expiry_unix_timestamp>;<refresh_token>"
// Returns nil if the token is not in OAuth format.
func parseOAuthToken(token string) *oauthToken {
	parts := strings.Split(token, ";")
	if len(parts) != 3 {
		return nil
	}
	if !strings.HasPrefix(parts[0], oauthTokenPrefix) {
		return nil
	}
	expiry, err := strconv.ParseInt(parts[1], 10, 64)
	if err != nil {
		return nil
	}
	return &oauthToken{
		AccessToken:  parts[0],
		ExpiryTime:   time.Unix(expiry, 0),
		RefreshToken: parts[2],
	}
}

// String returns the composite token string.
func (t *oauthToken) String() string {
	return fmt.Sprintf("%s;%d;%s", t.AccessToken, t.ExpiryTime.Unix(), t.RefreshToken)
}

// IsExpired reports whether the token has expired.
func (t *oauthToken) IsExpired() bool {
	return time.Now().After(t.ExpiryTime)
}

// isOAuthFilePath reports whether the API key refers to a JSON file on disk.
func isOAuthFilePath(apiKey string) bool {
	return strings.HasSuffix(apiKey, ".json")
}

// readOAuthFromFile reads OAuth token fields from a JSON file.
// The file must contain claudeAiOauth.{accessToken, expiresAt, refreshToken}.
// expiresAt is a unix timestamp in milliseconds.
func readOAuthFromFile(path string) (*oauthToken, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read oauth file %s: %w", path, err)
	}
	var file struct {
		ClaudeAiOauth struct {
			AccessToken  string `json:"accessToken"`
			ExpiresAt    int64  `json:"expiresAt"`
			RefreshToken string `json:"refreshToken"`
		} `json:"claudeAiOauth"`
	}
	if err := json.Unmarshal(data, &file); err != nil {
		return nil, fmt.Errorf("parse oauth file %s: %w", path, err)
	}
	oauth := file.ClaudeAiOauth
	if oauth.AccessToken == "" || oauth.RefreshToken == "" {
		return nil, fmt.Errorf("oauth file %s missing accessToken or refreshToken", path)
	}
	// expiresAt is in milliseconds
	return &oauthToken{
		AccessToken:  oauth.AccessToken,
		ExpiryTime:   time.Unix(oauth.ExpiresAt/1000, (oauth.ExpiresAt%1000)*int64(time.Millisecond)),
		RefreshToken: oauth.RefreshToken,
	}, nil
}

// writeOAuthToFile updates only the OAuth token fields in a JSON file,
// preserving all other contents.
func writeOAuthToFile(path string, tok *oauthToken) error {
	data, err := os.ReadFile(path)
	if err != nil {
		return fmt.Errorf("read oauth file for update %s: %w", path, err)
	}
	var raw map[string]json.RawMessage
	if err := json.Unmarshal(data, &raw); err != nil {
		return fmt.Errorf("parse oauth file for update %s: %w", path, err)
	}
	// Parse the nested claudeAiOauth object, preserving extra fields
	var oauthRaw map[string]json.RawMessage
	if existing, ok := raw["claudeAiOauth"]; ok {
		if err := json.Unmarshal(existing, &oauthRaw); err != nil {
			return fmt.Errorf("parse claudeAiOauth in %s: %w", path, err)
		}
	} else {
		oauthRaw = make(map[string]json.RawMessage)
	}
	// Update only the three token fields
	oauthRaw["accessToken"], _ = json.Marshal(tok.AccessToken)
	oauthRaw["expiresAt"], _ = json.Marshal(tok.ExpiryTime.UnixMilli())
	oauthRaw["refreshToken"], _ = json.Marshal(tok.RefreshToken)

	updatedOAuth, err := json.Marshal(oauthRaw)
	if err != nil {
		return fmt.Errorf("marshal claudeAiOauth: %w", err)
	}
	raw["claudeAiOauth"] = updatedOAuth

	updatedData, err := json.MarshalIndent(raw, "", "    ")
	if err != nil {
		return fmt.Errorf("marshal oauth file: %w", err)
	}
	updatedData = append(updatedData, '\n')
	if err := os.WriteFile(path, updatedData, 0600); err != nil {
		return fmt.Errorf("write oauth file %s: %w", path, err)
	}
	return nil
}

// oauthRefreshRequest is the request body for token refresh.
type oauthRefreshRequest struct {
	ClientID     string `json:"client_id"`
	GrantType    string `json:"grant_type"`
	RefreshToken string `json:"refresh_token"`
	Scope        string `json:"scope"`
}

// oauthRefreshResponse is the response from token refresh.
type oauthRefreshResponse struct {
	AccessToken  string `json:"access_token"`
	ExpiresIn    int64  `json:"expires_in"`
	RefreshToken string `json:"refresh_token"`
}

// refreshOAuthToken refreshes an expired OAuth token.
func (s *Service) refreshOAuthToken(ctx context.Context, tok *oauthToken) (*oauthToken, error) {
	reqBody := oauthRefreshRequest{
		ClientID:     oauthClientID,
		GrantType:    "refresh_token",
		RefreshToken: tok.RefreshToken,
		Scope:        oauthScope,
	}
	payload, err := json.Marshal(reqBody)
	if err != nil {
		return nil, fmt.Errorf("marshal refresh request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, "POST", oauthRefreshURL, bytes.NewReader(payload))
	if err != nil {
		return nil, fmt.Errorf("create refresh request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("User-Agent", oauthRefreshAgent)

	httpc := cmp.Or(s.HTTPC, http.DefaultClient)
	resp, err := httpc.Do(req)
	if err != nil {
		return nil, fmt.Errorf("refresh request failed: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("refresh failed with status %d: %s", resp.StatusCode, body)
	}

	var refreshResp oauthRefreshResponse
	if err := json.NewDecoder(resp.Body).Decode(&refreshResp); err != nil {
		return nil, fmt.Errorf("decode refresh response: %w", err)
	}

	return &oauthToken{
		AccessToken:  refreshResp.AccessToken,
		ExpiryTime:   time.Now().Add(time.Duration(refreshResp.ExpiresIn) * time.Second),
		RefreshToken: refreshResp.RefreshToken,
	}, nil
}

// IsClaudeModel reports whether userName is a user-friendly Claude model.
// It uses ClaudeModelName under the hood.
func IsClaudeModel(userName string) bool {
	return ClaudeModelName(userName) != ""
}

// ClaudeModelName returns the Anthropic Claude model name for userName.
// It returns an empty string if userName is not a recognized Claude model.
func ClaudeModelName(userName string) string {
	switch userName {
	case "claude", "sonnet":
		return Claude45Sonnet
	case "opus":
		return Claude45Opus
	default:
		return ""
	}
}

// TokenContextWindow returns the maximum token context window size for this service
func (s *Service) TokenContextWindow() int {
	return 200000
}

// maxOutputTokens returns the maximum allowed output tokens for the configured model.
// Source: https://models.dev/api.json (Anthropic provider, limit.output)
func (s *Service) maxOutputTokens() int {
	model := s.Model
	if model == "" {
		model = DefaultModel
	}
	switch model {
	case Claude46Opus:
		return 128000
	case Claude4Sonnet, Claude45Sonnet, Claude46Sonnet,
		Claude45Haiku, Claude45Opus:
		return 64000
	default:
		return 64000
	}
}

// MaxImageDimension returns the maximum allowed image dimension for multi-image requests.
// Anthropic enforces a 2000 pixel limit when multiple images are in a conversation.
func (s *Service) MaxImageDimension() int {
	return 2000
}

// Service provides Claude completions.
// Fields should not be altered concurrently with calling any method on Service.
type Service struct {
	HTTPC          *http.Client      // defaults to http.DefaultClient if nil
	URL            string            // defaults to DefaultURL if empty
	APIKey         string            // must be non-empty; for OAuth: "access;expiry;refresh" or path ending in .json
	Model          string            // defaults to DefaultModel if empty
	MaxTokens      int               // 0 means use model-specific limit from modelMaxOutputTokens
	ThinkingLevel  llm.ThinkingLevel // thinking level (ThinkingLevelOff disables, default is ThinkingLevelMedium)
	Backoff        []time.Duration   // retry backoff durations; defaults to {15s, 30s, 60s} if nil
	OnTokenRefresh func(newToken string) // called after successful OAuth token refresh (composite token string)
	OnTokenReload  func() string         // called to re-read the latest API key from source of truth

	oauthMu sync.Mutex // serializes OAuth token operations
}

var _ llm.Service = (*Service)(nil)

type content struct {
	// https://docs.anthropic.com/en/api/messages
	ID   string `json:"id,omitempty"`
	Type string `json:"type,omitempty"`

	// Subtly, an empty string appears in tool results often, so we have
	// to distinguish between empty string and no string.
	// Underlying error looks like one of:
	//   "messages.46.content.0.tool_result.content.0.text.text: Field required""
	//   "messages.1.content.1.tool_use.text: Extra inputs are not permitted"
	//
	// I haven't found a super great source for the API, but
	// https://github.com/anthropics/anthropic-sdk-typescript/blob/main/src/resources/messages/messages.ts
	// is somewhat acceptable but hard to read.
	Text      *string         `json:"text,omitempty"`
	MediaType string          `json:"media_type,omitempty"` // for image
	Source    json.RawMessage `json:"source,omitempty"`     // for image

	// for thinking
	Thinking  *string `json:"thinking,omitempty"`
	Data      string  `json:"data,omitempty"`      // for redacted_thinking or image
	Signature string  `json:"signature,omitempty"` // for thinking

	// for tool_use
	ToolName  string          `json:"name,omitempty"`
	ToolInput json.RawMessage `json:"input,omitempty"`

	// for tool_result
	ToolUseID string `json:"tool_use_id,omitempty"`
	ToolError bool   `json:"is_error,omitempty"`
	// note the recursive nature here; message looks like:
	// {
	//  "role": "user",
	//  "content": [
	//    {
	//      "type": "tool_result",
	//      "tool_use_id": "toolu_01A09q90qw90lq917835lq9",
	//      "content": [
	//        {"type": "text", "text": "15 degrees"},
	//        {
	//          "type": "image",
	//          "source": {
	//            "type": "base64",
	//            "media_type": "image/jpeg",
	//            "data": "/9j/4AAQSkZJRg...",
	//          }
	//        }
	//      ]
	//    }
	//  ]
	//}
	ToolResult []content `json:"content,omitempty"`

	// timing information for tool_result; not sent to Claude
	StartTime *time.Time `json:"-"`
	EndTime   *time.Time `json:"-"`

	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// message represents a message in the conversation.
type message struct {
	Role    string    `json:"role"`
	Content []content `json:"content"`
	ToolUse *toolUse  `json:"tool_use,omitempty"` // use to control whether/which tool to use
}

// toolUse represents a tool use in the message content.
type toolUse struct {
	ID   string `json:"id"`
	Name string `json:"name"`
}

// tool represents a tool available to Claude.
type tool struct {
	Name string `json:"name"`
	// Type is used by the text editor tool; see
	// https://docs.anthropic.com/en/docs/build-with-claude/tool-use/text-editor-tool
	Type         string          `json:"type,omitempty"`
	Description  string          `json:"description,omitempty"`
	InputSchema  json.RawMessage `json:"input_schema,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// usage represents the billing and rate-limit usage.
type usage struct {
	InputTokens              uint64  `json:"input_tokens"`
	CacheCreationInputTokens uint64  `json:"cache_creation_input_tokens"`
	CacheReadInputTokens     uint64  `json:"cache_read_input_tokens"`
	OutputTokens             uint64  `json:"output_tokens"`
	CostUSD                  float64 `json:"cost_usd"`
}

func (u *usage) Add(other usage) {
	u.InputTokens += other.InputTokens
	u.CacheCreationInputTokens += other.CacheCreationInputTokens
	u.CacheReadInputTokens += other.CacheReadInputTokens
	u.OutputTokens += other.OutputTokens
	u.CostUSD += other.CostUSD
}

// response represents the response from the message API.
type response struct {
	ID           string    `json:"id"`
	Type         string    `json:"type"`
	Role         string    `json:"role"`
	Model        string    `json:"model"`
	Content      []content `json:"content"`
	StopReason   string    `json:"stop_reason"`
	StopSequence *string   `json:"stop_sequence,omitempty"`
	Usage        usage     `json:"usage"`
}

type toolChoice struct {
	Type string `json:"type"`
	Name string `json:"name,omitempty"`
}

// https://docs.anthropic.com/en/api/messages#body-system
type systemContent struct {
	Text         string          `json:"text,omitempty"`
	Type         string          `json:"type,omitempty"`
	CacheControl json.RawMessage `json:"cache_control,omitempty"`
}

// request represents the request payload for creating a message.
// thinking configures extended thinking for Claude models.
type thinking struct {
	Type         string `json:"type"`                    // "enabled"
	BudgetTokens int    `json:"budget_tokens,omitempty"` // Max tokens for thinking
}

type request struct {
	// Field order matters for JSON serialization - stable fields should come first
	// to maximize prefix deduplication when storing LLM requests.
	Model         string          `json:"model"`
	MaxTokens     int             `json:"max_tokens"`
	Stream        bool            `json:"stream,omitempty"`
	System        []systemContent `json:"system,omitempty"`
	Tools         []*tool         `json:"tools,omitempty"`
	ToolChoice    *toolChoice     `json:"tool_choice,omitempty"`
	Thinking      *thinking       `json:"thinking,omitempty"`
	Temperature   float64         `json:"temperature,omitempty"`
	TopK          int             `json:"top_k,omitempty"`
	TopP          float64         `json:"top_p,omitempty"`
	StopSequences []string        `json:"stop_sequences,omitempty"`
	// Messages comes last since it grows with each request in a conversation
	Messages []message `json:"messages"`
}

func mapped[Slice ~[]E, E, T any](s Slice, f func(E) T) []T {
	out := make([]T, len(s))
	for i, v := range s {
		out[i] = f(v)
	}
	return out
}

func inverted[K, V cmp.Ordered](m map[K]V) map[V]K {
	inv := make(map[V]K)
	for k, v := range m {
		if _, ok := inv[v]; ok {
			panic(fmt.Errorf("inverted map has multiple keys for value %v", v))
		}
		inv[v] = k
	}
	return inv
}

var (
	fromLLMRole = map[llm.MessageRole]string{
		llm.MessageRoleAssistant: "assistant",
		llm.MessageRoleUser:      "user",
	}
	toLLMRole = inverted(fromLLMRole)

	fromLLMContentType = map[llm.ContentType]string{
		llm.ContentTypeText:             "text",
		llm.ContentTypeThinking:         "thinking",
		llm.ContentTypeRedactedThinking: "redacted_thinking",
		llm.ContentTypeToolUse:          "tool_use",
		llm.ContentTypeToolResult:       "tool_result",
	}
	toLLMContentType = inverted(fromLLMContentType)

	fromLLMToolChoiceType = map[llm.ToolChoiceType]string{
		llm.ToolChoiceTypeAuto: "auto",
		llm.ToolChoiceTypeAny:  "any",
		llm.ToolChoiceTypeNone: "none",
		llm.ToolChoiceTypeTool: "tool",
	}

	toLLMStopReason = map[string]llm.StopReason{
		"stop_sequence": llm.StopReasonStopSequence,
		"max_tokens":    llm.StopReasonMaxTokens,
		"end_turn":      llm.StopReasonEndTurn,
		"tool_use":      llm.StopReasonToolUse,
		"refusal":       llm.StopReasonRefusal,
	}
)

func fromLLMCache(c bool) json.RawMessage {
	if !c {
		return nil
	}
	return json.RawMessage(`{"type":"ephemeral"}`)
}

func fromLLMContent(c llm.Content) content {
	var toolResult []content
	if len(c.ToolResult) > 0 {
		toolResult = make([]content, len(c.ToolResult))
		for i, tr := range c.ToolResult {
			// For image content inside a tool_result, we need to map it to "image" type
			if tr.MediaType != "" && tr.MediaType == "image/jpeg" || tr.MediaType == "image/png" {
				// Format as an image for Claude
				toolResult[i] = content{
					Type: "image",
					Source: json.RawMessage(fmt.Sprintf(`{"type":"base64","media_type":"%s","data":"%s"}`,
						tr.MediaType, tr.Data)),
				}
			} else {
				toolResult[i] = fromLLMContent(tr)
			}
		}
	}

	d := content{
		Type:         fromLLMContentType[c.Type],
		CacheControl: fromLLMCache(c.Cache),
	}

	// Set fields based on content type to avoid sending invalid fields
	switch c.Type {
	case llm.ContentTypeText:
		// Images are represented as text with MediaType and Data
		if c.MediaType != "" {
			d.Type = "image"
			d.Source = json.RawMessage(fmt.Sprintf(`{"type":"base64","media_type":"%s","data":"%s"}`,
				c.MediaType, c.Data))
		} else {
			d.Text = &c.Text
		}
	case llm.ContentTypeThinking:
		d.Thinking = &c.Thinking
		d.Signature = c.Signature
	case llm.ContentTypeRedactedThinking:
		d.Data = c.Data
		d.Signature = c.Signature
	case llm.ContentTypeToolUse:
		d.ID = c.ID
		d.ToolName = c.ToolName
		d.ToolInput = c.ToolInput
		// Handle both nil and JSON "null" (which unmarshals as []byte("null"))
		if d.ToolInput == nil || string(d.ToolInput) == "null" {
			d.ToolInput = json.RawMessage("{}")
		}
	case llm.ContentTypeToolResult:
		d.ToolUseID = c.ToolUseID
		d.ToolError = c.ToolError
		d.ToolResult = toolResult
	}

	return d
}

func fromLLMToolUse(tu *llm.ToolUse) *toolUse {
	if tu == nil {
		return nil
	}
	return &toolUse{
		ID:   tu.ID,
		Name: tu.Name,
	}
}

// stripThinkingBlocks returns a copy of the message with thinking and
// redacted_thinking content blocks removed. Used to strip stale thinking
// from older assistant turns before sending to the API.
func stripThinkingBlocks(msg llm.Message) llm.Message {
	var filtered []llm.Content
	for _, c := range msg.Content {
		if c.Type == llm.ContentTypeThinking || c.Type == llm.ContentTypeRedactedThinking {
			continue
		}
		filtered = append(filtered, c)
	}
	msg.Content = filtered
	return msg
}

func fromLLMMessage(msg llm.Message) message {
	var contents []content
	for _, c := range msg.Content {
		// Skip thinking blocks with no signature — they're corrupt/incomplete
		// and the API rejects them.
		if c.Type == llm.ContentTypeThinking && c.Signature == "" {
			continue
		}
		contents = append(contents, fromLLMContent(c))
	}
	return message{
		Role:    fromLLMRole[msg.Role],
		Content: contents,
		ToolUse: fromLLMToolUse(msg.ToolUse),
	}
}

func fromLLMToolChoice(tc *llm.ToolChoice) *toolChoice {
	if tc == nil {
		return nil
	}
	return &toolChoice{
		Type: fromLLMToolChoiceType[tc.Type],
		Name: tc.Name,
	}
}

func fromLLMTool(t *llm.Tool) *tool {
	return &tool{
		Name:         t.Name,
		Type:         t.Type,
		Description:  t.Description,
		InputSchema:  t.InputSchema,
		CacheControl: fromLLMCache(t.Cache),
	}
}

func fromLLMSystem(s llm.SystemContent) systemContent {
	return systemContent{
		Text:         s.Text,
		Type:         s.Type,
		CacheControl: fromLLMCache(s.Cache),
	}
}

func (s *Service) fromLLMRequest(r *llm.Request, isOAuth bool) *request {
	model := cmp.Or(s.Model, DefaultModel)
	maxTokens := cmp.Or(s.MaxTokens, maxOutputTokens(model))

	// Find the last assistant message index so we can strip thinking blocks
	// from all earlier assistant messages. The Anthropic API validates thinking
	// signatures, and they become invalid when the underlying model version
	// rotates (e.g. "claude-opus-4-6" points to a new version). Only the
	// most recent assistant turn's thinking blocks need to be preserved.
	lastAssistantIdx := -1
	for i := len(r.Messages) - 1; i >= 0; i-- {
		if r.Messages[i].Role == llm.MessageRoleAssistant {
			lastAssistantIdx = i
			break
		}
	}

	var messages []message
	for i, m := range r.Messages {
		// Strip thinking/redacted_thinking blocks from all assistant messages
		// except the last one. This avoids "Invalid signature" errors when
		// the model version has changed since the thinking was generated.
		if m.Role == llm.MessageRoleAssistant && i != lastAssistantIdx {
			m = stripThinkingBlocks(m)
		}
		msg := fromLLMMessage(m)
		if len(msg.Content) > 0 {
			messages = append(messages, msg)
		}
	}

	req := &request{
		Model:      model,
		Messages:   messages,
		MaxTokens:  maxTokens,
		ToolChoice: fromLLMToolChoice(r.ToolChoice),
		Tools:      mapped(r.Tools, fromLLMTool),
		System:     buildSystemContent(r, isOAuth),
	}

	// Enable extended thinking if a thinking level is set
	if s.ThinkingLevel != llm.ThinkingLevelOff {
		budget := s.ThinkingLevel.ThinkingBudgetTokens()
		// Ensure max_tokens > budget_tokens as required by Anthropic API
		if maxTokens <= budget {
			req.MaxTokens = budget + 1024
		}
		req.Thinking = &thinking{Type: "enabled", BudgetTokens: budget}
	}

	// Cap max_tokens at the model's maximum allowed output tokens
	if limit := s.maxOutputTokens(); req.MaxTokens > limit {
		req.MaxTokens = limit
		// Also cap the thinking budget if it exceeds the new max_tokens
		if req.Thinking != nil && req.Thinking.BudgetTokens >= req.MaxTokens {
			req.Thinking.BudgetTokens = req.MaxTokens - 1024
		}
	}
	return req
}

// buildSystemContent builds the system content list, prepending the OAuth
// prefix when isOAuth is true.
func buildSystemContent(r *llm.Request, isOAuth bool) []systemContent {
	var system []systemContent
	if isOAuth {
		system = append(system, systemContent{
			Type:         "text",
			Text:         oauthSystemPrefix,
			CacheControl: json.RawMessage(`{"type":"ephemeral"}`),
		})
	}
	for _, sc := range r.System {
		system = append(system, fromLLMSystem(sc))
	}
	return system
}

// fromLLMRequestStrippingAllThinking is like fromLLMRequest but strips thinking
// blocks from ALL assistant messages (including the last one). Used as a fallback
// when the API rejects thinking signatures — e.g. after model version rotation.
func (s *Service) fromLLMRequestStrippingAllThinking(r *llm.Request, isOAuth bool) *request {
	model := cmp.Or(s.Model, DefaultModel)
	maxTokens := cmp.Or(s.MaxTokens, maxOutputTokens(model))

	var messages []message
	for _, m := range r.Messages {
		if m.Role == llm.MessageRoleAssistant {
			m = stripThinkingBlocks(m)
		}
		msg := fromLLMMessage(m)
		if len(msg.Content) > 0 {
			messages = append(messages, msg)
		}
	}
	req := &request{
		Model:      model,
		Messages:   messages,
		MaxTokens:  maxTokens,
		ToolChoice: fromLLMToolChoice(r.ToolChoice),
		Tools:      mapped(r.Tools, fromLLMTool),
		System:     buildSystemContent(r, isOAuth),
	}

	if s.ThinkingLevel != llm.ThinkingLevelOff {
		budget := s.ThinkingLevel.ThinkingBudgetTokens()
		if maxTokens <= budget {
			req.MaxTokens = budget + 1024
		}
		req.Thinking = &thinking{Type: "enabled", BudgetTokens: budget}
	}

	if limit := s.maxOutputTokens(); req.MaxTokens > limit {
		req.MaxTokens = limit
		if req.Thinking != nil && req.Thinking.BudgetTokens >= req.MaxTokens {
			req.Thinking.BudgetTokens = req.MaxTokens - 1024
		}
	}
	return req
}

func toLLMUsage(u usage) llm.Usage {
	return llm.Usage{
		InputTokens:              u.InputTokens,
		CacheCreationInputTokens: u.CacheCreationInputTokens,
		CacheReadInputTokens:     u.CacheReadInputTokens,
		OutputTokens:             u.OutputTokens,
		CostUSD:                  u.CostUSD,
	}
}

func toLLMContent(c content) llm.Content {
	// Convert toolResult from []content to []llm.Content
	var toolResultContents []llm.Content
	if len(c.ToolResult) > 0 {
		toolResultContents = make([]llm.Content, len(c.ToolResult))
		for i, tr := range c.ToolResult {
			toolResultContents[i] = toLLMContent(tr)
		}
	}

	ret := llm.Content{
		ID:         c.ID,
		Type:       toLLMContentType[c.Type],
		MediaType:  c.MediaType,
		Data:       c.Data,
		Signature:  c.Signature,
		ToolName:   c.ToolName,
		ToolInput:  c.ToolInput,
		ToolUseID:  c.ToolUseID,
		ToolError:  c.ToolError,
		ToolResult: toolResultContents,
	}
	if c.Text != nil {
		ret.Text = *c.Text
	}
	if c.Thinking != nil {
		ret.Thinking = *c.Thinking
	}
	return ret
}

func toLLMResponse(r *response) *llm.Response {
	return &llm.Response{
		ID:           r.ID,
		Type:         r.Type,
		Role:         toLLMRole[r.Role],
		Model:        r.Model,
		Content:      mapped(r.Content, toLLMContent),
		StopReason:   toLLMStopReason[r.StopReason],
		StopSequence: r.StopSequence,
		Usage:        toLLMUsage(r.Usage),
	}
}

// streamEvent represents a single SSE event from the Anthropic streaming API.
type streamEvent struct {
	Type string `json:"type"`

	// message_start
	Message *response `json:"message,omitempty"`

	// content_block_start
	Index        int      `json:"index,omitempty"`
	ContentBlock *content `json:"content_block,omitempty"`

	// content_block_delta
	Delta json.RawMessage `json:"delta,omitempty"`

	// message_delta
	Usage *usage `json:"usage,omitempty"`
}

// streamDelta represents the delta field in content_block_delta and message_delta events.
type streamDelta struct {
	Type string `json:"type"`

	// text_delta
	Text string `json:"text,omitempty"`

	// thinking_delta
	Thinking string `json:"thinking,omitempty"`

	// input_json_delta
	PartialJSON string `json:"partial_json,omitempty"`

	// signature_delta
	Signature string `json:"signature,omitempty"`

	// message_delta
	StopReason   string  `json:"stop_reason,omitempty"`
	StopSequence *string `json:"stop_sequence,omitempty"`
}

// sseEvent represents a parsed Server-Sent Event per the SSE spec.
// See https://html.spec.whatwg.org/multipage/server-sent-events.html#event-stream-interpretation
type sseEvent struct {
	EventType string // from "event:" field; empty if not set
	Data      string // from "data:" field(s); multiple data lines joined with "\n"
}

// iterSSEEvents reads an SSE stream and yields parsed events.
// It follows the SSE spec: events are delimited by blank lines,
// multiple "data:" lines are joined with "\n", and the "event:" field
// sets the event type.
func iterSSEEvents(r io.Reader, yield func(sseEvent) error) error {
	scanner := bufio.NewScanner(r)
	// SSE lines can be large (e.g. tool input JSON).
	// Max buffer: 10MB to handle very large content blocks.
	scanner.Buffer(make([]byte, 0, 64*1024), 10*1024*1024)

	var (
		eventType string
		dataLines []string
		hasData   bool
	)

	dispatch := func() error {
		if !hasData {
			// Reset and skip — no data fields means no event to dispatch
			eventType = ""
			return nil
		}
		ev := sseEvent{
			EventType: eventType,
			Data:      strings.Join(dataLines, "\n"),
		}
		// Reset state
		eventType = ""
		dataLines = dataLines[:0]
		hasData = false
		return yield(ev)
	}

	for scanner.Scan() {
		line := scanner.Text()

		// Blank line dispatches the event
		if line == "" {
			if err := dispatch(); err != nil {
				return err
			}
			continue
		}

		// Lines starting with ':' are comments
		if strings.HasPrefix(line, ":") {
			continue
		}

		// Split into field name and value
		var field, value string
		if idx := strings.IndexByte(line, ':'); idx >= 0 {
			field = line[:idx]
			value = line[idx+1:]
			// SSE spec: if value starts with a space, remove it
			if strings.HasPrefix(value, " ") {
				value = value[1:]
			}
		} else {
			field = line
		}

		switch field {
		case "event":
			eventType = value
		case "data":
			dataLines = append(dataLines, value)
			hasData = true
			// "id" and "retry" fields are ignored for our use case
		}
	}

	if err := scanner.Err(); err != nil {
		return fmt.Errorf("reading SSE stream: %w", err)
	}

	// Dispatch any trailing event (stream ended without final blank line)
	return dispatch()
}

// truncateForError returns a string representation of data suitable for error messages,
// truncating to a reasonable length.
func truncateForError(data string, maxLen int) string {
	if len(data) <= maxLen {
		return data
	}
	return data[:maxLen] + fmt.Sprintf("... (%d bytes total)", len(data))
}

// parseSSEStream reads an SSE stream and assembles the complete response.
func parseSSEStream(r io.Reader) (*response, error) {
	var (
		resp        *response
		contents    []content // indexed by content block index
		messageDone bool
	)

	err := iterSSEEvents(r, func(sse sseEvent) error {
		data := sse.Data
		if data == "[DONE]" {
			return nil
		}

		var event streamEvent
		if err := json.Unmarshal([]byte(data), &event); err != nil {
			return fmt.Errorf("parsing SSE event (event=%q, data=%s): %w",
				sse.EventType, truncateForError(data, 512), err)
		}

		switch event.Type {
		case "message_start":
			if event.Message == nil {
				return fmt.Errorf("message_start event has no message")
			}
			resp = event.Message
			resp.Content = nil // will be rebuilt from content blocks

		case "content_block_start":
			if event.ContentBlock == nil {
				return fmt.Errorf("content_block_start event has no content_block")
			}
			// Grow slice to accommodate index
			for len(contents) <= event.Index {
				contents = append(contents, content{})
			}
			block := *event.ContentBlock
			// For tool_use blocks, the initial input is always empty {};
			// clear it so delta accumulation starts fresh.
			if block.Type == "tool_use" {
				block.ToolInput = nil
			}
			contents[event.Index] = block

		case "content_block_delta":
			if event.Index >= len(contents) {
				return fmt.Errorf("content_block_delta index %d out of range", event.Index)
			}
			var delta streamDelta
			if err := json.Unmarshal(event.Delta, &delta); err != nil {
				return fmt.Errorf("parsing content_block_delta: %w", err)
			}
			c := &contents[event.Index]
			switch delta.Type {
			case "text_delta":
				if c.Text == nil {
					c.Text = new(string)
				}
				*c.Text += delta.Text
			case "thinking_delta":
				if c.Thinking == nil {
					c.Thinking = new(string)
				}
				*c.Thinking += delta.Thinking
			case "input_json_delta":
				// Accumulate raw JSON for tool_use input
				c.ToolInput = append(c.ToolInput, []byte(delta.PartialJSON)...)
			case "signature_delta":
				c.Signature += delta.Signature
			}

		case "content_block_stop":
			// nothing to do; the block is already assembled

		case "message_delta":
			var delta streamDelta
			if err := json.Unmarshal(event.Delta, &delta); err != nil {
				return fmt.Errorf("parsing message_delta: %w", err)
			}
			if resp != nil {
				resp.StopReason = delta.StopReason
				resp.StopSequence = delta.StopSequence
			}
			if event.Usage != nil && resp != nil {
				// message_delta usage contains output_tokens
				resp.Usage.OutputTokens = event.Usage.OutputTokens
			}

		case "message_stop":
			messageDone = true

		case "ping":
			// keepalive, ignore

		case "error":
			return fmt.Errorf("stream error event: %s", data)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	if resp == nil {
		return nil, fmt.Errorf("no message_start event in stream")
	}

	if !messageDone {
		return nil, fmt.Errorf("incomplete stream: no stop_reason received (stream may have been truncated)")
	}

	// Ensure tool_use blocks always have a non-nil ToolInput.
	// When a tool has empty input {}, the stream sends input_json_delta
	// with partial_json:"", and append(nil, []byte("")...) stays nil.
	// Anthropic requires the "input" field on tool_use blocks, and
	// json:"input,omitempty" omits nil, causing a 400 error.
	for i := range contents {
		if contents[i].Type == "tool_use" && contents[i].ToolInput == nil {
			contents[i].ToolInput = json.RawMessage("{}")
		}
	}

	resp.Content = contents
	return resp, nil
}

// loadOAuthToken reads the current OAuth token from wherever it's stored.
// Returns nil if the API key is not an OAuth token.
func (s *Service) loadOAuthToken() (*oauthToken, error) {
	if isOAuthFilePath(s.APIKey) {
		return readOAuthFromFile(s.APIKey)
	}
	tok := parseOAuthToken(s.APIKey)
	return tok, nil // nil tok is fine — means not OAuth
}

// ensureValidOAuthToken loads the OAuth token and refreshes it if expired.
// Returns the valid token (or nil if not OAuth). Persists refreshed tokens.
func (s *Service) ensureValidOAuthToken(ctx context.Context) (*oauthToken, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()

	tok, err := s.loadOAuthToken()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nil
	}
	if !tok.IsExpired() {
		return tok, nil
	}
	return s.refreshAndPersist(ctx, tok)
}

// reloadAndRefreshOAuthToken re-reads the token from source of truth, and if
// still expired, refreshes it. Used when the API returns an auth error.
func (s *Service) reloadAndRefreshOAuthToken(ctx context.Context) (*oauthToken, error) {
	s.oauthMu.Lock()
	defer s.oauthMu.Unlock()

	// First, reload the API key from the source of truth (DB) if available
	if s.OnTokenReload != nil {
		if reloaded := s.OnTokenReload(); reloaded != "" {
			s.APIKey = reloaded
		}
	}

	tok, err := s.loadOAuthToken()
	if err != nil {
		return nil, err
	}
	if tok == nil {
		return nil, nil
	}
	if !tok.IsExpired() {
		return tok, nil
	}
	return s.refreshAndPersist(ctx, tok)
}

// refreshAndPersist refreshes the token and writes it back to its source.
// Must be called with oauthMu held.
func (s *Service) refreshAndPersist(ctx context.Context, tok *oauthToken) (*oauthToken, error) {
	newTok, err := s.refreshOAuthToken(ctx, tok)
	if err != nil {
		return nil, fmt.Errorf("oauth token refresh failed: %w", err)
	}
	if isOAuthFilePath(s.APIKey) {
		if err := writeOAuthToFile(s.APIKey, newTok); err != nil {
			slog.Error("failed to write refreshed OAuth token to file", "path", s.APIKey, "error", err)
		}
	} else {
		s.APIKey = newTok.String()
		if s.OnTokenRefresh != nil {
			s.OnTokenRefresh(s.APIKey)
		}
	}
	return newTok, nil
}

// Do sends a streaming request to Anthropic and collects the full response.
func (s *Service) Do(ctx context.Context, ir *llm.Request) (*llm.Response, error) {
	startTime := time.Now()

	oauthTok, err := s.ensureValidOAuthToken(ctx)
	if err != nil {
		return nil, err
	}

	isOAuth := oauthTok != nil
	request := s.fromLLMRequest(ir, isOAuth)
	request.Stream = true
	payload, err := json.Marshal(request)
	if err != nil {
		return nil, err
	}
	payload = append(payload, '\n')

	// strippedPayload is built lazily on the first "Invalid signature" error.
	// It strips ALL thinking blocks from the request as a fallback.
	var strippedPayload []byte

	// oauthRetried tracks whether we've already attempted a token reload+refresh
	// in response to an auth error, to avoid infinite retry loops.
	var oauthRetried bool

	backoff := s.Backoff
	if backoff == nil {
		backoff = []time.Duration{15 * time.Second, 30 * time.Second, time.Minute}
	}

	url := cmp.Or(s.URL, DefaultURL)
	httpc := cmp.Or(s.HTTPC, http.DefaultClient)

	// retry loop
	var errs error // accumulated errors across all attempts
	for attempts := 0; ; attempts++ {
		if attempts > 10 {
			return nil, fmt.Errorf("anthropic request failed after %d attempts: %w", attempts, errs)
		}
		if attempts > 0 {
			// Bail out early if context is already done — no point sleeping
			// and retrying when every attempt will fail immediately.
			if ctx.Err() != nil {
				return nil, fmt.Errorf("anthropic request failed after %d attempts (context cancelled): %w", attempts, errs)
			}
			sleep := backoff[min(attempts-1, len(backoff)-1)] + time.Duration(rand.Int64N(int64(time.Second)))
			slog.WarnContext(ctx, "anthropic request sleep before retry", "sleep", sleep, "attempts", attempts)
			select {
			case <-time.After(sleep):
			case <-ctx.Done():
				return nil, fmt.Errorf("anthropic request failed after %d attempts (context cancelled during backoff): %w", attempts, errs)
			}
		}
		req, err := http.NewRequestWithContext(ctx, "POST", url, bytes.NewReader(payload))
		if err != nil {
			return nil, errors.Join(errs, err)
		}

		req.Header.Set("Content-Type", "application/json")
		req.Header.Set("Anthropic-Version", "2023-06-01")

		if isOAuth {
			// OAuth tokens use Bearer auth and require additional headers
			req.Header.Set("Authorization", "Bearer "+oauthTok.AccessToken)
			req.Header.Set("anthropic-dangerous-direct-browser-access", "true")
			req.Header.Set("anthropic-beta", oauthBetaFeatures)
			req.Header.Set("User-Agent", oauthUserAgent)
			req.Header.Set("x-app", "cli")
		} else {
			// Standard API key auth
			req.Header.Set("X-API-Key", s.APIKey)
		}

		resp, err := httpc.Do(req)
		if err != nil {
			// Don't retry httprr cache misses
			if strings.Contains(err.Error(), "cached HTTP response not found") {
				return nil, err
			}
			errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: %w", attempts+1, time.Now().Format(time.DateTime), err))
			continue
		}

		switch {
		case resp.StatusCode == http.StatusOK:
			response, err := parseSSEStream(resp.Body)
			resp.Body.Close()
			if err != nil {
				// Stream parse errors might be transient (connection reset, etc.)
				errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: %w", attempts+1, time.Now().Format(time.DateTime), err))
				continue
			}
			// Calculate and set the cost_usd field
			response.Usage.CostUSD = llm.CostUSDFromResponse(resp.Header)

			endTime := time.Now()
			result := toLLMResponse(response)
			result.StartTime = &startTime
			result.EndTime = &endTime
			return result, nil
		default:
			buf, _ := io.ReadAll(resp.Body)
			resp.Body.Close()

			switch {
			case resp.StatusCode >= 500 && resp.StatusCode < 600:
				// server error, retry
				slog.WarnContext(ctx, "anthropic_request_failed", "response", string(buf), "status_code", resp.StatusCode, "url", url, "model", s.Model)
				errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: status %v (url=%s, model=%s): %s", attempts+1, time.Now().Format(time.DateTime), resp.Status, url, cmp.Or(s.Model, DefaultModel), buf))
				continue
			case resp.StatusCode == 429:
				// rate limited, retry
				slog.WarnContext(ctx, "anthropic_request_rate_limited", "response", string(buf), "url", url, "model", s.Model)
				errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: status %v (url=%s, model=%s): %s", attempts+1, time.Now().Format(time.DateTime), resp.Status, url, cmp.Or(s.Model, DefaultModel), buf))
				continue
			case resp.StatusCode == 401 && isOAuth && !oauthRetried:
				// Auth error with OAuth — reload token from source of truth and retry.
				// This handles cases where the token was refreshed externally (e.g.,
				// another process updated the JSON file or DB).
				oauthRetried = true
				slog.WarnContext(ctx, "anthropic_oauth_auth_error, reloading token",
					"response", string(buf), "url", url, "model", s.Model)
				newTok, refreshErr := s.reloadAndRefreshOAuthToken(ctx)
				if refreshErr != nil {
					return nil, errors.Join(errs, fmt.Errorf("attempt %d at %s: status 401, token reload/refresh failed: %w",
						attempts+1, time.Now().Format(time.DateTime), refreshErr))
				}
				if newTok != nil {
					oauthTok = newTok
				}
				errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: status 401, retrying with reloaded token",
					attempts+1, time.Now().Format(time.DateTime)))
				continue
			case resp.StatusCode >= 400 && resp.StatusCode < 500:
				// Check for "Invalid signature" in thinking blocks — this happens
				// when the model version rotated and old signatures are no longer valid.
				// Retry once with ALL thinking blocks stripped from the request.
				if strippedPayload == nil && strings.Contains(string(buf), "Invalid `signature`") {
					slog.WarnContext(ctx, "anthropic_invalid_thinking_signature, retrying without thinking blocks",
						"response", string(buf), "url", url, "model", s.Model)
					strippedReq := s.fromLLMRequestStrippingAllThinking(ir, isOAuth)
					strippedReq.Stream = true
					strippedPayload, err = json.Marshal(strippedReq)
					if err != nil {
						return nil, errors.Join(errs, fmt.Errorf("failed to marshal stripped request: %w", err))
					}
					strippedPayload = append(strippedPayload, '\n')
					payload = strippedPayload
					errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: invalid thinking signature, retrying without thinking blocks", attempts+1, time.Now().Format(time.DateTime)))
					continue
				}
				// some other 400, probably unrecoverable
				slog.WarnContext(ctx, "anthropic_request_failed", "response", string(buf), "status_code", resp.StatusCode, "url", url, "model", s.Model)
				return nil, errors.Join(errs, fmt.Errorf("attempt %d at %s: status %v (url=%s, model=%s): %s", attempts+1, time.Now().Format(time.DateTime), resp.Status, url, cmp.Or(s.Model, DefaultModel), buf))
			default:
				// ...retry, I guess?
				slog.WarnContext(ctx, "anthropic_request_failed", "response", string(buf), "status_code", resp.StatusCode, "url", url, "model", s.Model)
				errs = errors.Join(errs, fmt.Errorf("attempt %d at %s: status %v (url=%s, model=%s): %s", attempts+1, time.Now().Format(time.DateTime), resp.Status, url, cmp.Or(s.Model, DefaultModel), buf))
				continue
			}
		}
	}
}

// For debugging only, Claude can definitely handle the full patch tool.
// func (s *Service) UseSimplifiedPatch() bool {
// 	return true
// }

// ConfigDetails returns configuration information for logging
func (s *Service) ConfigDetails() map[string]string {
	model := cmp.Or(s.Model, DefaultModel)
	url := cmp.Or(s.URL, DefaultURL)
	return map[string]string{
		"url":             url,
		"model":           model,
		"has_api_key_set": fmt.Sprintf("%v", s.APIKey != ""),
	}
}
