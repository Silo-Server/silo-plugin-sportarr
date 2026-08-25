package provider

// AgentSearchResponse is returned by GET /api/metadata/agents/search.
type AgentSearchResponse struct {
	Results []AgentSearchResult `json:"results"`
}

type AgentSearchResult struct {
	ID        string `json:"id"`
	Title     string `json:"title"`
	Year      int    `json:"year"`
	PosterURL string `json:"poster_url"`
}

// AgentSeriesResponse is returned by GET /api/metadata/agents/series/{league_id}.
type AgentSeriesResponse struct {
	Title         string   `json:"title"`
	Summary       string   `json:"summary"`
	ContentRating string   `json:"content_rating"`
	Year          int      `json:"year"`
	Genres        []string `json:"genres"`
	Studio        string   `json:"studio"`
	PosterURL     string   `json:"poster_url"`
	BannerURL     string   `json:"banner_url"`
	FanartURL     string   `json:"fanart_url"`
}

// AgentSeasonsResponse is returned by GET /api/metadata/agents/series/{league_id}/seasons.
type AgentSeasonsResponse struct {
	Seasons []AgentSeason `json:"seasons"`
}

type AgentSeason struct {
	CompetitionSeasonID string `json:"competition_season_id"`
	SeasonNumber        int    `json:"season_number"`
	Name                string `json:"name"`
	Title               string `json:"title"`
	PosterURL           string `json:"poster_url"`
	EpisodeCount        int    `json:"episode_count"`
}

// AgentEpisodesResponse is returned by
// GET /api/metadata/agents/series/{league_id}/season/{num}/episodes.
type AgentEpisodesResponse struct {
	Episodes []AgentEpisode `json:"episodes"`
}

type AgentEpisode struct {
	ID              string `json:"id"`
	Title           string `json:"title"`
	Summary         string `json:"summary"`
	SeasonNumber    int    `json:"season_number"`
	EpisodeNumber   int    `json:"episode_number"`
	AirDate         string `json:"air_date"`
	DurationMinutes int    `json:"duration_minutes"`
	ThumbURL        string `json:"thumb_url"`
	PartName        string `json:"part_name"`
}

// EntityImageResponse is returned by GET /api/v1/images/entity/{type}/{id}.
type EntityImageResponse struct {
	Images []EntityImage `json:"images"`
}

type EntityImage struct {
	ID        string `json:"id"`
	ImageType string `json:"image_type"`
	URL       string `json:"url"`
	Width     *int   `json:"width"`
	Height    *int   `json:"height"`
	IsPrimary bool   `json:"is_primary"`
	Priority  int    `json:"priority"`
}
