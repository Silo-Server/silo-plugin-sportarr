package provider

import (
	"context"
	"encoding/json"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestSearchParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metadata/agents/search" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if r.URL.Query().Get("title") != "NFL" {
			t.Errorf("unexpected title param: %s", r.URL.Query().Get("title"))
		}
		if err := json.NewEncoder(w).Encode(AgentSearchResponse{
			Results: []AgentSearchResult{
				{ID: "lg-000123", HubID: "abc-uuid", Title: "NFL Football", Year: 2024, PosterURL: "https://sportarr.net/static/images/nfl.jpg"},
			},
		}); err != nil {
			t.Errorf("encode series search response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(10)
	c.SetBaseURL(srv.URL)

	resp, err := c.Search(context.Background(), "NFL")
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(resp.Results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(resp.Results))
	}
	if resp.Results[0].HubID != "abc-uuid" {
		t.Errorf("expected hub_id abc-uuid, got %s", resp.Results[0].HubID)
	}
	if resp.Results[0].Title != "NFL Football" {
		t.Errorf("expected title NFL Football, got %s", resp.Results[0].Title)
	}
}

func TestGetSeriesParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metadata/agents/series/abc-123" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(AgentSeriesResponse{
			Title:     "NFL Football",
			Summary:   "American football league",
			Year:      1920,
			Genres:    []string{"American Football", "Sports"},
			Studio:    "NFL",
			PosterURL: "https://sportarr.net/img/nfl-poster.jpg",
		}); err != nil {
			t.Errorf("encode series response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(10)
	c.SetBaseURL(srv.URL)

	resp, err := c.GetSeries(context.Background(), "abc-123")
	if err != nil {
		t.Fatalf("get series failed: %v", err)
	}
	if resp.Title != "NFL Football" {
		t.Errorf("expected title NFL Football, got %s", resp.Title)
	}
	if len(resp.Genres) != 2 {
		t.Errorf("expected 2 genres, got %d", len(resp.Genres))
	}
}

func TestGetSeasonsParsesReleasedAgentArtworkFields(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metadata/agents/series/formula-1/seasons" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		_, _ = io.WriteString(w, `{"seasons":[{"season_number":2026,"title":"2026","poster_url":"https://images.example/f1-2026.jpg","episode_count":24}]}`)
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL)
	resp, err := c.GetSeasons(context.Background(), "formula-1")
	if err != nil {
		t.Fatalf("get seasons: %v", err)
	}
	if len(resp.Seasons) != 1 || resp.Seasons[0].Title != "2026" || resp.Seasons[0].PosterURL != "https://images.example/f1-2026.jpg" {
		t.Fatalf("unexpected released season response: %+v", resp.Seasons)
	}
}

