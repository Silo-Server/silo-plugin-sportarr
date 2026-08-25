package main

import (
	"context"
	"crypto/sha256"
	_ "embed"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"strings"

	"google.golang.org/protobuf/types/known/structpb"

	pluginv1 "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginproto/silo/plugin/v1"
	publicmanifest "github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/manifest"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtime"
	"github.com/Silo-Server/silo-plugin-sdk/pkg/pluginsdk/runtimedefault"
	"github.com/Silo-Server/silo-plugin-sportarr/metadata"
	"github.com/Silo-Server/silo-plugin-sportarr/provider"
)

var version string

const defaultBaseURL = "https://sportarr.net"

func sportarrCanonicalPath(baseURL, imageURL string) string {
	if imageURL == "" {
		return ""
	}

	base, baseErr := url.Parse(baseURL)
	image, imageErr := url.Parse(imageURL)
	if baseErr == nil && imageErr == nil && base.IsAbs() && image.IsAbs() &&
		strings.EqualFold(base.Scheme, image.Scheme) && strings.EqualFold(base.Host, image.Host) {
		return "sportarr://" + image.RequestURI()
	}

	// The Sportarr image API returns root-relative paths (e.g. /static/images/…);
	// retain their origin-relative meaning in the canonical form.
	if imageErr == nil && image.Host == "" && strings.HasPrefix(imageURL, "/") {
		return "sportarr://" + image.RequestURI()
	}
	return imageURL
}

func resolveOneSportarrPath(baseURL, path, _ string) string {
	if path == "" {
		return ""
	}
	if strings.HasPrefix(path, "sportarr://") {
		base, baseErr := url.Parse(baseURL)
		reference, referenceErr := url.Parse(strings.TrimPrefix(path, "sportarr://"))
		if baseErr == nil && referenceErr == nil && base.IsAbs() && !reference.IsAbs() && reference.Host == "" {
			return base.ResolveReference(reference).String()
		}
	}
	return path
}

type runtimeServer struct {
	runtimedefault.Server

	manifest *pluginv1.PluginManifest
	provider *provider.Provider
	baseURL  string
}

type metadataServer struct {
	pluginv1.UnimplementedMetadataProviderServer
	runtime *runtimeServer
}

//go:embed manifest.json
var manifestJSON []byte

func (s *runtimeServer) GetManifest(context.Context, *pluginv1.GetManifestRequest) (*pluginv1.GetManifestResponse, error) {
	return &pluginv1.GetManifestResponse{Manifest: s.manifest}, nil
}

func (s *runtimeServer) Configure(_ context.Context, req *pluginv1.ConfigureRequest) (*pluginv1.ConfigureResponse, error) {
	baseURL := defaultBaseURL

	for _, entry := range req.GetConfig() {
		if entry.GetKey() != "sportarr" {
			continue
		}
		if val := entry.GetValue(); val != nil {
			m := val.AsMap()
			if u, ok := m["base_url"].(string); ok && u != "" {
				baseURL = strings.TrimRight(u, "/")
			}
		}
	}

	s.baseURL = baseURL
	s.provider = provider.NewProvider(baseURL)
	return &pluginv1.ConfigureResponse{}, nil
}

func (s *runtimeServer) providerForRequest() (*provider.Provider, error) {
	if s.provider == nil {
		return nil, fmt.Errorf("sportarr: plugin not configured")
	}
	return s.provider, nil
}

