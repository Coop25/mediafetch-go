package mediafetch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strconv"
	"strings"
)

type Client struct {
	downloadDir string
	ytdlpBinary string
	settings    YTDLPSettings
}

const fallbackManifestName = ".mediafetch-fallback.json"

type directMedia struct {
	FormatID   string `json:"format_id"`
	URL        string `json:"url"`
	Resolution string `json:"resolution"`
	Ext        string `json:"ext"`
	Filesize   *int64 `json:"filesize,omitempty"`
	FormatNote string `json:"format_note"`
}

type fallbackMedia struct {
	CanonicalURL string        `json:"canonical_url,omitempty"`
	Info         VideoInfo     `json:"info"`
	Downloads    []directMedia `json:"downloads,omitempty"`
}

func NewClient(cfg ClientConfig) (*Client, error) {
	downloadDir := strings.TrimSpace(cfg.DownloadDir)
	if downloadDir == "" {
		return nil, errors.New("download directory is required")
	}

	ytdlpBinary := strings.TrimSpace(cfg.YTDLPBinary)
	if ytdlpBinary == "" {
		ytdlpBinary = "yt-dlp"
	}

	if err := os.MkdirAll(downloadDir, 0o755); err != nil {
		return nil, fmt.Errorf("create download directory: %w", err)
	}

	return &Client{
		downloadDir: downloadDir,
		ytdlpBinary: ytdlpBinary,
		settings:    normalizeYTDLPSettings(cfg.YTDLPSettings),
	}, nil
}

func (c *Client) DownloadDir() string {
	return c.downloadDir
}

func (c *Client) Extract(ctx context.Context, rawURL string) (string, VideoInfo, error) {
	if !ValidateSupportedURL(rawURL) {
		return "", VideoInfo{}, errors.New("not a valid supported video URL")
	}

	downloadID, err := randomID()
	if err != nil {
		return "", VideoInfo{}, fmt.Errorf("generate download ID: %w", err)
	}

	downloadPath := filepath.Join(c.downloadDir, downloadID)
	if err := os.MkdirAll(downloadPath, 0o755); err != nil {
		return "", VideoInfo{}, fmt.Errorf("create download directory: %w", err)
	}

	info, lastErr := c.extractWithYTDLP(ctx, rawURL)
	if lastErr == nil {
		return downloadID, VideoInfo{
			ThumbnailURL: info.ThumbnailURL,
			Title:        info.Title,
			Duration:     info.Duration,
			Formats:      info.Formats,
		}, nil
	}

	if shouldRetryWithResolvedURL(rawURL, lastErr) {
		resolvedURL := c.resolveMediaURL(ctx, rawURL)
		if resolvedURL != rawURL {
			info, resolvedErr := c.extractWithYTDLP(ctx, resolvedURL)
			if resolvedErr == nil {
				if err := writeFallbackManifest(downloadPath, fallbackMedia{CanonicalURL: resolvedURL, Info: info}); err != nil {
					_ = os.RemoveAll(downloadPath)
					return "", VideoInfo{}, err
				}

				return downloadID, VideoInfo{
					ThumbnailURL: info.ThumbnailURL,
					Title:        info.Title,
					Duration:     info.Duration,
					Formats:      info.Formats,
				}, nil
			}
			lastErr = resolvedErr
		}
	}

	if media, fallbackErr := c.extractWithFacebookHTMLFallback(ctx, rawURL); fallbackErr == nil {
		if media.CanonicalURL != "" {
			if info, resolvedErr := c.extractWithYTDLP(ctx, media.CanonicalURL); resolvedErr == nil {
				media.Info = mergeFallbackInfo(media.Info, info)
			}
		}

		if len(media.Info.Formats) == 0 {
			media.Info.Formats = formatsFromDirectMedia(media.Downloads)
		}
		media.Info.Title = fallback(media.Info.Title, "Video")

		if err := writeFallbackManifest(downloadPath, *media); err != nil {
			_ = os.RemoveAll(downloadPath)
			return "", VideoInfo{}, err
		}

		return downloadID, media.Info, nil
	} else if fallbackErr != nil {
		lastErr = fallbackErr
	}

	_ = os.RemoveAll(downloadPath)
	return "", VideoInfo{}, classifyProcessingError(rawURL, lastErr)
}

