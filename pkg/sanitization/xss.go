package sanitization

import (
	"html"
	"regexp"
	"strings"
)

var (
	htmlTagRegex   = regexp.MustCompile(`<[^>]*>`)
	urlRegex       = regexp.MustCompile(`https?://[^\s]+`)
	emailRegex     = regexp.MustCompile(`[a-zA-Z0-9._%+-]+@[a-zA-Z0-9.-]+\.[a-zA-Z]{2,}`)
	scriptTagRegex = regexp.MustCompile(`(?i)<script[^>]*>.*?</script>`)
	jsEventRegex   = regexp.MustCompile(`(?i)\bon\w+\s*=`)
)

func SanitizeHTML(input string) string {
	input = html.EscapeString(input)
	input = scriptTagRegex.ReplaceAllString(input, "")
	input = jsEventRegex.ReplaceAllString(input, "")
	return input
}

func StripHTML(input string) string {
	return htmlTagRegex.ReplaceAllString(input, "")
}

func SanitizeMessage(input string) string {
	input = strings.TrimSpace(input)
	input = html.EscapeString(input)
	input = scriptTagRegex.ReplaceAllString(input, "")
	input = jsEventRegex.ReplaceAllString(input, "")
	input = strings.ReplaceAll(input, "\x00", "")
	return input
}

func SanitizeUsername(input string) string {
	input = strings.TrimSpace(input)
	input = html.EscapeString(input)
	input = strings.ReplaceAll(input, "\x00", "")
	return input
}

func ValidateAndSanitizeURL(url string) string {
	if !urlRegex.MatchString(url) {
		return ""
	}
	return url
}

func MaskEmail(email string) string {
	parts := strings.Split(email, "@")
	if len(parts) != 2 {
		return email
	}
	username := parts[0]
	length := len(username)
	if length <= 2 {
		return "**@" + parts[1]
	}
	return string(username[0]) + strings.Repeat("*", length-2) + string(username[length-1]) + "@" + parts[1]
}
