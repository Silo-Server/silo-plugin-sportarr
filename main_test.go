package main

import (
	"testing"

	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
)

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
		{"relative static path", "/static/images/league/9a/poster.jpg", "sportarr:///static/images/league/9a/poster.jpg"},
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

// TestCanonicalRoundTrip ensures both absolute and root-relative image URLs the
// Sportarr API returns survive the canonicalize→store→resolve round trip and
// come back as fetchable absolute URLs.
func TestCanonicalRoundTrip(t *testing.T) {
	base := "https://sportarr.net"

	inputs := []string{
		"https://sportarr.net/static/images/league/9a/poster.jpg",
		"/static/images/league/9a/badge.png",
	}
	for _, in := range inputs {
		canonical := sportarrCanonicalPath(base, in)
		resolved := resolveOneSportarrPath(base, canonical, "")
		if resolved != "https://sportarr.net/static/images/league/9a/"+lastSeg(in) {
			t.Errorf("round trip for %q: canonical=%q resolved=%q", in, canonical, resolved)
		}
	}
}

func lastSeg(s string) string {
	for i := len(s) - 1; i >= 0; i-- {
		if s[i] == '/' {
			return s[i+1:]
		}
	}
	return s
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