func (c *Client) Download(ctx context.Context, rawURL, downloadID, formatID string) (string, error) {
	if !ValidateSupportedURL(rawURL) {
		return "", errors.New("not a valid supported video URL")
	}

	if downloadID == "" || strings.Contains(downloadID, "/") || strings.Contains(downloadID, "..") {
		return "", errors.New("invalid download ID")
	}

	downloadPath := filepath.Join(c.downloadDir, downloadID)
	if err := os.MkdirAll(downloadPath, 0o755); err != nil {
		return "", fmt.Errorf("create download directory: %w", err)
	}

	if media, err := readFallbackManifest(downloadPath); err == nil {
		filePath, handled, downloadErr := c.downloadFromFallback(ctx, rawURL, downloadPath, formatID, media)
		if handled {
			return filePath, downloadErr
		}
	}

	outputTemplate := filepath.Join(downloadPath, "%(title)s.%(ext)s")
	lastErr := c.downloadWithYTDLP(ctx, rawURL, outputTemplate, formatID)

	if lastErr != nil && shouldRetryWithResolvedURL(rawURL, lastErr) {
		resolvedURL := c.resolveMediaURL(ctx, rawURL)
		if resolvedURL != rawURL {
			lastErr = c.downloadWithYTDLP(ctx, resolvedURL, outputTemplate, formatID)
		}
	}

	if lastErr == nil {
		return c.findDownloadedFile(downloadPath)
	}

	if media, fallbackErr := c.extractWithFacebookHTMLFallback(ctx, rawURL); fallbackErr == nil {
		filePath, handled, downloadErr := c.downloadFromFallback(ctx, rawURL, downloadPath, formatID, *media)
		if handled {
			if downloadErr == nil {
				_ = writeFallbackManifest(downloadPath, *media)
			}
			return filePath, downloadErr
		}
		lastErr = fallbackErr
	}

	return "", classifyProcessingError(rawURL, lastErr)
}

func (c *Client) extractWithYTDLP(ctx context.Context, rawURL string) (VideoInfo, error) {
	var lastErr error
	for _, args := range buildInfoStrategies(rawURL, c.settings) {
		output, err := runCommand(ctx, c.ytdlpBinary, args...)
		if err != nil {
			lastErr = err
			continue
		}

		var info ytdlpInfo
		if err := json.Unmarshal(output, &info); err != nil {
			lastErr = fmt.Errorf("parse yt-dlp output: %w", err)
			continue
		}

		formats := normalizeFormats(info.Formats)
		if len(formats) == 0 {
			formats = []Format{{
				FormatID:   "best",
				Resolution: "best",
				Ext:        "mp4",
				FormatNote: "Best available quality - MP4 (recommended)",
			}}
		}

		var duration *int
		if info.Duration != nil {
			d := int(math.Round(*info.Duration))
			duration = &d
		}

		return VideoInfo{
			ThumbnailURL: info.Thumbnail,
			Title:        fallback(info.Title, "Video"),
			Duration:     duration,
			Formats:      formats,
		}, nil
	}

	return VideoInfo{}, lastErr
}

func (c *Client) downloadWithYTDLP(ctx context.Context, rawURL, outputTemplate, formatID string) error {
	primarySelector, fallbackSelector := buildDownloadSelectors(rawURL, formatID)
	args := buildDownloadArgs(rawURL, outputTemplate, primarySelector, c.settings)
	if _, err := runCommand(ctx, c.ytdlpBinary, args...); err != nil {
		if strings.Contains(err.Error(), "Requested format is not available") && fallbackSelector != primarySelector {
			retryArgs := buildDownloadArgs(rawURL, outputTemplate, fallbackSelector, c.settings)
			if _, retryErr := runCommand(ctx, c.ytdlpBinary, retryArgs...); retryErr == nil {
				return nil
			} else {
				return retryErr
			}
		}
		return err
	}

	return nil
}

