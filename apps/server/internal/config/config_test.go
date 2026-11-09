package config

import (
	"errors"
	"flag"
	"os"
	"path/filepath"
	"testing"
)

func testEnvironment(t *testing.T, values map[string]string) Environment {
	t.Helper()
	root := t.TempDir()
	values["LOCALAPPDATA"] = filepath.Join(root, "local")
	return Environment{OS: "windows", CWD: root, Executable: filepath.Join(root, "app", "music-player.exe"), Getenv: func(key string) string { return values[key] }}
}

func TestDefaultsAndOverridePrecedence(t *testing.T) {
	values := map[string]string{}
	env := testEnvironment(t, values)
	cfg, err := LoadWith(nil, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Join(values["LOCALAPPDATA"], "MusicPlayer") || cfg.DatabasePath != filepath.Join(cfg.DataDir, "data", "music.db") || cfg.Addr != "127.0.0.1:8080" || cfg.QuotaBytes != 10*1024*1024*1024 {
		t.Fatalf("defaults=%+v", cfg)
	}
	dir := filepath.Join(env.CWD, "configuration")
	if err = os.MkdirAll(dir, 0700); err != nil {
		t.Fatal(err)
	}
	file := filepath.Join(dir, "config.json")
	if err = os.WriteFile(file, []byte(`{"addr":"127.0.0.1:8001","dataDir":"library","database":"db/player.db","quotaBytes":1000,"ffmpeg":"tools/ffmpeg","ffprobe":"tools/ffprobe","logLevel":"debug","lrclibUrl":"https://example.com"}`), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err = LoadWith([]string{"--config", file}, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Join(dir, "library") || cfg.FFmpeg != filepath.Join(dir, "tools", "ffmpeg") || cfg.DatabasePath != filepath.Join(dir, "library", "db", "player.db") {
		t.Fatalf("file paths=%+v", cfg)
	}
	values["MUSIC_CONFIG"] = file
	values["MUSIC_DATA_DIR"] = "env-library"
	values["MUSIC_ADDR"] = "127.0.0.1:8002"
	values["MUSIC_QUOTA_BYTES"] = "2000"
	values["MUSIC_FFMPEG"] = "env-tools/ffmpeg"
	cfg, err = LoadWith([]string{"--data-dir", "cli-library", "--addr", "127.0.0.1:8003", "--quota-bytes", "3000", "--ffprobe", "cli-tools/ffprobe"}, env)
	if err != nil {
		t.Fatal(err)
	}
	if cfg.DataDir != filepath.Join(env.CWD, "cli-library") || cfg.Addr != "127.0.0.1:8003" || cfg.QuotaBytes != 3000 || cfg.FFmpeg != filepath.Join(env.CWD, "env-tools", "ffmpeg") || cfg.FFprobe != filepath.Join(env.CWD, "cli-tools", "ffprobe") {
		t.Fatalf("overrides=%+v", cfg)
	}
}

func TestConfigurationFailuresDoNotCreateData(t *testing.T) {
	for _, raw := range []string{`null`, `[]`, `{`, `{} {}`, `{"unknown":1}`, `{"quotaBytes":0}`, `{"dataDir":""}`, `{"database":"../outside.db"}`, `{"database":"/outside.db"}`, `{"database":"C:/outside.db"}`, `{"database":"."}`, `{"logLevel":"verbose"}`, `{"lrclibUrl":"file:///tmp"}`, `{"addr":"localhost:99999"}`} {
		t.Run(raw, func(t *testing.T) {
			env := testEnvironment(t, map[string]string{})
			path := filepath.Join(env.CWD, "invalid.json")
			if err := os.WriteFile(path, []byte(raw), 0600); err != nil {
				t.Fatal(err)
			}
			if _, err := LoadWith([]string{"--config", path}, env); err == nil {
				t.Fatal("invalid config accepted")
			}
			if _, err := os.Stat(filepath.Join(env.CWD, "local")); !os.IsNotExist(err) {
				t.Fatal("validation created data", err)
			}
		})
	}
	env := testEnvironment(t, map[string]string{})
	for _, args := range [][]string{{"--config", "missing.json"}, {"--quota-bytes", ""}, {"--quota-bytes", "abc"}, {"--data-dir", ""}, {"--database", "../x"}, {"--unknown"}, {"unexpected"}} {
		if _, err := LoadWith(args, env); err == nil {
			t.Fatalf("invalid arguments accepted: %v", args)
		}
	}
	if _, err := LoadWith([]string{"--help"}, env); !errors.Is(err, flag.ErrHelp) {
		t.Fatalf("help=%v", err)
	}
}

func TestWebDiscoveryAndExplicitMissingDirectory(t *testing.T) {
	env := testEnvironment(t, map[string]string{})
	web := filepath.Join(filepath.Dir(env.Executable), "web")
	if err := os.MkdirAll(web, 0700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(web, "index.html"), []byte("web"), 0600); err != nil {
		t.Fatal(err)
	}
	cfg, err := LoadWith(nil, env)
	if err != nil || cfg.WebDir != web {
		t.Fatalf("discovery=%+v %v", cfg, err)
	}
	if _, err = LoadWith([]string{"--web-dir", "missing"}, env); err == nil {
		t.Fatal("missing explicit web directory accepted")
	}
}
func TestLinuxPlatformDefaults(t *testing.T) {
	root, file, err := platformPaths("linux", "")
	if err != nil || root != "/var/lib/music-player" || file != "/etc/music-player/config.json" {
		t.Fatalf("Linux defaults=%s %s %v", root, file, err)
	}
}
func TestQuotaFlagOverridesInvalidLowerPriorityEnvironment(t *testing.T) {
	env := testEnvironment(t, map[string]string{"MUSIC_QUOTA_BYTES": "invalid"})
	cfg, err := LoadWith([]string{"--quota-bytes", "3000"}, env)
	if err != nil || cfg.QuotaBytes != 3000 {
		t.Fatalf("quota precedence=%+v %v", cfg, err)
	}
}
