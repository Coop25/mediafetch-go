package mediafetch

import (
	"path/filepath"
	"strings"
)

const defaultUserAgent = "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36"

func normalizeYTDLPSettings(settings YTDLPSettings) YTDLPSettings {
	settings.UserAgent = fallback(strings.TrimSpace(settings.UserAgent), defaultUserAgent)
	settings.CookiesFromBrowser = strings.TrimSpace(settings.CookiesFromBrowser)
	settings.CookiesFile = strings.TrimSpace(settings.CookiesFile)
	settings.Proxy = strings.TrimSpace(settings.Proxy)
	settings.ConfigLocation = strings.TrimSpace(settings.ConfigLocation)
	return settings
}

func buildInfoStrategies(rawURL string, settings YTDLPSettings) [][]string {
	base := buildInfoArgs(rawURL, settings)
	if !IsFacebookURL(rawURL) {
		return [][]string{base}
	}

	withHeaders := append(append([]string{}, base...),
		"--add-header", "Accept:text/html,application/xhtml+xml,application/xml;q=0.9,*/*;q=0.8",
		"--add-header", "Accept-Language:en-us,en;q=0.5",
	)
	withExtractorArgs := append(append([]string{}, base...),
		"--extractor-args", "facebook:skip=dash",
		"--user-agent", "Mozilla/5.0 (Macintosh; Intel Mac OS X 10_15_7) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/136.0.0.0 Safari/537.36",
	)
	withMobileUA := append(buildBaseYTDLPArgs(settings),
		"--dump-single-json",
		"--no-warnings",
		"--skip-download",
		"--no-playlist",
		"--user-agent", "Mozilla/5.0 (iPhone; CPU iPhone OS 18_0 like Mac OS X) AppleWebKit/605.1.15 (KHTML, like Gecko) Version/18.0 Mobile/15E148 Safari/604.1",
		rawURL,
	)

	return [][]string{withHeaders, withExtractorArgs, withMobileUA}
}

func buildInfoArgs(rawURL string, settings YTDLPSettings) []string {
	args := append(buildBaseYTDLPArgs(settings),
		"--dump-single-json",
		"--no-warnings",
		"--skip-download",
		"--no-playlist",
	)
	return append(args, rawURL)
}

func buildDownloadArgs(rawURL, outputTemplate, selector string, settings YTDLPSettings) []string {
	args := append(buildBaseYTDLPArgs(settings),
		"-o", outputTemplate,
		"--no-playlist",
	)

	if IsYouTubeURL(rawURL) || IsRedditURL(rawURL) || IsInstagramURL(rawURL) || IsTikTokURL(rawURL) || IsTwitterURL(rawURL) {
		args = append(args, "--merge-output-format", "mp4")
	}

	if selector != "" {
		args = append(args, "-f", selector)
	}

	return append(args, rawURL)
}

func buildBaseYTDLPArgs(settings YTDLPSettings) []string {
	args := []string{
		"--user-agent", fallback(settings.UserAgent, defaultUserAgent),
	}

	if settings.CookiesFromBrowser != "" {
		args = append(args, "--cookies-from-browser", settings.CookiesFromBrowser)
	}
	if settings.CookiesFile != "" {
		args = append(args, "--cookies", settings.CookiesFile)
	}
	if settings.Proxy != "" {
		args = append(args, "--proxy", settings.Proxy)
	}
	if settings.ConfigLocation != "" {
		args = append(args, "--config-location", filepath.Clean(settings.ConfigLocation))
	}

	return args
}