func shouldRetryWithResolvedURL(rawURL string, err error) bool {
	if err == nil || !shouldResolveRedirect(rawURL) {
		return false
	}

	return strings.Contains(err.Error(), "Unsupported URL")
}

func (c *Client) resolveMediaURL(ctx context.Context, rawURL string) string {
	if !shouldResolveRedirect(rawURL) {
		return rawURL
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return rawURL
	}

	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	client := &http.Client{
		CheckRedirect: func(req *http.Request, via []*http.Request) error {
			if len(via) >= 10 {
				return errors.New("stopped after 10 redirects")
			}
			return nil
		},
	}

	resp, err := client.Do(req)
	if err != nil {
		return rawURL
	}
	defer resp.Body.Close()

	if resp.Request == nil || resp.Request.URL == nil {
		return rawURL
	}

	resolvedURL := resp.Request.URL.String()
	if strings.TrimSpace(resolvedURL) == "" {
		return rawURL
	}

	return resolvedURL
}

func (c *Client) extractWithFacebookHTMLFallback(ctx context.Context, rawURL string) (*fallbackMedia, error) {
	if !IsFacebookURL(rawURL) {
		return nil, errors.New("facebook html fallback only supports facebook urls")
	}

	candidates := []string{rawURL}
	resolvedURL := c.resolveMediaURL(ctx, rawURL)
	if resolvedURL != rawURL {
		candidates = append(candidates, resolvedURL)
	}

	var lastErr error
	for _, candidateURL := range candidates {
		html, finalURL, err := c.fetchHTMLSource(ctx, candidateURL)
		if err != nil {
			lastErr = err
			continue
		}

		media := parseFacebookHTMLFallback(html)
		if media == nil {
			lastErr = errors.New("facebook html fallback found no media URLs")
			continue
		}

		if strings.TrimSpace(media.CanonicalURL) == "" {
			media.CanonicalURL = finalURL
		}
		if strings.TrimSpace(media.Info.Title) == "" {
			media.Info.Title = parseHTMLMetaContent(html, "og:title")
		}
		if strings.TrimSpace(media.Info.ThumbnailURL) == "" {
			media.Info.ThumbnailURL = parseHTMLMetaContent(html, "og:image")
		}
		if len(media.Info.Formats) == 0 {
			media.Info.Formats = formatsFromDirectMedia(media.Downloads)
		}
		if len(media.Downloads) > 0 {
			return media, nil
		}
		lastErr = errors.New("facebook html fallback found no direct downloads")
	}

	return nil, lastErr
}

func (c *Client) fetchHTMLSource(ctx context.Context, rawURL string) (string, string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, rawURL, nil)
	if err != nil {
		return "", rawURL, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8")
	req.Header.Set("Accept-Language", "en-US,en;q=0.9")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", rawURL, err
	}
	defer resp.Body.Close()

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return "", rawURL, err
	}

	finalURL := rawURL
	if resp.Request != nil && resp.Request.URL != nil {
		finalURL = resp.Request.URL.String()
	}

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", finalURL, fmt.Errorf("facebook html fallback failed with status %s", resp.Status)
	}

	return string(body), finalURL, nil
}

func parseFacebookHTMLFallback(html string) *fallbackMedia {
	patterns := []struct {
		key        string
		formatID   string
		resolution string
	}{
		{`browser_native_hd_url`, "hd", "hd"},
		{`playable_url_quality_hd`, "hd", "hd"},
		{`browser_native_sd_url`, "sd", "sd"},
		{`playable_url`, "sd", "sd"},
	}

	seen := map[string]bool{}
	downloads := make([]directMedia, 0, len(patterns))
	for _, pattern := range patterns {
		url := parseJSONStringField(html, pattern.key)
		if strings.TrimSpace(url) == "" || seen[url] {
			continue
		}
		seen[url] = true
		downloads = append(downloads, directMedia{
			FormatID:   pattern.formatID,
			URL:        url,
			Resolution: pattern.resolution,
			Ext:        "mp4",
			FormatNote: buildFormatNote(pattern.resolution, "mp4", pattern.resolution != ""),
		})
	}

	if len(downloads) == 0 {
		return nil
	}

	return &fallbackMedia{
		CanonicalURL: parseHTMLMetaContent(html, "og:url"),
		Info: VideoInfo{
			Title:        parseHTMLMetaContent(html, "og:title"),
			ThumbnailURL: parseHTMLMetaContent(html, "og:image"),
		},
		Downloads: downloads,
	}
}

