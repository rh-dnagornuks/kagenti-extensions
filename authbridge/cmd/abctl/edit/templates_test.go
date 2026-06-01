package edit

import (
	"context"
	"os"
	"strings"
	"testing"

	"github.com/kagenti/kagenti-extensions/authbridge/cmd/abctl/apiclient"
)

func TestRenderTemplates_EmptyCatalog(t *testing.T) {
	if got := RenderTemplates(nil); got != nil {
		t.Fatalf("nil catalog should produce nil output, got %q", string(got))
	}
	if got := RenderTemplates([]apiclient.PluginCatalogEntry{}); got != nil {
		t.Fatalf("empty catalog should produce nil output, got %q", string(got))
	}
}

func TestRenderTemplates_FenceMarkerPresent(t *testing.T) {
	out := RenderTemplates([]apiclient.PluginCatalogEntry{{Name: "noop"}})
	if !strings.Contains(string(out), FenceMarker) {
		t.Fatalf("output missing fence marker:\n%s", string(out))
	}
}

func TestRenderTemplates_PluginWithFields(t *testing.T) {
	cat := []apiclient.PluginCatalogEntry{
		{
			Name:        "ibac",
			Description: "Intent-based access control via LLM judge.",
			Fields: []apiclient.PluginFieldEntry{
				{Name: "judge_endpoint", Type: "string", Required: true,
					Description: "Base URL of the LLM judge."},
				{Name: "judge_model", Type: "string", Required: true,
					Description: "Model name."},
				{Name: "timeout_ms", Type: "int", Default: "5000",
					Description: "Per-call timeout."},
				{Name: "unclassified_policy", Type: "string", Default: "passthrough",
					Enum: []string{"passthrough", "judge"},
					Description: "Behavior when no parser claimed the request."},
			},
		},
	}
	out := string(RenderTemplates(cat))

	for _, want := range []string{
		"# --- ibac ---",
		"Intent-based access control via LLM judge.",
		"# Required: judge_endpoint, judge_model",
		"#       - name: ibac",
		"#         config:",
		`#           judge_endpoint: ""  # required; Base URL of the LLM judge.`,
		"#           timeout_ms: 5000  # default=5000; Per-call timeout.",
		"enum=passthrough|judge",
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n----\n%s", want, out)
		}
	}

	// Required fields should appear before optional ones.
	reqIdx := strings.Index(out, "judge_endpoint:")
	optIdx := strings.Index(out, "timeout_ms:")
	if reqIdx < 0 || optIdx < 0 {
		t.Fatalf("could not locate fields in output:\n%s", out)
	}
	if reqIdx > optIdx {
		t.Errorf("required fields should render before optional ones; got required at %d, optional at %d", reqIdx, optIdx)
	}
}

func TestRenderTemplates_PluginNoFields(t *testing.T) {
	cat := []apiclient.PluginCatalogEntry{
		{Name: "a2a-parser", Description: "A2A protocol parser."},
	}
	out := string(RenderTemplates(cat))
	if !strings.Contains(out, "# (no configurable fields)") {
		t.Fatalf("expected no-fields hint:\n%s", out)
	}
	if strings.Contains(out, "config:") {
		t.Errorf("plugin without fields shouldn't emit config: line:\n%s", out)
	}
}

func TestRenderTemplates_PlaceholderTypes(t *testing.T) {
	cat := []apiclient.PluginCatalogEntry{
		{
			Name: "broker",
			Fields: []apiclient.PluginFieldEntry{
				{Name: "name", Type: "string"},
				{Name: "count", Type: "int"},
				{Name: "flag", Type: "bool"},
				{Name: "items", Type: "[]string"},
				{Name: "nested", Type: "object"},
				{Name: "with_default", Type: "string", Default: "abc"},
			},
		},
	}
	out := string(RenderTemplates(cat))
	for _, want := range []string{
		`name: ""`,
		"count: 0",
		"flag: false",
		"items: []",
		"nested: {}",
		`with_default: "abc"`, // string default should be quoted
	} {
		if !strings.Contains(out, want) {
			t.Errorf("output missing %q\n----\n%s", want, out)
		}
	}
}

func TestFetchCmd_AppendsTemplatesWhenCatalogProvided(t *testing.T) {
	r := func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(fixtureCMYAML), nil
	}
	cat := []apiclient.PluginCatalogEntry{
		{Name: "ibac", Description: "test plugin"},
	}
	cmd := FetchCmd(context.Background(), r, "team1", "email-agent", cat)
	msg := cmd().(FetchedMsg)
	if msg.Err != nil {
		t.Fatalf("FetchCmd err: %v", msg.Err)
	}
	body, err := os.ReadFile(msg.TempPath)
	if err != nil {
		t.Fatalf("read tempfile: %v", err)
	}
	if !strings.Contains(string(body), FenceMarker) {
		t.Fatalf("tempfile missing fence marker; templates not appended:\n%s", string(body))
	}
	if !strings.Contains(string(body), "# --- ibac ---") {
		t.Fatalf("tempfile missing plugin block:\n%s", string(body))
	}
}

func TestFetchCmd_NoTemplatesWhenCatalogNil(t *testing.T) {
	r := func(_ context.Context, _ ...string) ([]byte, error) {
		return []byte(fixtureCMYAML), nil
	}
	cmd := FetchCmd(context.Background(), r, "team1", "email-agent", nil)
	msg := cmd().(FetchedMsg)
	if msg.Err != nil {
		t.Fatalf("FetchCmd err: %v", msg.Err)
	}
	body, err := os.ReadFile(msg.TempPath)
	if err != nil {
		t.Fatalf("read tempfile: %v", err)
	}
	if strings.Contains(string(body), FenceMarker) {
		t.Fatalf("tempfile should not contain fence marker when catalog is nil:\n%s", string(body))
	}
}
