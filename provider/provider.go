package provider

import (
	"context"
	"fmt"
	"sort"

	"github.com/Silo-Server/silo-plugin-sportarr/metadata"
)

type Provider struct {
	client *Client
}

func NewProvider(baseURL string) *Provider {
	c := NewClient(10)
	if baseURL != "" {
		c.SetBaseURL(baseURL)
	}
	return &Provider{client: c}
}

func NewProviderWithClient(c *Client) *Provider {
	return &Provider{client: c}
}

func (p *Provider) Slug() string       { return "sportarr" }
func (p *Provider) Name() string       { return "Sportarr" }
func (p *Provider) ForTypes() []string { return []string{"series"} }

func mapImageType(t string) (metadata.ImageType, bool) {
	switch t {
	case "poster":
		return metadata.ImagePoster, true
	case "backdrop":
		return metadata.ImageBackdrop, true
	case "logo":
		return metadata.ImageLogo, true
	case "banner":
		return metadata.ImageBanner, true
	case "thumbnail":
		return metadata.ImageStill, true
	default:
		return 0, false
	}
}

func pickPrimaryURL(images []EntityImage, imageType string) string {
	var best *EntityImage
	for i := range images {
		img := &images[i]
		if img.ImageType != imageType {
			continue
		}
		if best == nil {
			best = img
			continue
		}
		if img.IsPrimary && !best.IsPrimary {
			best = img
		} else if img.IsPrimary == best.IsPrimary && img.Priority > best.Priority {
			best = img
		}
	}
	if best == nil {
		return ""
	}
	return best.URL
}

func entityImagesToRemote(images []EntityImage) []metadata.RemoteImage {
	sorted := make([]EntityImage, len(images))
	copy(sorted, images)
	sort.SliceStable(sorted, func(i, j int) bool {
		if sorted[i].IsPrimary != sorted[j].IsPrimary {
			return sorted[i].IsPrimary
		}
		return sorted[i].Priority > sorted[j].Priority
	})

	var out []metadata.RemoteImage
	for _, img := range sorted {
		imgType, ok := mapImageType(img.ImageType)
		if !ok {
			continue
		}
		ri := metadata.RemoteImage{
			URL:  img.URL,
			Type: imgType,
		}
		if img.Width != nil {
			ri.Width = *img.Width
		}
		if img.Height != nil {
			ri.Height = *img.Height
		}
		out = append(out, ri)
	}
	return out
}

func (p *Provider) Search(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	if sportarrID := query.ProviderIDs["sportarr"]; sportarrID != "" {
		return p.searchByID(ctx, sportarrID)
	}

	if query.Title != "" {
		return p.searchByTitle(ctx, query)
	}

	return nil, nil
}

func (p *Provider) searchByID(ctx context.Context, leagueID string) ([]metadata.SearchResult, error) {
	series, err := p.client.GetSeries(ctx, leagueID)
	if err != nil {
		return nil, err
	}
	return []metadata.SearchResult{{
		Name:        series.Title,
		Year:        series.Year,
		ProviderIDs: map[string]string{"sportarr": stableID(series.ID, leagueID)},
		ImageURL:    series.PosterURL,
		Overview:    series.Summary,
		Provider:    p.Slug(),
	}}, nil
}

// stableID prefers Sportarr's public short ID, which is the durable identifier
// accepted by the metadata-agent API. The fallback preserves existing records
// when an older response omits the explicit ID field.
func stableID(id, fallback string) string {
	if id != "" {
		return id
	}
	return fallback
}

// entityID prefers hub_id for child records because their only downstream
// identifier-based operation is the UUID-oriented entity-image lookup.
func entityID(hubID, fallback string) string {
	if hubID != "" {
		return hubID
	}
	return fallback
}

func (p *Provider) searchByTitle(ctx context.Context, query metadata.SearchQuery) ([]metadata.SearchResult, error) {
	resp, err := p.client.Search(ctx, query.Title)
	if err != nil {
		return nil, err
	}

	var out []metadata.SearchResult
	for _, r := range resp.Results {
		out = append(out, metadata.SearchResult{
			Name:        r.Title,
			Year:        r.Year,
			ProviderIDs: map[string]string{"sportarr": stableID(r.ID, r.HubID)},
			ImageURL:    r.PosterURL,
			Provider:    p.Slug(),
		})
	}
	return out, nil
}

