package provider

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/Silo-Server/silo-plugin-sportarr/metadata"
)

func encodeJSON(t *testing.T, w http.ResponseWriter, value any) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(value); err != nil {
		t.Errorf("encode response: %v", err)
	}
}

func newTestProvider(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(100)
	c.SetBaseURL(srv.URL)
	c.SetAPIKey("test-sportarr-key")
	return NewProviderWithClient(c)
}

func newTestProviderWithoutAPIKey(t *testing.T, handler http.Handler) *Provider {
	t.Helper()
	srv := httptest.NewServer(handler)
	t.Cleanup(srv.Close)
	c := NewClient(100)
	c.SetBaseURL(srv.URL)
	return NewProviderWithClient(c)
}

func TestProviderOnlyAdvertisesReleasedSeriesContract(t *testing.T) {
	p := NewProvider("")
	if got := p.ForTypes(); len(got) != 1 || got[0] != "series" {
		t.Fatalf("ForTypes() = %v, want only series", got)
	}
}

func TestSearchByTitle(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encodeJSON(t, w, AgentSearchResponse{
			Results: []AgentSearchResult{
				{ID: "league-1", Title: "Premier League", Year: 1992},
				{ID: "league-2", Title: "Premier League 2", Year: 2023},
			},
		})
	}))

	results, err := p.Search(context.Background(), metadata.SearchQuery{Title: "Premier League"})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 2 {
		t.Fatalf("expected 2 results, got %d", len(results))
	}
	if results[0].ProviderIDs["sportarr"] != "league-1" {
		t.Errorf("expected sportarr ID league-1, got %s", results[0].ProviderIDs["sportarr"])
	}
}

func TestSearchByProviderID(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metadata/agents/series/league-1" {
			t.Errorf("expected series lookup, got %s", r.URL.Path)
		}
		encodeJSON(t, w, AgentSeriesResponse{
			Title: "UFC", Year: 1993, Summary: "MMA league",
		})
	}))

	results, err := p.Search(context.Background(), metadata.SearchQuery{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
	})
	if err != nil {
		t.Fatalf("search failed: %v", err)
	}
	if len(results) != 1 {
		t.Fatalf("expected 1 result, got %d", len(results))
	}
	if results[0].Name != "UFC" {
		t.Errorf("expected name UFC, got %s", results[0].Name)
	}
}

func TestGetMetadata(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/metadata/agents/series/league-1":
			encodeJSON(t, w, AgentSeriesResponse{
				Title:   "Formula 1",
				Summary: "Open-wheel racing",
				Year:    1950,
				Genres:  []string{"Motorsport"},
				Studio:  "FIA",
			})
		case "/api/metadata/agents/series/league-1/seasons":
			encodeJSON(t, w, AgentSeasonsResponse{
				Seasons: []AgentSeason{
					{SeasonNumber: 2023, Name: "2023"},
					{SeasonNumber: 2024, Name: "2024"},
				},
			})
		case "/api/v1/images/entity/league/league-1":
			encodeJSON(t, w, EntityImageResponse{
				Images: []EntityImage{
					{ID: "p1", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/p1", IsPrimary: true},
					{ID: "b1", ImageType: "backdrop", URL: "https://sportarr.net/api/v1/images/b1", IsPrimary: true},
					{ID: "l1", ImageType: "logo", URL: "https://sportarr.net/api/v1/images/l1"},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("get metadata failed: %v", err)
	}
	if !result.HasMetadata {
		t.Fatal("expected HasMetadata=true")
	}
	if result.Title != "Formula 1" {
		t.Errorf("expected title Formula 1, got %s", result.Title)
	}
	if result.SeasonCount != 2 {
		t.Errorf("expected 2 seasons, got %d", result.SeasonCount)
	}
	if len(result.Genres) != 1 || result.Genres[0] != "Motorsport" {
		t.Errorf("unexpected genres: %v", result.Genres)
	}
	if result.PosterPath != "https://sportarr.net/api/v1/images/p1" {
		t.Errorf("expected poster path from entity images, got %s", result.PosterPath)
	}
	if result.BackdropPath != "https://sportarr.net/api/v1/images/b1" {
		t.Errorf("expected backdrop path from entity images, got %s", result.BackdropPath)
	}
	if result.LogoPath != "https://sportarr.net/api/v1/images/l1" {
		t.Errorf("expected logo path from entity images, got %s", result.LogoPath)
	}
}

func TestGetMetadataUsesPublicAgentArtworkWithoutAPIKey(t *testing.T) {
	p := newTestProviderWithoutAPIKey(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/metadata/agents/series/league-1":
			encodeJSON(t, w, AgentSeriesResponse{
				Title:     "Formula 1",
				PosterURL: "https://images.example/formula-1-poster.jpg",
				FanartURL: "https://images.example/formula-1-fanart.jpg",
			})
		case "/api/metadata/agents/series/league-1/seasons":
			encodeJSON(t, w, AgentSeasonsResponse{})
		default:
			t.Errorf("unexpected request without an API key: %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
		}
	}))

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("get metadata without API key: %v", err)
	}
	if got, want := result.PosterPath, "https://images.example/formula-1-poster.jpg"; got != want {
		t.Fatalf("poster path = %q, want public agent artwork %q", got, want)
	}
	if got, want := result.BackdropPath, "https://images.example/formula-1-fanart.jpg"; got != want {
		t.Fatalf("backdrop path = %q, want public agent artwork %q", got, want)
	}
}