func (s *metadataServer) Search(ctx context.Context, req *pluginv1.SearchMetadataRequest) (*pluginv1.SearchMetadataResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.Search(ctx, metadata.SearchQuery{
		Title:       req.GetQuery(),
		Year:        int(req.GetYear()),
		ContentType: req.GetItemType(),
		ProviderIDs: stringMapFromStruct(req.GetProviderIds()),
		Language:    req.GetLanguage(),
	})
	if err != nil {
		return nil, err
	}

	response := &pluginv1.SearchMetadataResponse{
		Results: make([]*pluginv1.ProviderSearchResult, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(result.ProviderIDs)
		if err != nil {
			return nil, err
		}
		response.Results = append(response.Results, &pluginv1.ProviderSearchResult{
			ProviderId:  result.ProviderIDs["sportarr"],
			ItemType:    req.GetItemType(),
			Title:       result.Name,
			Year:        int32(result.Year),
			Overview:    result.Overview,
			ProviderIds: providerIDs,
			ImageUrl:    sportarrCanonicalPath(s.runtime.baseURL, result.ImageURL),
		})
	}
	return response, nil
}

func (s *metadataServer) GetMetadata(ctx context.Context, req *pluginv1.GetMetadataRequest) (*pluginv1.GetMetadataResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	result, err := p.GetMetadata(ctx, metadataRequestFromProto(req, "sportarr"))
	if err != nil || result == nil {
		return nil, err
	}

	item, err := metadataItemFromResult(result, req.GetItemType(), s.runtime.baseURL)
	if err != nil {
		return nil, err
	}
	return &pluginv1.GetMetadataResponse{Item: item}, nil
}

func (s *metadataServer) GetPersonDetail(_ context.Context, _ *pluginv1.GetPersonDetailRequest) (*pluginv1.GetPersonDetailResponse, error) {
	return &pluginv1.GetPersonDetailResponse{}, nil
}

func (s *metadataServer) GetSeasons(ctx context.Context, req *pluginv1.GetSeasonsRequest) (*pluginv1.GetSeasonsResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.GetSeasons(ctx, seasonsRequestFromProto(req, "sportarr"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetSeasonsResponse{
		Seasons: make([]*pluginv1.SeasonRecord, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(map[string]string{"sportarr": result.ContentID})
		if err != nil {
			return nil, err
		}
		response.Seasons = append(response.Seasons, &pluginv1.SeasonRecord{
			ProviderId:   result.ContentID,
			ProviderIds:  providerIDs,
			SeasonNumber: int32(result.SeasonNumber),
			Title:        result.Title,
			Overview:     result.Overview,
			AirDate:      result.AirDate,
			PosterPath:   sportarrCanonicalPath(s.runtime.baseURL, result.PosterPath),
		})
	}
	return response, nil
}

func (s *metadataServer) GetEpisodes(ctx context.Context, req *pluginv1.GetEpisodesRequest) (*pluginv1.GetEpisodesResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	results, err := p.GetEpisodes(ctx, episodesRequestFromProto(req, "sportarr"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetEpisodesResponse{
		Episodes: make([]*pluginv1.EpisodeRecord, 0, len(results)),
	}
	for _, result := range results {
		providerIDs, err := stringStruct(result.ProviderIDs)
		if err != nil {
			return nil, err
		}
		response.Episodes = append(response.Episodes, &pluginv1.EpisodeRecord{
			ProviderId:    result.ContentID,
			SeasonNumber:  int32(result.SeasonNumber),
			EpisodeNumber: int32(result.EpisodeNumber),
			Title:         result.Title,
			Overview:      result.Overview,
			AirDate:       result.AirDate,
			Runtime:       int32(result.Runtime),
			StillPath:     sportarrCanonicalPath(s.runtime.baseURL, result.StillPath),
			ProviderIds:   providerIDs,
		})
	}
	return response, nil
}

func (s *metadataServer) GetImages(ctx context.Context, req *pluginv1.GetImagesRequest) (*pluginv1.GetImagesResponse, error) {
	p, err := s.runtime.providerForRequest()
	if err != nil {
		return nil, err
	}

	images, err := p.GetImages(ctx, imageRequestFromProto(req, "sportarr"))
	if err != nil {
		return nil, err
	}

	response := &pluginv1.GetImagesResponse{}
	for _, img := range images {
		kind := ""
		switch img.Type {
		case metadata.ImagePoster:
			kind = "poster"
		case metadata.ImageBackdrop:
			kind = "backdrop"
		case metadata.ImageLogo:
			kind = "logo"
		case metadata.ImageStill:
			kind = "still"
		case metadata.ImageBanner:
			kind = "banner"
		}
		response.Images = append(response.Images, &pluginv1.ImageRecord{
			Kind:   kind,
			Url:    sportarrCanonicalPath(s.runtime.baseURL, img.URL),
			Width:  int32(img.Width),
			Height: int32(img.Height),
		})
	}
	return response, nil
}

func (s *metadataServer) ResolveImageURL(_ context.Context, req *pluginv1.ResolveImageURLRequest) (*pluginv1.ResolveImageURLResponse, error) {
	resolved := resolveOneSportarrPath(s.runtime.baseURL, req.GetPath(), req.GetVariant())
	return &pluginv1.ResolveImageURLResponse{Url: resolved}, nil
}

func (s *metadataServer) ResolveImageURLs(_ context.Context, req *pluginv1.ResolveImageURLsRequest) (*pluginv1.ResolveImageURLsResponse, error) {
	urls := make(map[string]string, len(req.GetPaths()))
	for _, path := range req.GetPaths() {
		urls[path] = resolveOneSportarrPath(s.runtime.baseURL, path, req.GetVariant())
	}
	return &pluginv1.ResolveImageURLsResponse{Urls: urls}, nil
}

func main() {
	manifest, err := loadManifest()
	if err != nil {
		panic(err)
	}

	rs := &runtimeServer{
		manifest: manifest,
		provider: provider.NewProvider(defaultBaseURL),
		baseURL:  defaultBaseURL,
	}

	runtime.Serve(runtime.ServeConfig{
		Servers: runtime.CapabilityServers{
			Runtime:          rs,
			MetadataProvider: &metadataServer{runtime: rs},
		},
	})
}

func loadManifest() (*pluginv1.PluginManifest, error) {
	manifest, err := publicmanifest.Load(manifestJSON)
	if err != nil {
		return nil, fmt.Errorf("load embedded manifest: %w", err)
	}

	if version != "" {
		manifest.Version = version
	}

	executablePath, err := os.Executable()
	if err != nil {
		return nil, fmt.Errorf("resolve executable path: %w", err)
	}
	binaryData, err := os.ReadFile(executablePath)
	if err != nil {
		return nil, fmt.Errorf("read executable %q: %w", executablePath, err)
	}
	checksum := sha256.Sum256(binaryData)
	manifest.Checksum = hex.EncodeToString(checksum[:])

	return manifest, nil
}

func metadataItemFromResult(result *metadata.MetadataResult, itemType, baseURL string) (*pluginv1.MetadataItem, error) {
	providerIDs, err := stringStruct(result.ProviderIDs)
	if err != nil {
		return nil, err
	}

	return &pluginv1.MetadataItem{
		ProviderId:       result.ProviderIDs["sportarr"],
		ItemType:         itemType,
		Title:            result.Title,
		OriginalTitle:    result.OriginalTitle,
		SortTitle:        result.SortTitle,
		Year:             int32(result.Year),
		Overview:         result.Overview,
		Tagline:          result.Tagline,
		Runtime:          int32(result.Runtime),
		Genres:           append([]string(nil), result.Genres...),
		Studios:          append([]string(nil), result.Studios...),
		Networks:         append([]string(nil), result.Networks...),
		Countries:        append([]string(nil), result.Countries...),
		OriginalLanguage: result.OriginalLanguage,
		ContentRating:    result.ContentRating,
		ProviderIds:      providerIDs,
		PosterPath:       sportarrCanonicalPath(baseURL, result.PosterPath),
		BackdropPath:     sportarrCanonicalPath(baseURL, result.BackdropPath),
		LogoPath:         sportarrCanonicalPath(baseURL, result.LogoPath),
		SeasonCount:      int32(result.SeasonCount),
		FirstAirDate:     result.FirstAirDate,
		LastAirDate:      result.LastAirDate,
		AirTime:          result.AirTime,
	}, nil
}

func stringMapFromStruct(value *structpb.Struct) map[string]string {
	result := make(map[string]string)
	if value == nil {
		return result
	}
	for key, raw := range value.AsMap() {
		text, ok := raw.(string)
		if ok && text != "" {
			result[key] = text
		}
	}
	return result
}

func providerIDsFromProto(value *structpb.Struct, capabilityID string, fallbackID string) map[string]string {
	result := stringMapFromStruct(value)
	if fallbackID != "" && result[capabilityID] == "" {
		result[capabilityID] = fallbackID
	}
	return result
}

func metadataRequestFromProto(req *pluginv1.GetMetadataRequest, capabilityID string) metadata.MetadataRequest {
	return metadata.MetadataRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetProviderId()),
		ContentType: req.GetItemType(),
		Language:    req.GetLanguage(),
		FilePath:    req.GetFilePath(),
	}
}

func seasonsRequestFromProto(req *pluginv1.GetSeasonsRequest, capabilityID string) metadata.SeasonsRequest {
	return metadata.SeasonsRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetSeriesProviderId()),
		ContentType: "series",
		Language:    req.GetLanguage(),
	}
}

func episodesRequestFromProto(req *pluginv1.GetEpisodesRequest, capabilityID string) metadata.EpisodesRequest {
	return metadata.EpisodesRequest{
		ProviderIDs:  providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetSeriesProviderId()),
		SeasonNumber: int(req.GetSeasonNumber()),
		Language:     req.GetLanguage(),
	}
}

func imageRequestFromProto(req *pluginv1.GetImagesRequest, capabilityID string) metadata.ImageRequest {
	return metadata.ImageRequest{
		ProviderIDs: providerIDsFromProto(req.GetProviderIds(), capabilityID, req.GetProviderId()),
		ContentType: req.GetItemType(),
		Language:    req.GetLanguage(),
	}
}

func stringStruct(value map[string]string) (*structpb.Struct, error) {
	if len(value) == 0 {
		return nil, nil
	}

	converted := make(map[string]any, len(value))
	for key, entry := range value {
		if entry == "" {
			continue
		}
		converted[key] = entry
	}
	if len(converted) == 0 {
		return nil, nil
	}
	return structpb.NewStruct(converted)
}
