package mediafetch

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

type YTDLPSettings struct {
	UserAgent          string
	CookiesFromBrowser string
	CookiesFile        string
	Proxy              string
	ConfigLocation     string
}

type ClientConfig struct {
	DownloadDir   string
	YTDLPBinary   string
	YTDLPSettings YTDLPSettings
}