func TestGetSeasons(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/metadata/agents/series/league-1/seasons":
			encodeJSON(t, w, AgentSeasonsResponse{
				Seasons: []AgentSeason{
					{CompetitionSeasonID: "cs-2023", SeasonNumber: 2023, Name: "2023 Season", EpisodeCount: 23},
					{CompetitionSeasonID: "cs-2024", SeasonNumber: 2024, Name: "2024 Season", EpisodeCount: 24},
				},
			})
		case "/api/v1/images/entity/season/cs-2023":
			encodeJSON(t, w, EntityImageResponse{
				Images: []EntityImage{
					{ID: "sp1", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/sp1", IsPrimary: true},
				},
			})
		case "/api/v1/images/entity/season/cs-2024":
			encodeJSON(t, w, EntityImageResponse{
				Images: []EntityImage{
					{ID: "sp2", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/sp2"},
				},
			})
		default:
			w.WriteHeader(404)
		}
	}))

	seasons, err := p.GetSeasons(context.Background(), metadata.SeasonsRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
	})
	if err != nil {
		t.Fatalf("get seasons failed: %v", err)
	}
	if len(seasons) != 2 {
		t.Fatalf("expected 2 seasons, got %d", len(seasons))
	}
	if seasons[0].SeasonNumber != 2023 {
		t.Errorf("expected season 2023, got %d", seasons[0].SeasonNumber)
	}
	if seasons[1].Title != "2024 Season" {
		t.Errorf("expected title 2024 Season, got %s", seasons[1].Title)
	}
	if seasons[0].PosterPath != "https://sportarr.net/api/v1/images/sp1" {
		t.Errorf("expected poster from entity images, got %s", seasons[0].PosterPath)
	}
	if seasons[1].PosterPath != "https://sportarr.net/api/v1/images/sp2" {
		t.Errorf("expected poster from entity images, got %s", seasons[1].PosterPath)
	}
}

func TestGetSeasonsWithoutCompetitionSeasonID(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/metadata/agents/series/league-1/seasons":
			encodeJSON(t, w, AgentSeasonsResponse{
				Seasons: []AgentSeason{
					{SeasonNumber: 2023, Title: "2023 Season", PosterURL: "https://images.example/2023.jpg"},
				},
			})
		default:
			t.Errorf("unexpected request to %s (should not call entity image API)", r.URL.Path)
			w.WriteHeader(404)
		}
	}))

	seasons, err := p.GetSeasons(context.Background(), metadata.SeasonsRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
	})
	if err != nil {
		t.Fatalf("get seasons failed: %v", err)
	}
	if len(seasons) != 1 {
		t.Fatalf("expected 1 season, got %d", len(seasons))
	}
	if seasons[0].Title != "2023 Season" {
		t.Errorf("expected released agent title field, got %q", seasons[0].Title)
	}
	if seasons[0].PosterPath != "https://images.example/2023.jpg" {
		t.Errorf("expected public agent poster without CompetitionSeasonID, got %s", seasons[0].PosterPath)
	}
}

func TestGetEpisodes(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		encodeJSON(t, w, AgentEpisodesResponse{
			Episodes: []AgentEpisode{
				{ID: "ev-1", Title: "Monaco GP", SeasonNumber: 2024, EpisodeNumber: 8, AirDate: "2024-05-26", DurationMinutes: 120},
				{ID: "ev-2", Title: "Canadian GP", SeasonNumber: 2024, EpisodeNumber: 9, AirDate: "2024-06-09", DurationMinutes: 120},
			},
		})
	}))

	episodes, err := p.GetEpisodes(context.Background(), metadata.EpisodesRequest{
		ProviderIDs:  map[string]string{"sportarr": "league-1"},
		SeasonNumber: 2024,
	})
	if err != nil {
		t.Fatalf("get episodes failed: %v", err)
	}
	if len(episodes) != 2 {
		t.Fatalf("expected 2 episodes, got %d", len(episodes))
	}
	if episodes[0].Title != "Monaco GP" {
		t.Errorf("expected Monaco GP, got %s", episodes[0].Title)
	}
	if episodes[0].ProviderIDs["sportarr"] != "ev-1" {
		t.Errorf("expected sportarr ID ev-1, got %s", episodes[0].ProviderIDs["sportarr"])
	}
}

