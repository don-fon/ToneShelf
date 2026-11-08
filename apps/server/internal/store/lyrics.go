package store

import (
	"regexp"
	"strconv"
	"strings"
)

var offsetTagPattern = regexp.MustCompile("(?i)^\\[offset:([+-]?[0-9]+)\\]$")

func ensureLyricsOffset(lyrics string) string {
	if strings.TrimSpace(strings.TrimPrefix(lyrics, "\uFEFF")) == "" {
		return lyrics
	}
	for _, line := range strings.FieldsFunc(lyrics, func(char rune) bool { return char == '\r' || char == '\n' }) {
		line = strings.TrimSpace(strings.TrimPrefix(strings.TrimSpace(line), "\uFEFF"))
		match := offsetTagPattern.FindStringSubmatch(line)
		if match == nil {
			continue
		}
		value, err := strconv.ParseInt(match[1], 10, 64)
		if err == nil && value >= -9007199254740991 && value <= 9007199254740991 {
			return lyrics
		}
	}
	newline := "\n"
	if index := strings.IndexAny(lyrics, "\r\n"); index >= 0 {
		if lyrics[index] == '\r' {
			newline = "\r"
			if index+1 < len(lyrics) && lyrics[index+1] == '\n' {
				newline = "\r\n"
			}
		}
	}
	prefix := "[offset:0]" + newline
	if strings.HasPrefix(lyrics, "\uFEFF") {
		return "\uFEFF" + prefix + strings.TrimPrefix(lyrics, "\uFEFF")
	}
	return prefix + lyrics
}
