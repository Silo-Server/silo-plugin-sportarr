package main

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"google.golang.org/protobuf/types/known/structpb"
)

// Sportarr is a specialist provider: sports leagues rendered as TV shows. It
// must not become the default #1 metadata provider on every new TV series
// library, and it must not out-rank the general providers (TVDB priority 2,
// TMDB priority 3) when a user does opt in. The host seeds it disabled when the
// capability metadata declares default_enabled=false, and orders it by its
// declared default_priority, so both properties live in the manifest.
func TestManifestOptsOutOfDefaultEnable(t *testing.T) {
	m, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}
	meta := m.Capabilities[0].GetMetadata().AsMap()

	enabled, ok := meta["default_enabled"].(bool)
	if !ok {
		t.Fatalf("expected default_enabled bool in capability metadata, got %T (%v)", meta["default_enabled"], meta["default_enabled"])
	}
	if enabled {
		t.Errorf("sportarr must declare default_enabled=false so it is seeded disabled")
	}

	dp, ok := meta["default_priority"].(map[string]any)
	if !ok {
		t.Fatalf("expected default_priority map, got %T", meta["default_priority"])
	}
	// Series metadata remains below TVDB(2)/TMDB(3).
	for _, level := range []string{"series", "season", "episode"} {
		p, ok := dp[level].(float64)
		if !ok {
			t.Fatalf("expected numeric priority for %q, got %T", level, dp[level])
		}
		if p != 50 {
			t.Errorf("sportarr %s priority = %v, want 50", level, p)
		}
	}
	if _, ok := dp["movie"]; ok {
		t.Fatal("sportarr must not advertise Movie support before Sportarr publishes that API")
	}

	presentation := m.GetPresentation()
	if presentation == nil {
		t.Fatal("expected presentation metadata")
	}
	if got := presentation.GetSetupMarkdown(); got == "" || !containsAll(got, "API key", "protected image API") {
		t.Errorf("setup must explain when the Sportarr API key is needed, got %q", got)
	}
}

func TestManifestLoads(t *testing.T) {
	m, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		t.Fatalf("failed to load manifest: %v", err)
	}
	if m.PluginId != "silo.sportarr" {
		t.Errorf("expected plugin_id silo.sportarr, got %s", m.PluginId)
	}
	if len(m.Capabilities) != 1 {
		t.Fatalf("expected 1 capability, got %d", len(m.Capabilities))
	}
	if m.Capabilities[0].Id != "sportarr" {
		t.Errorf("expected capability id sportarr, got %s", m.Capabilities[0].Id)
	}
	if m.Capabilities[0].Type != "metadata_provider.v1" {
		t.Errorf("expected capability type metadata_provider.v1, got %s", m.Capabilities[0].Type)
	}
	if got := m.GetPresentation().GetSourceUrl(); got != "https://github.com/Silo-Server/silo-plugin-metadata-sportarr" {
		t.Errorf("source URL = %q, want canonical Silo repository", got)
	}
}

func TestManifestMarksAPIKeySecret(t *testing.T) {
	var raw struct {
		GlobalConfigSchema []struct {
			JSONSchema string `json:"json_schema"`
			AdminForm  struct {
				Fields []struct {
					Key    string `json:"key"`
					Secret bool   `json:"secret"`
				} `json:"fields"`
			} `json:"admin_form"`
		} `json:"global_config_schema"`
	}
	if err := json.Unmarshal(manifestJSON, &raw); err != nil {
		t.Fatalf("decode manifest JSON: %v", err)
	}
	if len(raw.GlobalConfigSchema) != 1 || !strings.Contains(raw.GlobalConfigSchema[0].JSONSchema, `"api_key"`) {
		t.Fatal("Sportarr config schema must include api_key")
	}
	for _, field := range raw.GlobalConfigSchema[0].AdminForm.Fields {
		if field.Key == "api_key" {
			if !field.Secret {
				t.Fatal("Sportarr api_key must be marked secret")
			}
			return
		}
	}
	t.Fatal("Sportarr admin form is missing api_key")
}