func TestGetSeasonEpisodesParsesResponse(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metadata/agents/series/abc-123/season/2024/episodes" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if err := json.NewEncoder(w).Encode(AgentEpisodesResponse{
			Episodes: []AgentEpisode{
				{
					ID:              "evt-001",
					Title:           "Super Bowl LVIII",
					SeasonNumber:    2024,
					EpisodeNumber:   1,
					AirDate:         "2024-02-11",
					DurationMinutes: 240,
				},
			},
		}); err != nil {
			t.Errorf("encode episodes response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(10)
	c.SetBaseURL(srv.URL)

	resp, err := c.GetSeasonEpisodes(context.Background(), "abc-123", 2024)
	if err != nil {
		t.Fatalf("get episodes failed: %v", err)
	}
	if len(resp.Episodes) != 1 {
		t.Fatalf("expected 1 episode, got %d", len(resp.Episodes))
	}
	if resp.Episodes[0].Title != "Super Bowl LVIII" {
		t.Errorf("expected title Super Bowl LVIII, got %s", resp.Episodes[0].Title)
	}
	if resp.Episodes[0].DurationMinutes != 240 {
		t.Errorf("expected 240 min duration, got %d", resp.Episodes[0].DurationMinutes)
	}
}

func TestRetryOn5xx(t *testing.T) {
	attempts := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		attempts++
		if attempts <= 2 {
			w.WriteHeader(http.StatusInternalServerError)
			return
		}
		if err := json.NewEncoder(w).Encode(AgentSearchResponse{
			Results: []AgentSearchResult{{ID: "ok", Title: "OK"}},
		}); err != nil {
			t.Errorf("encode retry response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL)

	resp, err := c.Search(context.Background(), "test")
	if err != nil {
		t.Fatalf("expected success after retries, got: %v", err)
	}
	if len(resp.Results) != 1 || resp.Results[0].ID != "ok" {
		t.Errorf("unexpected result after retry")
	}
	if attempts != 3 {
		t.Errorf("expected 3 attempts, got %d", attempts)
	}
}

func TestNoCacheHeaders(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Header.Get("Cache-Control") != "no-cache, no-store" {
			t.Errorf("missing Cache-Control header")
		}
		if r.Header.Get("Pragma") != "no-cache" {
			t.Errorf("missing Pragma header")
		}
		if err := json.NewEncoder(w).Encode(AgentSearchResponse{}); err != nil {
			t.Errorf("encode no-cache response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(10)
	c.SetBaseURL(srv.URL)
	if _, err := c.Search(context.Background(), "test"); err != nil {
		t.Fatalf("search failed: %v", err)
	}
}

func TestGetEntityImagesParsesResponse(t *testing.T) {
	const apiKey = "sportarr-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/sportarr/api/v1/images/entity/league/abc-uuid" {
			t.Errorf("unexpected path: %s", r.URL.Path)
		}
		if got := r.Header.Get(apiKeyHeader); got != apiKey {
			t.Errorf("X-Api-Key = %q, want configured key", got)
		}
		if r.URL.Query().Get("completed_only") != "true" {
			t.Errorf("expected completed_only=true, got %s", r.URL.Query().Get("completed_only"))
		}
		if err := json.NewEncoder(w).Encode(EntityImageResponse{
			Images: []EntityImage{
				{
					ID:        "img-1",
					ImageType: "poster",
					URL:       "/static/images/league/img-1.jpg",
					IsPrimary: true,
					Priority:  10,
				},
				{
					ID:        "img-2",
					ImageType: "backdrop",
					URL:       "/static/images/league/img-2.jpg",
					Priority:  5,
				},
			},
		}); err != nil {
			t.Errorf("encode entity images response: %v", err)
		}
	}))
	defer srv.Close()

	c := NewClient(10)
	c.SetBaseURL(srv.URL + "/sportarr/")
	c.SetAPIKey("  " + apiKey + "  ")

	resp, err := c.GetEntityImages(context.Background(), "league", "abc-uuid")
	if err != nil {
		t.Fatalf("get entity images failed: %v", err)
	}
	if len(resp.Images) != 2 {
		t.Fatalf("expected 2 images, got %d", len(resp.Images))
	}
	if resp.Images[0].ID != "img-1" {
		t.Errorf("expected ID img-1, got %s", resp.Images[0].ID)
	}
	if resp.Images[0].ImageType != "poster" {
		t.Errorf("expected image_type poster, got %s", resp.Images[0].ImageType)
	}
	if !resp.Images[0].IsPrimary {
		t.Errorf("expected is_primary=true")
	}
	if resp.Images[1].URL != "/static/images/league/img-2.jpg" {
		t.Errorf("unexpected URL: %s", resp.Images[1].URL)
	}
}

func TestGetEntityImagesBatch(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/images/entity/season/s1":
			if err := json.NewEncoder(w).Encode(EntityImageResponse{
				Images: []EntityImage{{ID: "img-s1", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/img-s1"}},
			}); err != nil {
				t.Errorf("encode season s1 images response: %v", err)
			}
		case "/api/v1/images/entity/season/s2":
			if err := json.NewEncoder(w).Encode(EntityImageResponse{
				Images: []EntityImage{{ID: "img-s2", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/img-s2"}},
			}); err != nil {
				t.Errorf("encode season s2 images response: %v", err)
			}
		default:
			w.WriteHeader(404)
		}
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL)

	result := c.GetEntityImagesBatch(context.Background(), "season", []string{"s1", "s2", "s1"})
	if len(result) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(result))
	}
	if result["s1"].Images[0].ID != "img-s1" {
		t.Errorf("expected img-s1, got %s", result["s1"].Images[0].ID)
	}
	if result["s2"].Images[0].ID != "img-s2" {
		t.Errorf("expected img-s2, got %s", result["s2"].Images[0].ID)
	}
}

func TestGetEntityImagesBatchPartialFailure(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/images/entity/season/s1":
			if err := json.NewEncoder(w).Encode(EntityImageResponse{
				Images: []EntityImage{{ID: "img-s1", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/img-s1"}},
			}); err != nil {
				t.Errorf("encode partial batch images response: %v", err)
			}
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL)

	result := c.GetEntityImagesBatch(context.Background(), "season", []string{"s1", "bad-id"})
	if len(result) != 1 {
		t.Fatalf("expected 1 entry (bad-id should be skipped), got %d", len(result))
	}
	if _, ok := result["s1"]; !ok {
		t.Error("expected s1 in results")
	}
}

func TestGetEntityImagesBatchEmpty(t *testing.T) {
	c := NewClient(100)
	result := c.GetEntityImagesBatch(context.Background(), "season", nil)
	if len(result) != 0 {
		t.Errorf("expected empty map, got %d entries", len(result))
	}
}

func TestJSONRequestDoesNotFollowRedirectWithAPIKey(t *testing.T) {
	targetRequests := 0
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		targetRequests++
		w.WriteHeader(http.StatusNoContent)
	}))
	defer target.Close()

	source := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Redirect(w, r, target.URL, http.StatusFound)
	}))
	defer source.Close()

	c := NewClient(100)
	c.SetBaseURL(source.URL)
	c.SetAPIKey("sportarr-secret")
	_, err := c.Search(context.Background(), "Formula 1")
	if err == nil || !strings.Contains(err.Error(), "unexpected HTTP 302") {
		t.Fatalf("redirected JSON request error = %v, want HTTP 302 rejection", err)
	}
	if targetRequests != 0 {
		t.Fatalf("redirect target received %d requests; API key could have leaked", targetRequests)
	}
}