func (p *Provider) GetMetadata(ctx context.Context, req metadata.MetadataRequest) (*metadata.MetadataResult, error) {
	sportarrID := req.ProviderIDs["sportarr"]
	if sportarrID == "" {
		return nil, nil
	}

	series, err := p.client.GetSeries(ctx, sportarrID)
	if err != nil {
		return nil, err
	}

	leagueID := stableID(series.ID, sportarrID)

	result := &metadata.MetadataResult{
		HasMetadata:   true,
		Title:         series.Title,
		SortTitle:     series.SortTitle,
		Overview:      series.Summary,
		Year:          series.Year,
		ContentRating: series.ContentRating,
		ProviderIDs:   map[string]string{"sportarr": leagueID},
	}

	result.Genres = append(result.Genres, series.Genres...)
	if series.Studio != "" {
		result.Studios = []string{series.Studio}
	}

	seasons, err := p.client.GetSeasons(ctx, leagueID)
	if err == nil && seasons != nil {
		result.SeasonCount = len(seasons.Seasons)
	}

	// hub_id is an implementation identifier used only by the entity-image API.
	// Keep the stable short ID for metadata-agent calls and persisted provider IDs.
	if series.HubID != "" {
		imgs, err := p.client.GetEntityImages(ctx, "league", series.HubID)
		if err == nil {
			result.PosterPath = pickPrimaryURL(imgs.Images, "poster")
			result.BackdropPath = pickPrimaryURL(imgs.Images, "backdrop")
			result.LogoPath = pickPrimaryURL(imgs.Images, "logo")
		}
	}

	// Fall back to the absolute URLs the agent series endpoint provides when the
	// entity-image catalog has no completed image of that kind.
	if result.PosterPath == "" {
		result.PosterPath = series.PosterURL
	}
	if result.BackdropPath == "" {
		result.BackdropPath = series.FanartURL
	}

	return result, nil
}

func (p *Provider) GetSeasons(ctx context.Context, req metadata.SeasonsRequest) ([]metadata.SeasonResult, error) {
	sportarrID := req.ProviderIDs["sportarr"]
	if sportarrID == "" {
		return nil, nil
	}

	resp, err := p.client.GetSeasons(ctx, sportarrID)
	if err != nil {
		return nil, err
	}

	var seasonIDs []string
	for _, s := range resp.Seasons {
		if s.HubID != "" {
			seasonIDs = append(seasonIDs, s.HubID)
		}
	}
	imagesByID := p.client.GetEntityImagesBatch(ctx, "season", seasonIDs)

	seasons := make([]metadata.SeasonResult, 0, len(resp.Seasons))
	for _, s := range resp.Seasons {
		seasonID := entityID(s.HubID, s.ID)
		if seasonID == "" {
			seasonID = fmt.Sprintf("%s:%d", sportarrID, s.SeasonNumber)
		}
		posterPath := ""
		if imgs, ok := imagesByID[s.HubID]; ok {
			posterPath = pickPrimaryURL(imgs.Images, "poster")
		}
		if posterPath == "" {
			posterPath = s.PosterURL
		}
		seasons = append(seasons, metadata.SeasonResult{
			ContentID:    seasonID,
			SeasonNumber: s.SeasonNumber,
			Title:        s.Title,
			PosterPath:   posterPath,
		})
	}
	return seasons, nil
}

func (p *Provider) GetEpisodes(ctx context.Context, req metadata.EpisodesRequest) ([]metadata.EpisodeResult, error) {
	sportarrID := req.ProviderIDs["sportarr"]
	if sportarrID == "" {
		return nil, nil
	}

	resp, err := p.client.GetSeasonEpisodes(ctx, sportarrID, req.SeasonNumber)
	if err != nil {
		return nil, err
	}

	episodes := make([]metadata.EpisodeResult, 0, len(resp.Episodes))
	for _, ep := range resp.Episodes {
		eventID := entityID(ep.HubID, ep.ID)
		providerIDs := map[string]string{"sportarr": eventID}
		episodes = append(episodes, metadata.EpisodeResult{
			ContentID:     eventID,
			ProviderIDs:   providerIDs,
			SeasonNumber:  ep.SeasonNumber,
			EpisodeNumber: ep.EpisodeNumber,
			Title:         ep.Title,
			Overview:      ep.Summary,
			AirDate:       ep.AirDate,
			Runtime:       ep.DurationMinutes,
			StillPath:     ep.ThumbURL,
		})
	}
	return episodes, nil
}

func (p *Provider) GetImages(ctx context.Context, req metadata.ImageRequest) ([]metadata.RemoteImage, error) {
	sportarrID := req.ProviderIDs["sportarr"]
	if sportarrID == "" {
		return nil, nil
	}

	var entityType string
	switch req.ContentType {
	case "series":
		entityType = "league"
		series, err := p.client.GetSeries(ctx, sportarrID)
		if err != nil {
			return nil, err
		}
		if series.HubID == "" {
			return nil, nil
		}
		sportarrID = series.HubID
	case "season":
		entityType = "season"
	case "episode":
		entityType = "event"
	default:
		return nil, nil
	}

	resp, err := p.client.GetEntityImages(ctx, entityType, sportarrID)
	if err != nil {
		return nil, err
	}
	return entityImagesToRemote(resp.Images), nil
}
