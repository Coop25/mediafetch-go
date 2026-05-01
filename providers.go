package mediafetch

import (
	"net/url"
	"strings"
)

var (
	facebookHosts = []string{"facebook.com", "fb.com", "fb.watch", "m.facebook.com"}
	youTubeHosts  = []string{"youtube.com", "youtu.be", "m.youtube.com", "music.youtube.com"}
	redditHosts   = []string{"reddit.com", "www.reddit.com", "old.reddit.com", "redd.it", "v.redd.it"}
)

func ValidateSupportedURL(raw string) bool {
	return IsFacebookURL(raw) || IsYouTubeURL(raw) || IsRedditURL(raw)
}

func IsFacebookURL(raw string) bool {
	return matchesHost(raw, facebookHosts)
}

func IsYouTubeURL(raw string) bool {
	return matchesHost(raw, youTubeHosts)
}

func IsRedditURL(raw string) bool {
	return matchesHost(raw, redditHosts)
}

func matchesHost(raw string, patterns []string) bool {
	if raw == "" {
		return false
	}

	u, err := url.Parse(raw)
	if err != nil {
		return false
	}

	host := strings.ToLower(u.Hostname())
	for _, pattern := range patterns {
		if host == pattern || strings.HasSuffix(host, "."+pattern) || strings.Contains(host, pattern) {
			return true
		}
	}
	return false
}