func TestRequestURLRejectsTraversal(t *testing.T) {
	c := NewClient(100)
	c.SetBaseURL("http://sportarr.local/sportarr")
	for _, path := range []string{
		"/api/../admin",
		"/api/%2e%2e/admin",
		"/api/v1/%2E%2E/admin",
		`/api/v1\..\admin`,
	} {
		t.Run(path, func(t *testing.T) {
			if _, err := c.requestURL(path); err == nil {
				t.Fatalf("requestURL(%q) accepted traversal", path)
			}
		})
	}
}

func TestResolveImageRedirectAuthenticatesAndPreservesBasePath(t *testing.T) {
	const apiKey = "sportarr-secret"
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got, want := r.URL.Path, "/sportarr/api/v1/images/image-1"; got != want {
			t.Errorf("redirect request path = %q, want %q", got, want)
		}
		if got := r.Header.Get(apiKeyHeader); got != apiKey {
			t.Errorf("X-Api-Key = %q, want configured key", got)
		}
		w.Header().Set("Location", "https://cdn.example/formula-1.jpg")
		w.WriteHeader(http.StatusTemporaryRedirect)
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL + "/sportarr/")
	c.SetAPIKey(apiKey)
	c.lookupIP = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}

	got, err := c.ResolveImageRedirect(context.Background(), "/api/v1/images/image-1")
	if err != nil {
		t.Fatalf("resolve image redirect: %v", err)
	}
	if want := "https://cdn.example/formula-1.jpg"; got != want {
		t.Fatalf("redirect target = %q, want %q", got, want)
	}
}

func TestResolveImageRedirectPreservesRequestFragment(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Fragment != "" || strings.Contains(r.RequestURI, "#") {
			t.Errorf("redirect request included fragment: %q", r.RequestURI)
		}
		w.Header().Set("Location", "https://cdn.example/formula-1.svg")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL)
	c.lookupIP = func(context.Context, string) ([]net.IP, error) {
		return []net.IP{net.ParseIP("8.8.8.8")}, nil
	}

	got, err := c.ResolveImageRedirect(context.Background(), "/api/v1/images/image-1#logo-layer")
	if err != nil {
		t.Fatalf("resolve image redirect with fragment: %v", err)
	}
	if want := "https://cdn.example/formula-1.svg#logo-layer"; got != want {
		t.Fatalf("redirect target = %q, want %q", got, want)
	}
}

func TestResolveImageRedirectRejectsPrivateTarget(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Location", "https://127.0.0.1/formula-1.jpg")
		w.WriteHeader(http.StatusFound)
	}))
	defer srv.Close()

	c := NewClient(100)
	c.SetBaseURL(srv.URL)
	_, err := c.ResolveImageRedirect(context.Background(), "/api/images/league/formula-1/poster")
	if err == nil || !strings.Contains(err.Error(), "not globally routable") {
		t.Fatalf("private redirect error = %v, want globally-routable rejection", err)
	}
}
