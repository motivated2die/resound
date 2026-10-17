package server

import "strings"

// DescribeAgent names a paired device by the browser's user agent, which
// the app sends when it pairs: "Chrome on Android", "Safari on iPhone",
// "Edge on Windows". Unknown parts are left out ("Android device"); with
// nothing known it is "Unknown device". Installed apps send their browser's
// user agent.
func DescribeAgent(ua string) string {
	has := func(s ...string) bool {
		for _, x := range s {
			if strings.Contains(ua, x) {
				return true
			}
		}
		return false
	}
	var system string
	switch {
	case has("iPhone", "iPod"):
		system = "iPhone"
	case has("iPad"):
		system = "iPad"
	case has("Android"):
		system = "Android"
	case has("CrOS"):
		system = "ChromeOS"
	case has("Windows"):
		system = "Windows"
	case has("Macintosh", "Mac OS X"): // also iPads asking for desktop sites
		system = "Mac"
	case has("Linux"):
		system = "Linux"
	}
	// Order matters: Edge, Opera and Samsung Internet say "Chrome" too, and
	// every browser on iOS says "Safari".
	var browser string
	switch {
	case has("Edg/", "EdgA/", "EdgiOS/"):
		browser = "Edge"
	case has("SamsungBrowser/"):
		browser = "Samsung Internet"
	case has("OPR/", "OPT/"):
		browser = "Opera"
	case has("Firefox/", "FxiOS/"):
		browser = "Firefox"
	case has("CriOS/", "Chrome/"):
		browser = "Chrome"
	case has("Safari/"):
		browser = "Safari"
	}
	switch {
	case browser != "" && system != "":
		return browser + " on " + system
	case system != "":
		return system + " device"
	case browser != "":
		return browser
	}
	return "Unknown device"
}
