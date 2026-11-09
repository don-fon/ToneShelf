package artwork

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
	"strings"
	"sync"
	"time"
	"unicode"
)

const MaxImageBytes int64 = 8 * 1024 * 1024
const userAgent = "MusicPlayer/0.1 (private self-hosted music player; cover lookup)"

type Input struct {
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	Album           string  `json:"album"`
	DurationSeconds float64 `json:"-"`
}
type Candidate struct {
	ID              string  `json:"id"`
	Title           string  `json:"title"`
	Artist          string  `json:"artist"`
	Date            string  `json:"date,omitempty"`
	Country         string  `json:"country,omitempty"`
	Version         string  `json:"version,omitempty"`
	DurationSeconds float64 `json:"durationSeconds,omitempty"`
	Score           float64 `json:"score"`
	group           string
}
type Result struct {
	QueryTitle  string      `json:"queryTitle"`
	QueryArtist string      `json:"queryArtist"`
	Candidates  []Candidate `json:"candidates"`
	Expanded    bool        `json:"expanded"`
	Partial     bool        `json:"partial"`
}
type cachedImage struct {
	url     string
	expires time.Time
}
type Client struct {
	musicBrainz, archive string
	http                 *http.Client
	interval             time.Duration
	mu                   sync.Mutex
	next                 map[string]time.Time
	images               map[string]cachedImage
}

func New() *Client {
	return NewWithEndpoints("https://musicbrainz.org", "https://coverartarchive.org", 1100*time.Millisecond)
}
func NewWithEndpoints(musicBrainz, archive string, interval time.Duration) *Client {
	c := &Client{musicBrainz: strings.TrimRight(musicBrainz, "/"), archive: strings.TrimRight(archive, "/"), interval: interval, next: map[string]time.Time{}, images: map[string]cachedImage{}}
	c.http = &http.Client{Timeout: 20 * time.Second, CheckRedirect: func(r *http.Request, via []*http.Request) error {
		if len(via) >= 6 {
			return errors.New("too many cover redirects")
		}
		return c.allowed(r.URL)
	}}
	return c
}

var uuid = regexp.MustCompile(`^[0-9a-fA-F]{8}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{4}-[0-9a-fA-F]{12}$`)

func ValidID(id string) bool { return uuid.MatchString(id) }
func (c *Client) allowed(u *url.URL) error {
	for _, base := range []string{c.musicBrainz, c.archive} {
		b, _ := url.Parse(base)
		if u.Scheme == b.Scheme && u.Host == b.Host && u.User == nil {
			return nil
		}
	}
	if u.Scheme == "https" && u.Port() == "" && u.User == nil && (u.Hostname() == "archive.org" || strings.HasSuffix(u.Hostname(), ".archive.org")) {
		return nil
	}
	return errors.New("untrusted cover image address")
}
func (c *Client) get(ctx context.Context, address string) (*http.Response, error) {
	u, err := url.Parse(address)
	if err != nil {
		return nil, err
	}
	// CAA's historical metadata still contains HTTP image links.
	if u.Scheme == "http" && u.Host == "coverartarchive.org" {
		u.Scheme = "https"
	}
	if err = c.allowed(u); err != nil {
		return nil, err
	}
	for attempt := 0; attempt < 2; attempt++ {
		c.mu.Lock()
		at := c.next[u.Host]
		if at.Before(time.Now()) {
			at = time.Now()
		}
		c.next[u.Host] = at.Add(c.interval)
		c.mu.Unlock()
		timer := time.NewTimer(time.Until(at))
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, u.String(), nil)
		if err != nil {
			return nil, err
		}
		req.Header.Set("User-Agent", userAgent)
		response, err := c.http.Do(req)
		if err != nil {
			return nil, fmt.Errorf("封面服务连接失败: %w", err)
		}
		if (response.StatusCode == 429 || response.StatusCode == 503) && attempt == 0 {
			response.Body.Close()
			timer = time.NewTimer(2 * time.Second)
			select {
			case <-ctx.Done():
				timer.Stop()
				return nil, ctx.Err()
			case <-timer.C:
			}
			continue
		}
		return response, nil
	}
	return nil, errors.New("cover request failed")
}
func (c *Client) json(ctx context.Context, address string, target any) (bool, error) {
	response, err := c.get(ctx, address)
	if err != nil {
		return false, err
	}
	defer response.Body.Close()
	if response.StatusCode == 404 {
		return false, nil
	}
	if response.StatusCode != 200 {
		return false, fmt.Errorf("封面服务暂不可用（HTTP %d），请稍后重试", response.StatusCode)
	}
	if err = json.NewDecoder(io.LimitReader(response.Body, 2*1024*1024)).Decode(target); err != nil {
		return false, fmt.Errorf("封面服务响应无效: %w", err)
	}
	return true, nil
}
func (c *Client) front(ctx context.Context, entity, id string) (string, error) {
	key := entity + "/" + id
	c.mu.Lock()
	cached, ok := c.images[key]
	c.mu.Unlock()
	if ok && cached.expires.After(time.Now()) {
		return cached.url, nil
	}
	var data struct {
		Images []struct {
			Front      bool              `json:"front"`
			Image      string            `json:"image"`
			Thumbnails map[string]string `json:"thumbnails"`
		} `json:"images"`
	}
	found, err := c.json(ctx, c.archive+"/"+key, &data)
	if err != nil {
		return "", err
	}
	image := ""
	if found {
		for _, item := range data.Images {
			if item.Front {
				image = item.Thumbnails["500"]
				if image == "" {
					image = item.Image
				}
				break
			}
		}
	}
	if image != "" {
		u, err := url.Parse(image)
		if err != nil {
			return "", err
		}
		if u.Scheme == "http" && u.Host == "coverartarchive.org" {
			u.Scheme = "https"
			image = u.String()
		}
		if err = c.allowed(u); err != nil {
			return "", err
		}
	}
	c.mu.Lock()
	if len(c.images) >= 128 {
		c.images = map[string]cachedImage{}
	}
	c.images[key] = cachedImage{image, time.Now().Add(10 * time.Minute)}
	c.mu.Unlock()
	return image, nil
}
func (c *Client) image(ctx context.Context, id string) (string, error) {
	if !ValidID(id) {
		return "", errors.New("无效的发行版本 ID")
	}
	image, err := c.front(ctx, "release", id)
	if err != nil || image != "" {
		return image, err
	}
	var release struct {
		Group struct {
			ID string `json:"id"`
		} `json:"release-group"`
	}
	found, err := c.json(ctx, c.musicBrainz+"/ws/2/release/"+id+"?inc=release-groups&fmt=json", &release)
	if err != nil || !found || !ValidID(release.Group.ID) {
		return "", err
	}
	return c.front(ctx, "release-group", release.Group.ID)
}
func (c *Client) Download(ctx context.Context, id string, writer io.Writer) error {
	image, err := c.image(ctx, id)
	if err != nil {
		return err
	}
	if image == "" {
		return errors.New("该发行版本没有可用的正面封面")
	}
	response, err := c.get(ctx, image)
	if err != nil {
		return err
	}
	defer response.Body.Close()
	if response.StatusCode != 200 {
		return fmt.Errorf("封面图片下载失败（HTTP %d）", response.StatusCode)
	}
	if response.ContentLength > MaxImageBytes {
		return errors.New("封面图片超过 8 MiB")
	}
	size, err := io.Copy(writer, io.LimitReader(response.Body, MaxImageBytes+1))
	if err != nil {
		return err
	}
	if size > MaxImageBytes {
		return errors.New("封面图片超过 8 MiB")
	}
	return nil
}

