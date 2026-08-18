package gateway

import "testing"

func TestParseCatalogBodySub2APIShape(t *testing.T) {
	body := []byte(`{"data":[{"model_name":"gpt-5.6","description":"latest","vendor_name":"openai","tags":"reasoning","status":1},{"model_name":""},{"model_name":"gpt-5.6","vendor_name":"duplicate"}]}`)
	models, err := parseCatalogBody(body)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if len(models) != 1 {
		t.Fatalf("expected one de-duplicated model, got %d: %#v", len(models), models)
	}
	got := models[0]
	if got.ID != "gpt-5.6" || got.Description != "latest" || got.Vendor != "openai" || got.Tags != "reasoning" || got.Status != 1 {
		t.Fatalf("unexpected model: %#v", got)
	}
}

func TestParseCatalogBodyBareArray(t *testing.T) {
	body := []byte(`[{"model_name":"claude-4","icon":"anthropic"},{"id":"ignored"}]`)
	models, err := parseCatalogBody(body)
	if err != nil {
		t.Fatalf("parse catalog: %v", err)
	}
	if len(models) != 1 || models[0].ID != "claude-4" || models[0].Icon != "anthropic" {
		t.Fatalf("unexpected models: %#v", models)
	}
}
