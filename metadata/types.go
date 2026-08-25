package metadata

type SearchQuery struct {
	Title       string
	Year        int
	ContentType string
	ProviderIDs map[string]string
	Language    string
}

type SearchResult struct {
	Name        string
	Year        int
	ProviderIDs map[string]string
	ImageURL    string
	Overview    string
	Provider    string
}

type MetadataRequest struct {
	ProviderIDs map[string]string
	ContentType string
	Language    string
	FilePath    string
}

type MetadataResult struct {
	HasMetadata      bool
	ProviderIDs      map[string]string
	Title            string
	OriginalTitle    string
	SortTitle        string
	Overview         string
	Tagline          string
	Year             int
	Runtime          int
	Genres           []string
	Studios          []string
	Networks         []string
	Countries        []string
	OriginalLanguage string
	ContentRating    string
	Ratings          Ratings
	PosterPath       string
	PosterThumbhash  string
	BackdropPath     string
	BackdropThumbhash string
	LogoPath         string
	SeasonCount      int
	FirstAirDate     string
	LastAirDate      string
	AirTime          string
}

type Ratings struct {
	IMDB       float64
	TMDB       float64
	RTCritic   float64
	RTAudience float64
}

type ImageRequest struct {
	ProviderIDs map[string]string
	ContentType string
	Language    string
}

type RemoteImage struct {
	URL      string
	Type     ImageType
	Language string
	Width    int
	Height   int
	Rating   float64
}

type ImageType int

const (
	ImagePoster ImageType = iota
	ImageBackdrop
	ImageLogo
	ImageStill
	ImageBanner
)

type SeasonsRequest struct {
	ProviderIDs map[string]string
	ContentType string
	Language    string
}

type EpisodesRequest struct {
	ProviderIDs  map[string]string
	SeasonNumber int
	Language     string
}

type SeasonResult struct {
	ContentID    string
	SeasonNumber int
	Title        string
	Overview     string
	AirDate      string
	PosterPath   string
}

type EpisodeResult struct {
	ContentID     string
	ProviderIDs   map[string]string
	SeasonNumber  int
	EpisodeNumber int
	Title         string
	Overview      string
	AirDate       string
	Runtime       int
	Ratings       Ratings
	StillPath     string
}