type credit struct {
	Name   string `json:"name"`
	Artist struct {
		Name string `json:"name"`
	} `json:"artist"`
}
type release struct {
	ID      string   `json:"id"`
	Title   string   `json:"title"`
	Date    string   `json:"date"`
	Country string   `json:"country"`
	Version string   `json:"disambiguation"`
	Credit  []credit `json:"artist-credit"`
	Group   struct {
		ID string `json:"id"`
	} `json:"release-group"`
}
type recording struct {
	Title    string    `json:"title"`
	Length   float64   `json:"length"`
	Version  string    `json:"disambiguation"`
	Credit   []credit  `json:"artist-credit"`
	Releases []release `json:"releases"`
}

func artist(credits []credit) string {
	names := []string{}
	for _, item := range credits {
		if item.Name != "" {
			names = append(names, item.Name)
		} else {
			names = append(names, item.Artist.Name)
		}
	}
	return strings.Join(names, " / ")
}
func normalize(value string) string {
	return strings.Map(func(r rune) rune {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			return unicode.ToLower(r)
		}
		return -1
	}, value)
}
func similarity(a, b string) float64 {
	a, b = normalize(a), normalize(b)
	if a == "" || b == "" {
		return 0
	}
	if a == b {
		return 1
	}
	if strings.Contains(a, b) || strings.Contains(b, a) {
		return float64(min(len([]rune(a)), len([]rune(b)))) / float64(max(len([]rune(a)), len([]rune(b))))
	}
	return 0
}

var suffix = regexp.MustCompile(`(?i)\s*[（(](?:TV\s*size|TV\s*ver\.?)[）)]\s*$`)
var quality = regexp.MustCompile(`(?i)[_\s-]+(?:H|L|HQ|SQ|lossless)$`)
var quotedTitle = regexp.MustCompile(`「([^」]+)」`)
var filenameArtist = regexp.MustCompile(`(?:^|__)([A-Za-z][A-Za-z_ ]+)(?:[^A-Za-z_ ]|$)`)

