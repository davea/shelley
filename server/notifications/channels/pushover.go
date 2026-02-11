package channels

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"time"

	"shelley.exe.dev/server/notifications"
)

const pushoverAPIURL = "https://api.pushover.net/1/messages.json"

func init() {
	notifications.Register("pushover", func(config map[string]any, logger *slog.Logger) (notifications.Channel, error) {
		userKey, ok := config["user_key"].(string)
		if !ok || userKey == "" {
			return nil, fmt.Errorf("pushover channel requires \"user_key\"")
		}
		appKey, ok := config["app_key"].(string)
		if !ok || appKey == "" {
			return nil, fmt.Errorf("pushover channel requires \"app_key\"")
		}
		return newPushover(userKey, appKey), nil
	})
}

type pushover struct {
	userKey string
	appKey  string
	client  *http.Client
}

func newPushover(userKey, appKey string) *pushover {
	return &pushover{
		userKey: userKey,
		appKey:  appKey,
		client: &http.Client{
			Timeout: 10 * time.Second,
		},
	}
}

func (p *pushover) Name() string { return "pushover" }

func (p *pushover) Send(ctx context.Context, event notifications.Event) error {
	title, body, slug := formatPushoverMessage(event)
	if title == "" {
		return nil
	}

	msg := map[string]string{
		"token":   p.appKey,
		"user":    p.userKey,
		"title":   title,
		"message": body,
	}
	if hostname, err := os.Hostname(); err == nil && slug != "" {
		msg["url"] = "https://" + hostname + ".shelley.exe.xyz/c/" + slug
		msg["url_title"] = "Open conversation"
	}

	payload, err := json.Marshal(msg)
	if err != nil {
		return fmt.Errorf("marshal pushover payload: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, pushoverAPIURL, bytes.NewReader(payload))
	if err != nil {
		return fmt.Errorf("create pushover request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.client.Do(req)
	if err != nil {
		return fmt.Errorf("send pushover notification: %w", err)
	}
	defer resp.Body.Close()
	io.Copy(io.Discard, resp.Body)

	if resp.StatusCode >= 400 {
		return fmt.Errorf("pushover API returned %d", resp.StatusCode)
	}
	return nil
}

func formatPushoverMessage(event notifications.Event) (title, body, slug string) {
	hostname, _ := os.Hostname()
	if hostname == "" {
		hostname = "shelley"
	}
	title = hostname

	switch event.Type {
	case notifications.EventAgentDone:
		if p, ok := event.Payload.(notifications.AgentDonePayload); ok {
			slug = p.ConversationTitle
			if p.ConversationTitle != "" {
				body = p.ConversationTitle
			} else {
				body = "Agent finished"
			}
			if p.FinalResponse != "" {
				body += "\n" + p.FinalResponse
			}
		} else {
			body = "Agent finished"
		}

	case notifications.EventAgentError:
		body = "Agent error"
		if p, ok := event.Payload.(notifications.AgentErrorPayload); ok && p.ErrorMessage != "" {
			body += "\n" + p.ErrorMessage
		}

	default:
		return "", "", ""
	}
	return title, body, slug
}