func parseJSONStringField(input, field string) string {
	re := regexp.MustCompile(`"` + regexp.QuoteMeta(field) + `"\s*:\s*"((?:\\.|[^"\\])*)"`)
	matches := re.FindStringSubmatch(input)
	if len(matches) < 2 {
		return ""
	}

	value, err := strconv.Unquote(`"` + matches[1] + `"`)
	if err != nil {
		return ""
	}
	return strings.TrimSpace(value)
}

func parseHTMLMetaContent(input, property string) string {
	patterns := []*regexp.Regexp{
		regexp.MustCompile(`(?i)<meta[^>]+property=["']` + regexp.QuoteMeta(property) + `["'][^>]+content=["']([^"']+)["']`),
		regexp.MustCompile(`(?i)<meta[^>]+content=["']([^"']+)["'][^>]+property=["']` + regexp.QuoteMeta(property) + `["']`),
	}

	for _, pattern := range patterns {
		matches := pattern.FindStringSubmatch(input)
		if len(matches) >= 2 {
			return strings.TrimSpace(matches[1])
		}
	}

	return ""
}

func (c *Client) downloadFromFallback(ctx context.Context, rawURL, downloadPath, formatID string, media fallbackMedia) (string, bool, error) {
	if len(media.Downloads) > 0 {
		selected, err := pickDirectMedia(media.Downloads, formatID)
		if err != nil {
			return "", true, err
		}

		filePath, err := downloadDirectMedia(ctx, downloadPath, media.Info.Title, selected)
		return filePath, true, err
	}

	if media.CanonicalURL != "" && media.CanonicalURL != rawURL {
		outputTemplate := filepath.Join(downloadPath, "%(title)s.%(ext)s")
		if err := c.downloadWithYTDLP(ctx, media.CanonicalURL, outputTemplate, formatID); err != nil {
			return "", true, err
		}
		filePath, err := c.findDownloadedFile(downloadPath)
		return filePath, true, err
	}

	return "", false, nil
}

func (c *Client) findDownloadedFile(downloadPath string) (string, error) {
	matches, err := filepath.Glob(filepath.Join(downloadPath, "*"))
	if err != nil || len(matches) == 0 {
		return "", errors.New("download completed but no file was created")
	}

	filtered := matches[:0]
	for _, match := range matches {
		if filepath.Base(match) == fallbackManifestName {
			continue
		}
		filtered = append(filtered, match)
	}
	if len(filtered) == 0 {
		return "", errors.New("download completed but no file was created")
	}

	slices.Sort(filtered)
	return filtered[0], nil
}

func buildDownloadSelectors(rawURL, formatID string) (string, string) {
	isYouTube := IsYouTubeURL(rawURL)
	isReddit := IsRedditURL(rawURL)
	isInstagram := IsInstagramURL(rawURL)
	isTikTok := IsTikTokURL(rawURL)
	isTwitter := IsTwitterURL(rawURL)

	if formatID == "" || formatID == "best" {
		if isYouTube {
			return "bv*+ba/b", "b"
		}
		if isReddit {
			return "", "b"
		}
		if isInstagram || isTikTok || isTwitter {
			return "bv*+ba/b", "b"
		}
		return "best", ""
	}

	if isYouTube {
		return formatID + "+ba/" + formatID + "/bv*+ba/b", "bv*+ba/b"
	}

	if isReddit {
		return formatID + "+bestaudio/" + formatID, ""
	}

	if isInstagram || isTikTok || isTwitter {
		return formatID + "+ba/" + formatID + "/bv*+ba/b", "bv*+ba/b"
	}

	return formatID + "/best", "best"
}

