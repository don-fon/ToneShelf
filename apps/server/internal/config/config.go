package config

import (
	"encoding/json"
	"errors"
	"flag"
	"fmt"
	"io"
	"net"
	"net/url"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"strings"
)

type Config struct {
	Addr         string `json:"addr"`
	DataDir      string `json:"dataDir"`
	Database     string `json:"database"`
	WebDir       string `json:"webDir"`
	QuotaBytes   int64  `json:"quotaBytes"`
	FFmpeg       string `json:"ffmpeg"`
	FFprobe      string `json:"ffprobe"`
	LogLevel     string `json:"logLevel"`
	LRCLIBURL    string `json:"lrclibUrl"`
	ConfigFile   string `json:"-"`
	DatabasePath string `json:"-"`
}

type Environment struct {
	OS, CWD, Executable string
	Getenv              func(string) string
}

func Load(args []string) (Config, error) {
	cwd, err := os.Getwd()
	if err != nil {
		return Config{}, err
	}
	executable, err := os.Executable()
	if err != nil {
		return Config{}, err
	}
	return LoadWith(args, Environment{OS: runtime.GOOS, CWD: cwd, Executable: executable, Getenv: os.Getenv})
}

func LoadWith(args []string, env Environment) (Config, error) {
	cfg := Config{Addr: "127.0.0.1:8080", Database: "data/music.db", QuotaBytes: 10 * 1024 * 1024 * 1024, FFmpeg: "ffmpeg", FFprobe: "ffprobe", LogLevel: "info", LRCLIBURL: "https://lrclib.net"}
	dataRoot, defaultConfig, err := platformPaths(env.OS, env.Getenv("LOCALAPPDATA"))
	if err != nil {
		return cfg, err
	}
	cfg.DataDir = dataRoot
	flags := flag.NewFlagSet("music-player", flag.ContinueOnError)
	configFile := flags.String("config", "", "configuration file (default: "+defaultConfig+")")
	values := map[string]*string{}
	for _, name := range []string{"addr", "data-dir", "database", "web-dir", "quota-bytes", "ffmpeg", "ffprobe", "log-level", "lrclib-url"} {
		values[name] = flags.String(name, "", "override "+name)
	}
	if err := flags.Parse(args); err != nil {
		return cfg, err
	}
	if flags.NArg() != 0 {
		return cfg, fmt.Errorf("unexpected argument: %s", flags.Arg(0))
	}
	selected := env.Getenv("MUSIC_CONFIG")
	explicit := selected != ""
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "config" {
			selected = *configFile
			explicit = true
		}
	})
	if explicit && selected == "" {
		return cfg, errors.New("config path must not be empty")
	}
	if !explicit {
		selected = defaultConfig
	}
	cfg.ConfigFile = absolute(env.CWD, selected)
	data, err := os.ReadFile(cfg.ConfigFile)
	if err != nil && !(errors.Is(err, os.ErrNotExist) && !explicit) {
		return cfg, fmt.Errorf("read config: %w", err)
	}
	if err == nil {
		if !strings.HasPrefix(strings.TrimSpace(string(data)), "{") {
			return cfg, errors.New("config must be a JSON object")
		}
		decoder := json.NewDecoder(strings.NewReader(string(data)))
		decoder.DisallowUnknownFields()
		if err = decoder.Decode(&cfg); err != nil {
			return cfg, fmt.Errorf("decode config: %w", err)
		}
		if err = decoder.Decode(new(any)); err != io.EOF {
			return cfg, errors.New("config must contain a single JSON object")
		}
		if cfg.DataDir == "" {
			return cfg, errors.New("dataDir must not be empty")
		}
		cfg.DataDir = absolute(filepath.Dir(cfg.ConfigFile), cfg.DataDir)
		cfg.WebDir = optionalPath(filepath.Dir(cfg.ConfigFile), cfg.WebDir)
		cfg.FFmpeg = toolPath(filepath.Dir(cfg.ConfigFile), cfg.FFmpeg)
		cfg.FFprobe = toolPath(filepath.Dir(cfg.ConfigFile), cfg.FFprobe)
	}
	fields := map[string]*string{"addr": &cfg.Addr, "data-dir": &cfg.DataDir, "database": &cfg.Database, "web-dir": &cfg.WebDir, "ffmpeg": &cfg.FFmpeg, "ffprobe": &cfg.FFprobe, "log-level": &cfg.LogLevel, "lrclib-url": &cfg.LRCLIBURL}
	for name, field := range fields {
		value := env.Getenv("MUSIC_" + strings.ToUpper(strings.ReplaceAll(name, "-", "_")))
		if value != "" {
			*field = resolveOverride(env.CWD, name, value)
		}
	}
	quotaFlag := false
	flags.Visit(func(f *flag.Flag) {
		if f.Name == "quota-bytes" {
			quotaFlag = true
		}
	})
	if value := env.Getenv("MUSIC_QUOTA_BYTES"); value != "" && !quotaFlag {
		cfg.QuotaBytes, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("MUSIC_QUOTA_BYTES: %w", err)
		}
	}
	flags.Visit(func(f *flag.Flag) {
		if field, ok := fields[f.Name]; ok {
			*field = resolveOverride(env.CWD, f.Name, *values[f.Name])
		}
	})
	if value := *values["quota-bytes"]; quotaFlag {
		cfg.QuotaBytes, err = strconv.ParseInt(value, 10, 64)
		if err != nil {
			return cfg, fmt.Errorf("quota-bytes: %w", err)
		}
	}
	if cfg.DataDir == "" {
		return cfg, errors.New("data-dir must not be empty")
	}
	cfg.DataDir = absolute(env.CWD, cfg.DataDir)
	if err = cfg.Validate(); err != nil {
		return cfg, err
	}
	cfg.Database = filepath.Clean(filepath.FromSlash(cfg.Database))
	cfg.DatabasePath = filepath.Join(cfg.DataDir, cfg.Database)
	if cfg.WebDir == "" {
		for _, candidate := range []string{filepath.Join(filepath.Dir(env.Executable), "web"), filepath.Join(env.CWD, "apps", "web", "dist"), filepath.Join(env.CWD, "..", "web", "dist"), filepath.Join(env.CWD, "web")} {
			if info, err := os.Stat(filepath.Join(candidate, "index.html")); err == nil && !info.IsDir() {
				cfg.WebDir = filepath.Clean(candidate)
				break
			}
		}
	} else if info, err := os.Stat(filepath.Join(cfg.WebDir, "index.html")); err != nil || info.IsDir() {
		return cfg, errors.New("web-dir must contain index.html")
	}
	return cfg, nil
}

