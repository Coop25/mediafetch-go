package mediafetch

import (
	"net/url"
	"strings"
)

var (
	facebookHosts  = []string{"facebook.com", "fb.com", "fb.watch", "m.facebook.com"}
	instagramHosts = []string{"instagram.com", "www.instagram.com", "m.instagram.com", "instagr.am"}
	tikTokHosts    = []string{"tiktok.com", "www.tiktok.com", "m.tiktok.com", "vm.tiktok.com", "vt.tiktok.com"}
	twitterHosts   = []string{"twitter.com", "www.twitter.com", "mobile.twitter.com", "x.com", "www.x.com", "mobile.x.com"}
	youTubeHosts   = []string{"youtube.com", "youtu.be", "m.youtube.com", "music.youtube.com"}
	redditHosts    = []string{"reddit.com", "www.reddit.com", "old.reddit.com", "redd.it", "v.redd.it"}
)

func ValidateSupportedURL(raw string) bool {
	return IsFacebookURL(raw) || IsInstagramURL(raw) || IsTikTokURL(raw) || IsTwitterURL(raw) || IsYouTubeURL(raw) || IsRedditURL(raw)
}

func IsFacebookURL(raw string) bool {
	return matchesHost(raw, facebookHosts)
}

func IsInstagramURL(raw string) bool {
	return matchesHost(raw, instagramHosts)
}

func IsTikTokURL(raw string) bool {
	return matchesHost(raw, tikTokHosts)
}

func IsTwitterURL(raw string) bool {
	return matchesHost(raw, twitterHosts)
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