func TestGetImagesForSeries(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/entity/league/league-1" {
			t.Errorf("expected entity image path, got %s", r.URL.Path)
		}
		w1, h1 := 680, 1000
		w2, h2 := 1920, 1080
		encodeJSON(t, w, EntityImageResponse{
			Images: []EntityImage{
				{ID: "img-1", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/img-1", IsPrimary: true, Width: &w1, Height: &h1},
				{ID: "img-2", ImageType: "backdrop", URL: "https://sportarr.net/api/v1/images/img-2", Width: &w2, Height: &h2},
				{ID: "img-3", ImageType: "logo", URL: "https://sportarr.net/api/v1/images/img-3"},
				{ID: "img-4", ImageType: "banner", URL: "https://sportarr.net/api/v1/images/img-4"},
			},
		})
	}))

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("get images failed: %v", err)
	}
	if len(images) != 4 {
		t.Fatalf("expected 4 images, got %d", len(images))
	}
	// Primary poster should sort first
	if images[0].Type != metadata.ImagePoster {
		t.Errorf("expected poster first (is_primary), got type %d", images[0].Type)
	}
	if images[0].Width != 680 || images[0].Height != 1000 {
		t.Errorf("expected 680x1000, got %dx%d", images[0].Width, images[0].Height)
	}
}

func TestGetImagesUsesPublicSeriesArtworkWithoutAPIKey(t *testing.T) {
	p := newTestProviderWithoutAPIKey(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/metadata/agents/series/league-1" {
			t.Errorf("unexpected request without API key: %s", r.URL.Path)
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		encodeJSON(t, w, AgentSeriesResponse{
			PosterURL: "https://images.example/formula-1-poster.jpg",
			BannerURL: "https://images.example/formula-1-banner.jpg",
			FanartURL: "https://images.example/formula-1-fanart.jpg",
		})
	}))

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("get public series images: %v", err)
	}
	if len(images) != 3 || images[0].Type != metadata.ImagePoster || images[1].Type != metadata.ImageBackdrop || images[2].Type != metadata.ImageBanner {
		t.Fatalf("unexpected public series artwork: %+v", images)
	}
}

func TestGetImagesFallsBackToPublicSeriesArtworkAfterUnauthorizedEntityAPI(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.URL.Path {
		case "/api/v1/images/entity/league/league-1":
			w.WriteHeader(http.StatusUnauthorized)
		case "/api/metadata/agents/series/league-1":
			encodeJSON(t, w, AgentSeriesResponse{PosterURL: "https://images.example/formula-1-poster.jpg"})
		default:
			t.Errorf("unexpected fallback request: %s", r.URL.Path)
			w.WriteHeader(http.StatusNotFound)
		}
	}))

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"sportarr": "league-1"},
		ContentType: "series",
	})
	if err != nil {
		t.Fatalf("get series images after entity API 401: %v", err)
	}
	if len(images) != 1 || images[0].URL != "https://images.example/formula-1-poster.jpg" {
		t.Fatalf("unexpected fallback artwork: %+v", images)
	}
}

func TestGetImagesForEpisode(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/entity/event/ev-1" {
			t.Errorf("expected entity image path for event, got %s", r.URL.Path)
		}
		encodeJSON(t, w, EntityImageResponse{
			Images: []EntityImage{
				{ID: "img-t1", ImageType: "thumbnail", URL: "https://sportarr.net/api/v1/images/img-t1"},
			},
		})
	}))

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"sportarr": "ev-1"},
		ContentType: "episode",
	})
	if err != nil {
		t.Fatalf("get images failed: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].Type != metadata.ImageStill {
		t.Errorf("expected still type, got %d", images[0].Type)
	}
}

func TestGetImagesForSeason(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/api/v1/images/entity/season/season-uuid-1" {
			t.Errorf("expected entity image path for season, got %s", r.URL.Path)
		}
		encodeJSON(t, w, EntityImageResponse{
			Images: []EntityImage{
				{ID: "img-sp1", ImageType: "poster", URL: "https://sportarr.net/api/v1/images/img-sp1", IsPrimary: true},
			},
		})
	}))

	images, err := p.GetImages(context.Background(), metadata.ImageRequest{
		ProviderIDs: map[string]string{"sportarr": "season-uuid-1"},
		ContentType: "season",
	})
	if err != nil {
		t.Fatalf("get images failed: %v", err)
	}
	if len(images) != 1 {
		t.Fatalf("expected 1 image, got %d", len(images))
	}
	if images[0].Type != metadata.ImagePoster {
		t.Errorf("expected poster type, got %d", images[0].Type)
	}
}

