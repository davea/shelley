package server

import (
	"context"
	"encoding/json"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"testing"

	"shelley.exe.dev/loop"
	"shelley.exe.dev/models"
)

func TestHandleModelRefreshReturnsRefreshedModels(t *testing.T) {
	mgr, err := models.NewManager(&models.Config{
		Models: []models.Built{
			{
				ID:       "old-built",
				Provider: models.ProviderBuiltIn,
				Source:   "old source",
				Service:  loop.NewPredictableService(),
			},
		},
		Logger: slog.Default(),
	})
	if err != nil {
		t.Fatalf("NewManager failed: %v", err)
	}
	s := &Server{
		llmManager: mgr,
		logger:     slog.Default(),
		refreshBuiltModels: func(context.Context) ([]models.Built, error) {
			return []models.Built{
				{
					ID:       "new-built",
					Provider: models.ProviderBuiltIn,
					Source:   "new source",
					Service:  loop.NewPredictableService(),
				},
			}, nil
		},
	}

	req := httptest.NewRequest(http.MethodPost, "/api/models/refresh", nil)
	rec := httptest.NewRecorder()
	s.handleModelRefresh(rec, req)

	if rec.Code != http.StatusOK {
		t.Fatalf("status = %d, body = %q", rec.Code, rec.Body.String())
	}
	var got []ModelInfo
	if err := json.NewDecoder(rec.Body).Decode(&got); err != nil {
		t.Fatalf("decode response: %v", err)
	}
	if len(got) != 1 || got[0].ID != "new-built" || got[0].Source != "new source" {
		t.Fatalf("models = %+v, want only new-built from new source", got)
	}
	if mgr.HasModel("old-built") {
		t.Fatal("old built model was not removed")
	}
}
