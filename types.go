package videodl

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

type ClientConfig struct {
	DownloadDir string
	YTDLPBinary string
}