func terms(input Input) (string, string, string) {
	title := strings.TrimSpace(input.Title)
	name := strings.TrimSpace(input.Artist)
	hint := ""
	if matches := quotedTitle.FindStringSubmatch(title); len(matches) > 1 {
		title = matches[1]
	}
	if index := strings.Index(title, " - "); index >= 0 {
		title = title[index+3:]
	}
	title = quality.ReplaceAllString(title, "")
	title = suffix.ReplaceAllString(title, "")
	if index := strings.Index(title, " ("); index >= 0 {
		title = strings.TrimSpace(title[:index])
	}
	if strings.Contains(input.Title, " - ") || strings.Contains(input.Title, "__") {
		if matches := filenameArtist.FindStringSubmatch(input.Title); len(matches) > 1 {
			hint = strings.Trim(strings.ReplaceAll(matches[1], "_", " "), " ")
		}
	}
	return title, name, hint
}
func quote(value string) string {
	return `"` + strings.NewReplacer(`\`, `\\`, `"`, `\"`).Replace(value) + `"`
}
func (c *Client) Search(ctx context.Context, input Input) (Result, error) {
	title, name, hint := terms(input)
	result := Result{QueryTitle: title, QueryArtist: name, Candidates: []Candidate{}}
	if title == "" && strings.TrimSpace(input.Album) == "" {
		return result, errors.New("请输入歌名或专辑名称")
	}
	candidates := map[string]Candidate{}
	var firstError error
	add := func(rel release, who, song, version string, duration float64) {
		if !ValidID(rel.ID) {
			return
		}
		if who == "" {
			who = artist(rel.Credit)
		}
		score := 40*similarity(name, who) + 25*similarity(input.Album, rel.Title) + 25*similarity(title, song)
		if duration > 0 && input.DurationSeconds > 0 {
			score += 10 * math.Max(0, 1-math.Abs(duration-input.DurationSeconds)/15)
		}
		item := Candidate{ID: rel.ID, Title: rel.Title, Artist: who, Date: rel.Date, Country: rel.Country, Version: strings.TrimSpace(rel.Version + " " + version), DurationSeconds: duration, Score: math.Round(score), group: rel.Group.ID}
		if old, exists := candidates[item.ID]; !exists || old.Score < item.Score {
			candidates[item.ID] = item
		}
	}
	search := func(entity, query string, target any) bool {
		address := c.musicBrainz + "/ws/2/" + entity + "/?" + url.Values{"query": {query}, "fmt": {"json"}, "limit": {"12"}}.Encode()
		_, err := c.json(ctx, address, target)
		if err != nil {
			result.Partial = true
			if firstError == nil {
				firstError = err
			}
			return false
		}
		return true
	}
	if input.Album != "" && name != "" {
		var data struct {
			Releases []release `json:"releases"`
		}
		if search("release", "release:"+quote(input.Album)+" AND artist:"+quote(name), &data) {
			for _, rel := range data.Releases {
				if similarity(input.Album, rel.Title) >= .65 && similarity(name, artist(rel.Credit)) >= .5 {
					add(rel, "", "", "", 0)
				}
			}
		}
	}
	if len(candidates) < 4 && title != "" {
		names := []string{name}
		if hint != "" && normalize(hint) != normalize(name) {
			names = append(names, hint)
		}
		for _, who := range names {
			if who == "" {
				continue
			}
			var data struct {
				Recordings []recording `json:"recordings"`
			}
			if search("recording", "recording:"+quote(title)+" AND artist:"+quote(who), &data) {
				for _, rec := range data.Recordings {
					for _, rel := range rec.Releases {
						add(rel, artist(rec.Credit), rec.Title, rec.Version, rec.Length/1000)
					}
				}
			}
			if len(candidates) >= 4 {
				break
			}
		}
	}
	if len(candidates) == 0 && title != "" {
		result.Expanded = true
		var data struct {
			Recordings []recording `json:"recordings"`
		}
		if search("recording", "recording:"+quote(title), &data) {
			for _, rec := range data.Recordings {
				if similarity(title, rec.Title) < .8 {
					continue
				}
				for _, rel := range rec.Releases {
					add(rel, artist(rec.Credit), rec.Title, rec.Version, rec.Length/1000)
				}
			}
		}
	}
	sorted := []Candidate{}
	for _, item := range candidates {
		sorted = append(sorted, item)
	}
	sort.Slice(sorted, func(i, j int) bool {
		if sorted[i].Score == sorted[j].Score {
			return sorted[i].ID < sorted[j].ID
		}
		return sorted[i].Score > sorted[j].Score
	})
	seen := map[string]bool{}
	for _, item := range sorted[:min(len(sorted), 8)] {
		image, err := c.front(ctx, "release", item.ID)
		if err == nil && image == "" && ValidID(item.group) {
			image, err = c.front(ctx, "release-group", item.group)
		}
		if err != nil {
			result.Partial = true
			if firstError == nil {
				firstError = err
			}
			continue
		}
		if image == "" || seen[image] {
			continue
		}
		seen[image] = true
		c.mu.Lock()
		c.images["release/"+item.ID] = cachedImage{image, time.Now().Add(10 * time.Minute)}
		c.mu.Unlock()
		result.Candidates = append(result.Candidates, item)
	}
	if len(result.Candidates) == 0 && firstError != nil {
		return result, firstError
	}
	return result, nil
}