func TestSearchEmptyQuery(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make any HTTP request for empty query")
	}))

	results, err := p.Search(context.Background(), metadata.SearchQuery{})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if results != nil {
		t.Errorf("expected nil results, got %v", results)
	}
}

func TestGetMetadataNoProviderID(t *testing.T) {
	p := newTestProvider(t, http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		t.Fatal("should not make any HTTP request without provider ID")
	}))

	result, err := p.GetMetadata(context.Background(), metadata.MetadataRequest{
		ProviderIDs: map[string]string{},
	})
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if result != nil {
		t.Errorf("expected nil result, got %v", result)
	}
}

func TestPickPrimaryURL(t *testing.T) {
	images := []EntityImage{
		{ImageType: "poster", URL: "https://sportarr.net/api/v1/images/p1", Priority: 5},
		{ImageType: "poster", URL: "https://sportarr.net/api/v1/images/p2", IsPrimary: true, Priority: 1},
		{ImageType: "backdrop", URL: "https://sportarr.net/api/v1/images/b1", IsPrimary: true},
		{ImageType: "logo", URL: "https://sportarr.net/api/v1/images/l1"},
	}

	tests := []struct {
		name      string
		imageType string
		want      string
	}{
		{"primary poster wins over higher priority", "poster", "https://sportarr.net/api/v1/images/p2"},
		{"backdrop", "backdrop", "https://sportarr.net/api/v1/images/b1"},
		{"logo", "logo", "https://sportarr.net/api/v1/images/l1"},
		{"no match returns empty", "banner", ""},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := pickPrimaryURL(images, tt.imageType)
			if got != tt.want {
				t.Errorf("pickPrimaryURL(%q) = %q, want %q", tt.imageType, got, tt.want)
			}
		})
	}
}

func TestPickPrimaryURLPriorityTiebreak(t *testing.T) {
	images := []EntityImage{
		{ImageType: "poster", URL: "https://sportarr.net/api/v1/images/low", Priority: 1},
		{ImageType: "poster", URL: "https://sportarr.net/api/v1/images/high", Priority: 10},
	}
	got := pickPrimaryURL(images, "poster")
	if got != "https://sportarr.net/api/v1/images/high" {
		t.Errorf("expected highest priority poster, got %s", got)
	}
}

func TestPickPrimaryURLEmpty(t *testing.T) {
	got := pickPrimaryURL(nil, "poster")
	if got != "" {
		t.Errorf("expected empty for nil images, got %s", got)
	}
}

func TestEntityImagesToRemote(t *testing.T) {
	w1, h1 := 680, 1000
	images := []EntityImage{
		{ImageType: "poster", URL: "https://sportarr.net/api/v1/images/p1", Width: &w1, Height: &h1, Priority: 1},
		{ImageType: "backdrop", URL: "https://sportarr.net/api/v1/images/b1", IsPrimary: true},
		{ImageType: "logo", URL: "https://sportarr.net/api/v1/images/l1"},
		{ImageType: "banner", URL: "https://sportarr.net/api/v1/images/bn1"},
		{ImageType: "thumbnail", URL: "https://sportarr.net/api/v1/images/t1"},
		{ImageType: "headshot", URL: "https://sportarr.net/api/v1/images/skip"},
	}

	result := entityImagesToRemote(images)

	if len(result) != 5 {
		t.Fatalf("expected 5 images (headshot skipped), got %d", len(result))
	}

	// Primary backdrop should sort first
	if result[0].Type != metadata.ImageBackdrop {
		t.Errorf("expected backdrop first (is_primary), got type %d", result[0].Type)
	}
	if result[0].URL != "https://sportarr.net/api/v1/images/b1" {
		t.Errorf("unexpected URL: %s", result[0].URL)
	}

	// Check width/height populated
	found := false
	for _, img := range result {
		if img.Type == metadata.ImagePoster {
			found = true
			if img.Width != 680 || img.Height != 1000 {
				t.Errorf("expected 680x1000, got %dx%d", img.Width, img.Height)
			}
		}
	}
	if !found {
		t.Error("poster not found in results")
	}
}

func TestEntityImagesToRemoteEmpty(t *testing.T) {
	result := entityImagesToRemote(nil)
	if len(result) != 0 {
		t.Errorf("expected empty result, got %d", len(result))
	}
}
