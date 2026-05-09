package mediafetch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"
)

type Client struct {
	downloadDir string
	ytdlpBinary string
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

	var lastErr error
	for _, args := range buildInfoStrategies(rawURL) {
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

		return downloadID, VideoInfo{
			ThumbnailURL: info.Thumbnail,
			Title:        fallback(info.Title, "Video"),
			Duration:     duration,
			Formats:      formats,
		}, nil
	}

	if shouldRetryWithResolvedURL(rawURL, lastErr) {
		resolvedURL := c.resolveMediaURL(ctx, rawURL)
		if resolvedURL != rawURL {
			for _, args := range buildInfoStrategies(resolvedURL) {
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

				return downloadID, VideoInfo{
					ThumbnailURL: info.Thumbnail,
					Title:        fallback(info.Title, "Video"),
					Duration:     duration,
					Formats:      formats,
				}, nil
			}
		}
	}

	_ = os.RemoveAll(downloadPath)
	return "", VideoInfo{}, friendlyYTDLPError(rawURL, lastErr)
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

	outputTemplate := filepath.Join(downloadPath, "%(title)s.%(ext)s")
	var lastErr error
	primarySelector, fallbackSelector := buildDownloadSelectors(rawURL, formatID)
	args := buildDownloadArgs(rawURL, outputTemplate, primarySelector)
	if _, err := runCommand(ctx, c.ytdlpBinary, args...); err != nil {
		lastErr = err
		if strings.Contains(err.Error(), "Requested format is not available") && fallbackSelector != primarySelector {
			retryArgs := buildDownloadArgs(rawURL, outputTemplate, fallbackSelector)
			if _, retryErr := runCommand(ctx, c.ytdlpBinary, retryArgs...); retryErr == nil {
				lastErr = nil
			} else {
				lastErr = retryErr
			}
		}
	} else {
		lastErr = nil
	}

	if lastErr != nil && shouldRetryWithResolvedURL(rawURL, lastErr) {
		resolvedURL := c.resolveMediaURL(ctx, rawURL)
		if resolvedURL != rawURL {
			primarySelector, fallbackSelector = buildDownloadSelectors(resolvedURL, formatID)
			args = buildDownloadArgs(resolvedURL, outputTemplate, primarySelector)
			if _, err := runCommand(ctx, c.ytdlpBinary, args...); err != nil {
				lastErr = err
				if strings.Contains(err.Error(), "Requested format is not available") && fallbackSelector != primarySelector {
					retryArgs := buildDownloadArgs(resolvedURL, outputTemplate, fallbackSelector)
					if _, retryErr := runCommand(ctx, c.ytdlpBinary, retryArgs...); retryErr == nil {
						lastErr = nil
					} else {
						lastErr = retryErr
					}
				}
			} else {
				lastErr = nil
			}
		}
	}

	if lastErr == nil {
		matches, err := filepath.Glob(filepath.Join(downloadPath, "*"))
		if err != nil || len(matches) == 0 {
			return "", errors.New("download completed but no file was created")
		}

		slices.Sort(matches)
		return matches[0], nil
	}

	return "", friendlyYTDLPError(rawURL, lastErr)
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

func buildDownloadArgs(rawURL, outputTemplate, selector string) []string {
	args := []string{
		"-o", outputTemplate,
		"--no-playlist",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
	}

	if IsYouTubeURL(rawURL) || IsRedditURL(rawURL) || IsInstagramURL(rawURL) || IsTikTokURL(rawURL) || IsTwitterURL(rawURL) {
		args = append(args, "--merge-output-format", "mp4")
	}

	if selector != "" {
		args = append(args, "-f", selector)
	}

	return append(args, rawURL)
}

func buildInfoStrategies(rawURL string) [][]string {
	if !IsFacebookURL(rawURL) {
		return [][]string{
			{
				"--dump-single-json",
				"--no-warnings",
				"--skip-download",
				"--no-playlist",
				rawURL,
			},
		}
	}

	return [][]string{
		{
			"--dump-single-json",
			"--no-warnings",
			"--skip-download",
			"--no-playlist",
			"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
			"--add-header", "Accept:text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
			"--add-header", "Accept-Language:en-us,en;q=0.5",
			rawURL,
		},
		{
			"--dump-single-json",
			"--no-warnings",
			"--skip-download",
			"--no-playlist",
			"--extractor-args", "facebook:skip=dash",
			"--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
			rawURL,
		},
		{
			"--dump-single-json",
			"--no-warnings",
			"--skip-download",
			"--no-playlist",
			"--user-agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
			rawURL,
		},
	}
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