func TestConfigureMarksExplicitBaseURL(t *testing.T) {
	configured, err := structpb.NewStruct(map[string]any{"base_url": "http://sportarr:1867/"})
	if err != nil {
		t.Fatalf("make configured base URL: %v", err)
	}

	runtime := &runtimeServer{}
	if _, err := runtime.Configure(context.Background(), &pluginv1.ConfigureRequest{Config: []*pluginv1.ConfigEntry{{Key: "sportarr", Value: configured}}}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	if !runtime.baseURLConfigured {
		t.Fatal("explicit Sportarr base URL was not recorded")
	}
	if got, want := runtime.baseURL, "http://sportarr:1867"; got != want {
		t.Fatalf("configured base URL = %q, want %q", got, want)
	}
}

func TestSportarrCanonicalPath(t *testing.T) {
	tests := []struct {
		name     string
		baseURL  string
		imageURL string
		want     string
	}{
		{"empty", "https://sportarr.net", "", ""},
		{"full url", "https://sportarr.net", "https://sportarr.net/api/images/abc123", "sportarr:///api/images/abc123"},
		{"same effective default port", "http://sportarr.local", "http://sportarr.local:80/api/images/abc123", "sportarr:///api/images/abc123"},
		{"configured local URL", "http://sportarr.local:1867", "http://sportarr.local:1867/api/images/abc123", "sportarr:///api/images/abc123"},
		{"relative image path", "http://sportarr.local:1867", "/api/v1/images/image-1", "sportarr:///api/v1/images/image-1"},
		{"scheme-relative host stays external", "http://sportarr.local:1867", "//example.com/api/images/abc123", "//example.com/api/images/abc123"},
		{"near-prefix port stays external", "http://sportarr.local:1867", "http://sportarr.local:18670/api/images/abc123", "http://sportarr.local:18670/api/images/abc123"},
		{"base path boundary", "http://sportarr.local:1867/sportarr", "http://sportarr.local:1867/sportarr/images/abc123", "sportarr:///images/abc123"},
		{"near-prefix base path stays external", "http://sportarr.local:1867/sportarr", "http://sportarr.local:1867/sportarrx/images/abc123", "http://sportarr.local:1867/sportarrx/images/abc123"},
		{"external url", "https://sportarr.net", "https://example.com/image.jpg", "https://example.com/image.jpg"},
		{"different scheme stays external", "https://sportarr.local", "http://sportarr.local/api/images/abc123", "http://sportarr.local/api/images/abc123"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sportarrCanonicalPath(tt.baseURL, tt.imageURL)
			if got != tt.want {
				t.Errorf("sportarrCanonicalPath(%q, %q) = %q, want %q", tt.baseURL, tt.imageURL, got, tt.want)
			}
		})
	}
}

func TestResolveOneSportarrPath(t *testing.T) {
	base := "https://sportarr.net"

	tests := []struct {
		name string
		path string
		want string
	}{
		{"empty", "", ""},
		{"canonical", "sportarr:///api/images/abc123", "https://sportarr.net/api/images/abc123"},
		// Silo strips the plugin scheme before calling ResolveImageURL. The
		// resolver must therefore accept the bare path it receives over RPC.
		{"bare path from Silo", "/api/images/abc123", "https://sportarr.net/api/images/abc123"},
		{"full url passthrough", "https://example.com/image.jpg", "https://example.com/image.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := resolveOneSportarrPath(base, tt.path, "")
			if got != tt.want {
				t.Errorf("resolveOneSportarrPath(%q) = %q, want %q", tt.path, got, tt.want)
			}
		})
	}
}

func TestDefaultHubImageRPCResolvesBarePath(t *testing.T) {
	server := &metadataServer{runtime: &runtimeServer{baseURL: defaultBaseURL}}
	response, err := server.ResolveImageURL(context.Background(), &pluginv1.ResolveImageURLRequest{
		Path: "/api/images/league/formula-1/poster",
	})
	if err != nil {
		t.Fatalf("resolve default-hub image: %v", err)
	}
	if got, want := response.GetUrl(), defaultBaseURL+"/api/images/league/formula-1/poster"; got != want {
		t.Fatalf("resolved default-hub URL = %q, want %q", got, want)
	}
}

func TestConfiguredImageRPCAuthenticatesAndPreservesBasePath(t *testing.T) {
	const apiKey = "sportarr-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/sportarr/api/v1/images/image-1"; got != want {
			t.Errorf("image redirect path = %q, want %q", got, want)
		}
		if got := r.Header.Get("X-Api-Key"); got != apiKey {
			t.Errorf("X-Api-Key = %q, want configured key", got)
		}
		w.Header().Set("Location", "https://8.8.8.8/formula-1.jpg")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	configured, err := structpb.NewStruct(map[string]any{
		"base_url": srv.URL + "/sportarr/",
		"api_key":  "  " + apiKey + "  ",
	})
	if err != nil {
		t.Fatalf("make Sportarr config: %v", err)
	}
	runtime := &runtimeServer{}
	if _, err := runtime.Configure(context.Background(), &pluginv1.ConfigureRequest{Config: []*pluginv1.ConfigEntry{{Key: "sportarr", Value: configured}}}); err != nil {
		t.Fatalf("configure: %v", err)
	}
	server := &metadataServer{runtime: runtime}
	response, err := server.ResolveImageURL(context.Background(), &pluginv1.ResolveImageURLRequest{
		// Silo strips sportarr:// before invoking the plugin resolver.
		Path: "/api/v1/images/image-1",
	})
	if err != nil {
		t.Fatalf("resolve configured image: %v", err)
	}
	if got, want := response.GetUrl(), "https://8.8.8.8/formula-1.jpg"; got != want {
		t.Fatalf("resolved configured URL = %q, want %q", got, want)
	}
}

func containsAll(value string, needles ...string) bool {
	for _, needle := range needles {
		if !strings.Contains(value, needle) {
			return false
		}
	}
	return true
}