func (cfg Config) Validate() error {
	_, port, err := net.SplitHostPort(cfg.Addr)
	if err != nil {
		return fmt.Errorf("invalid addr: %w", err)
	}
	number, err := strconv.Atoi(port)
	if err != nil || number < 0 || number > 65535 {
		return errors.New("addr port must be between 0 and 65535")
	}
	if cfg.DataDir == "" || cfg.FFmpeg == "" || cfg.FFprobe == "" {
		return errors.New("data-dir, ffmpeg and ffprobe must not be empty")
	}
	if !filepath.IsLocal(cfg.Database) || cfg.Database == "." || strings.Contains(cfg.Database, ":") || strings.Contains(cfg.Database, "\\") {
		return errors.New("database must be a relative path inside data-dir, using forward slashes")
	}
	if cfg.QuotaBytes <= 0 {
		return errors.New("quota-bytes must be positive")
	}
	switch strings.ToLower(cfg.LogLevel) {
	case "debug", "info", "warn", "error":
	default:
		return errors.New("log-level must be debug, info, warn or error")
	}
	endpoint, err := url.Parse(cfg.LRCLIBURL)
	if err != nil || endpoint.Host == "" || (endpoint.Scheme != "http" && endpoint.Scheme != "https") {
		return errors.New("lrclib-url must be an HTTP(S) URL")
	}
	return nil
}

func absolute(base, path string) string {
	if filepath.IsAbs(path) {
		return filepath.Clean(path)
	}
	return filepath.Join(base, path)
}
func optionalPath(base, path string) string {
	if path == "" {
		return ""
	}
	return absolute(base, path)
}
func toolPath(base, path string) string {
	if strings.ContainsAny(path, "/\\") {
		return absolute(base, path)
	}
	return path
}
func resolveOverride(base, name, value string) string {
	switch name {
	case "data-dir", "web-dir":
		return optionalPath(base, value)
	case "ffmpeg", "ffprobe":
		return toolPath(base, value)
	}
	return value
}
func platformPaths(platform, localAppData string) (string, string, error) {
	switch platform {
	case "windows":
		if localAppData == "" {
			return "", "", errors.New("LOCALAPPDATA is required to determine the Windows default data directory")
		}
		root := filepath.Join(localAppData, "MusicPlayer")
		return root, filepath.Join(root, "config.json"), nil
	case "linux":
		return "/var/lib/music-player", "/etc/music-player/config.json", nil
	default:
		return "", "", fmt.Errorf("unsupported platform: %s", platform)
	}
}
