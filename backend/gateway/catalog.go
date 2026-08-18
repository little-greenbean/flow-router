package gateway

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"os"
	"strings"
	"time"
)

const (
	defaultOfficialModelCatalogURL = "https://basellm.github.io/llm-metadata/api/newapi/models.json"
	officialModelCatalogTimeout    = 15 * time.Second
	officialModelCatalogMaxBytes   = 10 << 20
)

// CatalogModel is a model entry from the Sub2API-compatible public catalog.
// It intentionally contains metadata only; no upstream credentials are part of
// the catalog response.
type CatalogModel struct {
	ID          string `json:"model_name"`
	Description string `json:"description"`
	Icon        string `json:"icon"`
	Tags        string `json:"tags"`
	Vendor      string `json:"vendor_name"`
	Status      int    `json:"status"`
}

// FetchOfficialModelCatalog fetches the public model metadata used by Sub2API.
// The URL is overridable for local mirrors and tests via
// UPSTREAM_OPS_MODEL_CATALOG_URL.
func FetchOfficialModelCatalog(ctx context.Context) ([]CatalogModel, error) {
	url := strings.TrimSpace(os.Getenv("UPSTREAM_OPS_MODEL_CATALOG_URL"))
	if url == "" {
		url = defaultOfficialModelCatalogURL
	}

	client := &http.Client{Timeout: officialModelCatalogTimeout}
	var lastErr error
	for attempt := 0; attempt < 3; attempt++ {
		models, err := fetchCatalogAttempt(ctx, client, url)
		if err == nil {
			return models, nil
		}
		lastErr = err
		if attempt == 2 {
			break
		}
		delay := time.Duration(100*(1<<attempt)) * time.Millisecond
		timer := time.NewTimer(delay)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
	return nil, fmt.Errorf("fetch official model catalog: %w", lastErr)
}

func fetchCatalogAttempt(ctx context.Context, client *http.Client, url string) ([]CatalogModel, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, url, nil)
	if err != nil {
		return nil, fmt.Errorf("build request: %w", err)
	}
	req.Header.Set("Accept", "application/json")
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return nil, fmt.Errorf("catalog returned HTTP %d", resp.StatusCode)
	}
	body, err := io.ReadAll(io.LimitReader(resp.Body, officialModelCatalogMaxBytes+1))
	if err != nil {
		return nil, fmt.Errorf("read catalog: %w", err)
	}
	if len(body) > officialModelCatalogMaxBytes {
		return nil, fmt.Errorf("catalog response exceeds %d bytes", officialModelCatalogMaxBytes)
	}
	return parseCatalogBody(body)
}

func parseCatalogBody(body []byte) ([]CatalogModel, error) {
	body = []byte(strings.TrimSpace(string(body)))
	if len(body) == 0 {
		return nil, fmt.Errorf("catalog response is empty")
	}

	var raw []CatalogModel
	if body[0] == '[' {
		if err := json.Unmarshal(body, &raw); err != nil {
			return nil, fmt.Errorf("decode catalog array: %w", err)
		}
	} else {
		var envelope struct {
			Data []CatalogModel `json:"data"`
		}
		if err := json.Unmarshal(body, &envelope); err != nil {
			return nil, fmt.Errorf("decode catalog object: %w", err)
		}
		raw = envelope.Data
	}

	out := make([]CatalogModel, 0, len(raw))
	seen := make(map[string]struct{}, len(raw))
	for _, model := range raw {
		model.ID = strings.TrimSpace(model.ID)
		if model.ID == "" {
			continue
		}
		if _, ok := seen[model.ID]; ok {
			continue
		}
		seen[model.ID] = struct{}{}
		out = append(out, model)
	}
	return out, nil
}
