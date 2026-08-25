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
	return NewProviderWithAPIKey(baseURL, "")
}

func NewProviderWithAPIKey(baseURL, apiKey string) *Provider {
	c := NewClient(10)
	if baseURL != "" {
		c.SetBaseURL(baseURL)
	}
	c.SetAPIKey(apiKey)
	return &Provider{client: c}
}

func NewProviderWithClient(c *Client) *Provider {
	return &Provider{client: c}
}

func (p *Provider) Slug() string       { return "sportarr" }
func (p *Provider) Name() string       { return "Sportarr" }
func (p *Provider) ForTypes() []string { return []string{"series"} }

func (p *Provider) ResolveImageRedirect(ctx context.Context, path string) (string, error) {
	return p.client.ResolveImageRedirect(ctx, path)
}

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
		ProviderIDs: map[string]string{"sportarr": leagueID},
		ImageURL:    series.PosterURL,
		Overview:    series.Summary,
		Provider:    p.Slug(),
	}}, nil
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
			ProviderIDs: map[string]string{"sportarr": r.ID},
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

	result := &metadata.MetadataResult{
		HasMetadata:   true,
		Title:         series.Title,
		Overview:      series.Summary,
		Year:          series.Year,
		ContentRating: series.ContentRating,
		ProviderIDs:   map[string]string{"sportarr": sportarrID},
		PosterPath:    series.PosterURL,
		BackdropPath:  series.FanartURL,
	}

	result.Genres = append(result.Genres, series.Genres...)
	if series.Studio != "" {
		result.Studios = []string{series.Studio}
	}

	seasons, err := p.client.GetSeasons(ctx, sportarrID)
	if err == nil && seasons != nil {
		result.SeasonCount = len(seasons.Seasons)
	}

	if p.client.hasAPIKey() {
		imgs, err := p.client.GetEntityImages(ctx, "league", sportarrID)
		if err == nil {
			if poster := pickPrimaryURL(imgs.Images, "poster"); poster != "" {
				result.PosterPath = poster
			}
			if backdrop := pickPrimaryURL(imgs.Images, "backdrop"); backdrop != "" {
				result.BackdropPath = backdrop
			}
			result.LogoPath = pickPrimaryURL(imgs.Images, "logo")
		}
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

	imagesByID := make(map[string]*EntityImageResponse)
	if p.client.hasAPIKey() {
		var seasonIDs []string
		for _, s := range resp.Seasons {
			if s.CompetitionSeasonID != "" {
				seasonIDs = append(seasonIDs, s.CompetitionSeasonID)
			}
		}
		imagesByID = p.client.GetEntityImagesBatch(ctx, "season", seasonIDs)
	}

	seasons := make([]metadata.SeasonResult, 0, len(resp.Seasons))
	for _, s := range resp.Seasons {
		title := s.Name
		if title == "" {
			title = s.Title
		}
		posterPath := s.PosterURL
		if imgs, ok := imagesByID[s.CompetitionSeasonID]; ok {
			if poster := pickPrimaryURL(imgs.Images, "poster"); poster != "" {
				posterPath = poster
			}
		}
		seasons = append(seasons, metadata.SeasonResult{
			ContentID:    fmt.Sprintf("%s:%d", sportarrID, s.SeasonNumber),
			SeasonNumber: s.SeasonNumber,
			Title:        title,
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
		providerIDs := map[string]string{"sportarr": ep.ID}
		episodes = append(episodes, metadata.EpisodeResult{
			ContentID:     ep.ID,
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
	case "season":
		entityType = "season"
	case "episode":
		entityType = "event"
	default:
		return nil, nil
	}
	if !p.client.hasAPIKey() {
		if req.ContentType != "series" {
			return nil, nil
		}
		series, err := p.client.GetSeries(ctx, sportarrID)
		if err != nil {
			return nil, err
		}
		return publicSeriesImages(series), nil
	}

	resp, err := p.client.GetEntityImages(ctx, entityType, sportarrID)
	if err != nil {
		if req.ContentType == "series" {
			series, fallbackErr := p.client.GetSeries(ctx, sportarrID)
			if fallbackErr == nil {
				return publicSeriesImages(series), nil
			}
		}
		return nil, err
	}
	return entityImagesToRemote(resp.Images), nil
}

func publicSeriesImages(series *AgentSeriesResponse) []metadata.RemoteImage {
	images := make([]metadata.RemoteImage, 0, 3)
	if series.PosterURL != "" {
		images = append(images, metadata.RemoteImage{URL: series.PosterURL, Type: metadata.ImagePoster})
	}
	if series.FanartURL != "" {
		images = append(images, metadata.RemoteImage{URL: series.FanartURL, Type: metadata.ImageBackdrop})
	}
	if series.BannerURL != "" {
		images = append(images, metadata.RemoteImage{URL: series.BannerURL, Type: metadata.ImageBanner})
	}
	return images
}