func normalizeFormats(input []ytdlpFormat) []Format {
	seen := map[string]bool{}
	var out []Format

	for _, format := range input {
		if format.VCodec == "" || format.VCodec == "none" {
			continue
		}
		if strings.Contains(format.Protocol, "m3u8") || strings.Contains(format.Protocol, "dash") || strings.Contains(format.Protocol, "http_dash_segments") {
			continue
		}

		noteLower := strings.ToLower(format.FormatNote)
		if strings.Contains(noteLower, "dash") || strings.Contains(noteLower, "hls") || strings.Contains(noteLower, "fragment") {
			continue
		}
		if !slices.Contains([]string{"mp4", "mov", "avi", "mkv"}, format.Ext) {
			continue
		}

		resolution := format.Resolution
		if format.Width != nil && format.Height != nil {
			resolution = fmt.Sprintf("%dx%d", *format.Width, *format.Height)
		}
		if resolution == "" {
			resolution = fallback(format.FormatNote, "unknown")
		}
		if strings.EqualFold(resolution, "audio only") || seen[resolution] {
			continue
		}

		seen[resolution] = true
		out = append(out, Format{
			FormatID:   fallback(format.FormatID, "best"),
			Resolution: resolution,
			Ext:        fallback(format.Ext, "mp4"),
			Filesize:   format.Filesize,
			FormatNote: buildFormatNote(resolution, format.Ext, format.Height != nil),
		})
	}

	return out
}

func buildFormatNote(resolution, ext string, hasHeight bool) string {
	if hasHeight {
		return fmt.Sprintf("%s - %s", resolution, strings.ToUpper(ext))
	}
	return fmt.Sprintf("Standard quality - %s", strings.ToUpper(fallback(ext, "mp4")))
}

func friendlyYTDLPError(rawURL string, err error) error {
	if err == nil {
		return errors.New("video processing failed")
	}

	message := err.Error()
	switch {
	case strings.Contains(message, "Cannot parse data"), strings.Contains(strings.ToLower(message), "unable to extract"):
		return errors.New(providerExtractionError(rawURL))
	case IsFacebookURL(rawURL) && strings.Contains(message, "Unsupported URL"):
		return errors.New(providerExtractionError(rawURL))
	case strings.Contains(message, "Private video"), strings.Contains(strings.ToLower(message), "login"):
		return errors.New("This video appears to be private or requires login. Only public posts or videos can be downloaded.")
	default:
		return fmt.Errorf("could not process video: %s", compactCommandError(message))
	}
}

func classifyProcessingError(rawURL string, err error) error {
	if err == nil {
		return errors.New("video processing failed")
	}

	message := err.Error()
	if strings.Contains(message, "yt-dlp") || strings.Contains(message, "Unsupported URL") || strings.Contains(strings.ToLower(message), "unable to extract") {
		return friendlyYTDLPError(rawURL, err)
	}

	return err
}

func providerExtractionError(rawURL string) string {
	switch {
	case IsFacebookURL(rawURL):
		return "Unable to download this Facebook video.\n" +
			"1. For Reels: open the link in your browser and copy the full reel URL.\n" +
			"2. For regular videos: try the direct video URL instead of a share link.\n" +
			"3. Some videos are restricted and cannot be downloaded."
	case IsInstagramURL(rawURL):
		return "Unable to download this Instagram post.\n" +
			"1. Use the full post, reel, or story URL instead of a shortened share link.\n" +
			"2. Only public Instagram content is supported.\n" +
			"3. Some posts are restricted and cannot be downloaded."
	case IsTikTokURL(rawURL):
		return "Unable to download this TikTok post.\n" +
			"1. Use the full TikTok video URL instead of a share redirect when possible.\n" +
			"2. Only public TikTok content is supported.\n" +
			"3. Some posts are restricted and cannot be downloaded."
	case IsTwitterURL(rawURL):
		return "Unable to download this Twitter/X post.\n" +
			"1. Use the full tweet or x.com status URL instead of a share redirect when possible.\n" +
			"2. Only public Twitter/X content is supported.\n" +
			"3. Some posts are restricted and cannot be downloaded."
	default:
		return "Unable to download this video because the provider data could not be extracted."
	}
}

