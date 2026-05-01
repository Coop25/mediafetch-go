package mediafetch

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"math"
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

	strategies := buildInfoStrategies(rawURL)

	var lastErr error
	for _, args := range strategies {
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

	_ = os.RemoveAll(downloadPath)
	return "", VideoInfo{}, friendlyYTDLPError(lastErr)
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
	args := []string{
		"-o", outputTemplate,
		"--no-playlist",
		"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
	}

	if IsYouTubeURL(rawURL) {
		args = append(args, "--merge-output-format", "mp4")
	}

	primarySelector, fallbackSelector := buildDownloadSelectors(rawURL, formatID)
	if primarySelector != "" {
		args = append(args, "-f", primarySelector)
	}
	args = append(args, rawURL)

	if _, err := runCommand(ctx, c.ytdlpBinary, args...); err != nil {
		if strings.Contains(err.Error(), "Requested format is not available") && fallbackSelector != "" && fallbackSelector != primarySelector {
			retryArgs := []string{
				"-o", outputTemplate,
				"--no-playlist",
				"--user-agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
			}
			if IsYouTubeURL(rawURL) {
				retryArgs = append(retryArgs, "--merge-output-format", "mp4")
			}
			retryArgs = append(retryArgs, "-f", fallbackSelector, rawURL)
			if _, retryErr := runCommand(ctx, c.ytdlpBinary, retryArgs...); retryErr != nil {
				return "", friendlyYTDLPError(retryErr)
			}
		} else {
			return "", friendlyYTDLPError(err)
		}
	}

	matches, err := filepath.Glob(filepath.Join(downloadPath, "*"))
	if err != nil || len(matches) == 0 {
		return "", errors.New("download completed but no file was created")
	}

	slices.Sort(matches)
	return matches[0], nil
}

func buildDownloadSelectors(rawURL, formatID string) (string, string) {
	isYouTube := IsYouTubeURL(rawURL)

	if formatID == "" || formatID == "best" {
		if isYouTube {
			return "bv*+ba/b", "b"
		}
		return "best", ""
	}

	if isYouTube {
		return formatID + "+ba/" + formatID + "/bv*+ba/b", "bv*+ba/b"
	}

	return formatID + "/best", "best"
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

func friendlyYTDLPError(err error) error {
	if err == nil {
		return errors.New("video processing failed")
	}

	message := err.Error()
	switch {
	case strings.Contains(message, "Cannot parse data"), strings.Contains(strings.ToLower(message), "unable to extract"):
		return errors.New(
			"Unable to download this video. Facebook has updated their security measures.\n" +
				"1. For Reels: open the link in your browser and copy the full reel URL.\n" +
				"2. For regular videos: try the direct video URL instead of a share link.\n" +
				"3. Some videos are restricted and cannot be downloaded.",
		)
	case strings.Contains(message, "Private video"), strings.Contains(strings.ToLower(message), "login"):
		return errors.New("This video appears to be private or requires login. Only public videos can be downloaded.")
	default:
		return fmt.Errorf("could not process video: %s", compactCommandError(message))
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
