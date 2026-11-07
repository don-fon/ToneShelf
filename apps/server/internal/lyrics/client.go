package lyrics

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"
)

const defaultUserAgent = "MusicPlayer/0.1 (private self-hosted music player)"

var (
	qualitySuffix = regexp.MustCompile(`(?i)(?:[\s_-]+(?:h|l|hq|sq|320k|lossless))$`)
	aliasAtEnd    = regexp.MustCompile(`\s*[\(（]([^\)）]+)[\)）]\s*$`)
	nonWord       = regexp.MustCompile(`[\p{P}\p{Z}\p{S}_]+`)
)

type SearchInput struct {
	Title, Artist, Album string
	DurationSeconds      float64
}

type Candidate struct {
	ID         int64   `json:"id"`
	TrackName  string  `json:"trackName"`
	ArtistName string  `json:"artistName"`
	AlbumName  string  `json:"albumName"`
	Duration   float64 `json:"duration"`
	Synced     bool    `json:"synced"`
	Lyrics     string  `json:"lyrics"`
	Score      float64 `json:"score"`
}

type SearchResult struct {
	QueryTitle  string      `json:"queryTitle"`
	QueryArtist string      `json:"queryArtist"`
	Candidates  []Candidate `json:"candidates"`
}

type Client struct {
	BaseURL, UserAgent string
	HTTP               *http.Client
}

type record struct {
	ID           int64   `json:"id"`
	TrackName    string  `json:"trackName"`
	ArtistName   string  `json:"artistName"`
	AlbumName    string  `json:"albumName"`
	Duration     float64 `json:"duration"`
	Instrumental bool    `json:"instrumental"`
	PlainLyrics  *string `json:"plainLyrics"`
	SyncedLyrics *string `json:"syncedLyrics"`
}

func New(baseURL string) *Client {
	return &Client{BaseURL: strings.TrimRight(baseURL, "/"), UserAgent: defaultUserAgent, HTTP: &http.Client{Timeout: 15 * time.Second}}
}

func (c *Client) Search(ctx context.Context, input SearchInput) (SearchResult, error) {
	title, artist := searchTerms(input.Title, input.Artist)
	result := SearchResult{QueryTitle: title, QueryArtist: artist, Candidates: make([]Candidate, 0)}
	if title == "" {
		return result, errors.New("歌曲标题为空")
	}
	endpoint, err := url.Parse(c.BaseURL + "/api/search")
	if err != nil {
		return result, err
	}
	query := endpoint.Query()
	query.Set("track_name", title)
	if artist != "" {
		query.Set("artist_name", artist)
	}
	endpoint.RawQuery = query.Encode()

	var records []record
	if err = c.getJSON(ctx, endpoint.String(), &records, true); err != nil {
		return result, err
	}
	for _, item := range records {
		lyricsText, synced := preferredLyrics(item)
		if item.Instrumental || lyricsText == "" {
			continue
		}
		result.Candidates = append(result.Candidates, Candidate{
			ID: item.ID, TrackName: item.TrackName, ArtistName: item.ArtistName,
			AlbumName: item.AlbumName, Duration: item.Duration, Synced: synced,
			Lyrics: lyricsText, Score: score(input, title, artist, item, synced),
		})
	}
	sort.SliceStable(result.Candidates, func(i, j int) bool {
		return result.Candidates[i].Score > result.Candidates[j].Score
	})
	result.Candidates = uniqueCandidates(result.Candidates)
	if len(result.Candidates) > 8 {
		result.Candidates = result.Candidates[:8]
	}
	return result, nil
}

func uniqueCandidates(candidates []Candidate) []Candidate {
	unique := make([]Candidate, 0, len(candidates))
	seen := make(map[string]struct{}, len(candidates))
	for _, candidate := range candidates {
		key := strings.Join([]string{
			normalize(candidate.TrackName), normalize(candidate.ArtistName),
			strconv.FormatInt(int64(math.Floor(candidate.Duration)), 10), strconv.FormatBool(candidate.Synced),
		}, "|")
		if _, exists := seen[key]; exists {
			continue
		}
		seen[key] = struct{}{}
		unique = append(unique, candidate)
	}
	return unique
}

