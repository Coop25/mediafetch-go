package mediafetch

import "context"

type Format struct {
	FormatID   string `json:"format_id"`
	Resolution string `json:"resolution"`
	Ext        string `json:"ext"`
	Filesize   *int64 `json:"filesize,omitempty"`
	FormatNote string `json:"format_note"`
}

type VideoInfo struct {
	ThumbnailURL string
	Title        string
	Duration     *int
	Formats      []Format
}

type DirectMedia struct {
	FormatID   string `json:"format_id"`
	URL        string `json:"url"`
	Resolution string `json:"resolution"`
	Ext        string `json:"ext"`
	Filesize   *int64 `json:"filesize,omitempty"`
	FormatNote string `json:"format_note"`
}

type FallbackMedia struct {
	CanonicalURL string        `json:"canonical_url,omitempty"`
	Info         VideoInfo     `json:"info"`
	Downloads    []DirectMedia `json:"downloads,omitempty"`
}

type FallbackProvider interface {
	Supports(rawURL string) bool
	Resolve(ctx context.Context, rawURL string) (*FallbackMedia, error)
}

type ClientConfig struct {
	DownloadDir       string
	YTDLPBinary       string
	FallbackProviders []FallbackProvider
}
