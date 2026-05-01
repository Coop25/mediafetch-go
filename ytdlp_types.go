package videodl

type ytdlpInfo struct {
	Title     string        `json:"title"`
	Thumbnail string        `json:"thumbnail"`
	Duration  *float64      `json:"duration"`
	Formats   []ytdlpFormat `json:"formats"`
}

type ytdlpFormat struct {
	FormatID   string `json:"format_id"`
	Resolution string `json:"resolution"`
	FormatNote string `json:"format_note"`
	Ext        string `json:"ext"`
	Protocol   string `json:"protocol"`
	VCodec     string `json:"vcodec"`
	Width      *int   `json:"width"`
	Height     *int   `json:"height"`
	Filesize   *int64 `json:"filesize"`
}