func (c *Client) getJSON(ctx context.Context, endpoint string, target any, retry bool) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, endpoint, nil)
	if err != nil {
		return err
	}
	req.Header.Set("User-Agent", c.UserAgent)
	response, err := c.HTTP.Do(req)
	if err != nil {
		return fmt.Errorf("连接歌词服务失败: %w", err)
	}
	defer response.Body.Close()
	if response.StatusCode == http.StatusTooManyRequests && retry {
		delay := retryDelay(response.Header.Get("Retry-After"))
		response.Body.Close()
		timer := time.NewTimer(delay)
		defer timer.Stop()
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-timer.C:
			return c.getJSON(ctx, endpoint, target, false)
		}
	}
	if response.StatusCode != http.StatusOK {
		_, _ = io.Copy(io.Discard, io.LimitReader(response.Body, 1<<20))
		return fmt.Errorf("歌词服务返回 HTTP %d", response.StatusCode)
	}
	decoder := json.NewDecoder(io.LimitReader(response.Body, 8<<20))
	if err = decoder.Decode(target); err != nil {
		return fmt.Errorf("解析歌词响应失败: %w", err)
	}
	return nil
}

func searchTerms(title, artist string) (string, string) {
	title = strings.TrimSpace(qualitySuffix.ReplaceAllString(title, ""))
	artist = strings.TrimSpace(artist)
	if artist == "" {
		if before, after, found := strings.Cut(title, " - "); found {
			artist = strings.TrimSpace(before)
			title = strings.TrimSpace(after)
			if first, _, found := strings.Cut(artist, " _ "); found {
				artist = strings.TrimSpace(first)
			}
		}
	}
	if match := aliasAtEnd.FindStringSubmatch(title); len(match) == 2 {
		if primary := strings.TrimSpace(aliasAtEnd.ReplaceAllString(title, "")); primary != "" {
			title = primary
		}
	}
	return title, artist
}

func preferredLyrics(item record) (string, bool) {
	if item.SyncedLyrics != nil && strings.TrimSpace(*item.SyncedLyrics) != "" {
		return *item.SyncedLyrics, true
	}
	if item.PlainLyrics != nil {
		return strings.TrimSpace(*item.PlainLyrics), false
	}
	return "", false
}

func score(input SearchInput, title, artist string, item record, synced bool) float64 {
	value := textScore(title, item.TrackName, 55)
	if artist != "" {
		value += textScore(artist, item.ArtistName, 25)
	}
	if input.Album != "" {
		value += textScore(input.Album, item.AlbumName, 8)
	}
	if input.DurationSeconds > 0 && item.Duration > 0 {
		difference := math.Abs(input.DurationSeconds - item.Duration)
		switch {
		case difference <= 3:
			value += 30 - difference*4
		case difference <= 10:
			value += 10 - difference
		default:
			value -= math.Min(20, difference/5)
		}
	}
	if synced {
		value += 6
	}
	return math.Round(value*10) / 10
}

func textScore(expected, actual string, maximum float64) float64 {
	expected, actual = normalize(expected), normalize(actual)
	if expected == "" || actual == "" {
		return 0
	}
	if expected == actual {
		return maximum
	}
	if strings.Contains(actual, expected) || strings.Contains(expected, actual) {
		return maximum * 0.8
	}
	return 0
}

func normalize(value string) string {
	value = strings.ToLower(value)
	value = strings.ReplaceAll(value, "初音未来", "初音ミク")
	return nonWord.ReplaceAllString(value, "")
}

func retryDelay(value string) time.Duration {
	seconds, err := strconv.Atoi(value)
	if err != nil || seconds < 1 {
		return time.Second
	}
	if seconds > 5 {
		seconds = 5
	}
	return time.Duration(seconds) * time.Second
}
