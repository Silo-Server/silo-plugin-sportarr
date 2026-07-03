package main

import (
	"testing"

	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
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
	// Must still declare the series levels (keeps it scoped to series content
	// and out of the movie/audiobook fallback), but ranked below TVDB(2)/TMDB(3).
	for _, level := range []string{"series", "season", "episode"} {
		p, ok := dp[level].(float64)
		if !ok {
			t.Fatalf("expected numeric priority for %q, got %T", level, dp[level])
		}
		if p <= 3 {
			t.Errorf("sportarr %s priority %v must be greater than TMDB(3) so it never out-ranks the general providers", level, p)
		}
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
}

func TestSportarrCanonicalPath(t *testing.T) {
	base := "https://sportarr.net"

	tests := []struct {
		name     string
		imageURL string
		want     string
	}{
		{"empty", "", ""},
		{"full url", "https://sportarr.net/api/images/abc123", "sportarr:///api/images/abc123"},
		{"external url", "https://example.com/image.jpg", "https://example.com/image.jpg"},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := sportarrCanonicalPath(base, tt.imageURL)
			if got != tt.want {
				t.Errorf("sportarrCanonicalPath(%q) = %q, want %q", tt.imageURL, got, tt.want)
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