func compactCommandError(message string) string {
	re := regexp.MustCompile(`\s+`)
	return strings.TrimSpace(re.ReplaceAllString(message, " "))
}

func fallback(value, defaultValue string) string {
	if strings.TrimSpace(value) == "" {
		return defaultValue
	}
	return value
}

func randomID() (string, error) {
	buf := make([]byte, 16)
	if _, err := rand.Read(buf); err != nil {
		return "", err
	}
	return hex.EncodeToString(buf), nil
}

func mergeFallbackInfo(base, override VideoInfo) VideoInfo {
	if strings.TrimSpace(base.ThumbnailURL) == "" {
		base.ThumbnailURL = override.ThumbnailURL
	}
	if strings.TrimSpace(base.Title) == "" {
		base.Title = override.Title
	}
	if base.Duration == nil {
		base.Duration = override.Duration
	}
	if len(base.Formats) == 0 {
		base.Formats = override.Formats
	}
	return base
}

func formatsFromDirectMedia(downloads []directMedia) []Format {
	if len(downloads) == 0 {
		return nil
	}

	formats := make([]Format, 0, len(downloads))
	for _, download := range downloads {
		formats = append(formats, Format{
			FormatID:   fallback(download.FormatID, "best"),
			Resolution: fallback(download.Resolution, "best"),
			Ext:        fallback(download.Ext, "mp4"),
			Filesize:   download.Filesize,
			FormatNote: fallback(download.FormatNote, buildFormatNote(download.Resolution, download.Ext, download.Resolution != "")),
		})
	}
	return formats
}

func pickDirectMedia(downloads []directMedia, formatID string) (directMedia, error) {
	if len(downloads) == 0 {
		return directMedia{}, errors.New("no direct downloads are available")
	}

	if formatID == "" || formatID == "best" {
		return downloads[0], nil
	}

	for _, download := range downloads {
		if download.FormatID == formatID {
			return download, nil
		}
	}

	return directMedia{}, fmt.Errorf("requested format %q is not available", formatID)
}

func downloadDirectMedia(ctx context.Context, downloadPath, title string, media directMedia) (string, error) {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, media.URL, nil)
	if err != nil {
		return "", err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36")

	resp, err := http.DefaultClient.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()

	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return "", fmt.Errorf("direct media download failed with status %s", resp.Status)
	}

	fileName := sanitizeFilename(fallback(title, "Video")) + "." + fallback(media.Ext, "mp4")
	filePath := filepath.Join(downloadPath, fileName)
	file, err := os.Create(filePath)
	if err != nil {
		return "", err
	}
	defer file.Close()

	if _, err := io.Copy(file, resp.Body); err != nil {
		return "", err
	}

	return filePath, nil
}

func sanitizeFilename(name string) string {
	name = regexp.MustCompile(`[<>:"/\\|?*\x00-\x1f]`).ReplaceAllString(name, "_")
	name = strings.TrimSpace(name)
	if name == "" {
		return "video"
	}
	return name
}

func writeFallbackManifest(downloadPath string, media fallbackMedia) error {
	data, err := json.Marshal(media)
	if err != nil {
		return fmt.Errorf("marshal fallback media: %w", err)
	}

	manifestPath := filepath.Join(downloadPath, fallbackManifestName)
	if err := os.WriteFile(manifestPath, data, 0o644); err != nil {
		return fmt.Errorf("write fallback manifest: %w", err)
	}

	return nil
}

func readFallbackManifest(downloadPath string) (fallbackMedia, error) {
	manifestPath := filepath.Join(downloadPath, fallbackManifestName)
	data, err := os.ReadFile(manifestPath)
	if err != nil {
		return fallbackMedia{}, err
	}

	var media fallbackMedia
	if err := json.Unmarshal(data, &media); err != nil {
		return fallbackMedia{}, fmt.Errorf("parse fallback manifest: %w", err)
	}

	return media, nil
}
