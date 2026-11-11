package main

import (
	"bytes"
	"context"
	"encoding/json"
	"path/filepath"
	"strings"
	"testing"

	"music-player/server/internal/store"
)

func TestServerOnlyAccountAdministration(t *testing.T) {
	root := t.TempDir()
	var output bytes.Buffer
	run := func(action string, args ...string) error {
		output.Reset()
		return runAccounts(append([]string{action, "--data-dir", root}, args...), strings.NewReader("chosen account\n"), &output)
	}
	if err := run("add", "--id", "default-user", "--account-stdin"); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(output.String(), "chosen account") {
		t.Fatal("stdin credential echoed")
	}
	if err := run("list"); err != nil {
		t.Fatal(err)
	}
	var accounts []store.AccountInfo
	if err := json.Unmarshal(output.Bytes(), &accounts); err != nil || len(accounts) != 1 || !accounts[0].Enabled {
		t.Fatalf("accounts %s %v", output.String(), err)
	}
	if err := run("add", "--id", "default-user"); err == nil {
		t.Fatal("add overwrote existing account")
	}
	if err := run("reset", "--id", "default-user"); err != nil {
		t.Fatal(err)
	}
	parts := strings.Split(strings.TrimSpace(output.String()), ": ")
	generated := parts[len(parts)-1]
	s, err := store.Open(context.Background(), filepath.Join(root, "data", "music.db"), store.WithDataRoot(root))
	if err != nil {
		t.Fatal(err)
	}
	defer s.Close()
	if len(generated) != 43 {
		t.Fatal("generated credential length")
	}
	if id, _, err := s.AuthenticateAccount(context.Background(), generated); err != nil || id != store.DefaultUserID {
		t.Fatalf("generated login %s %v", id, err)
	}
	if err := run("disable", "--id", "default-user"); err != nil {
		t.Fatal(err)
	}
	if _, _, err = s.AuthenticateAccount(context.Background(), generated); err == nil {
		t.Fatal("disabled credential accepted")
	}
	if err := run("reset", "--id", "missing"); err == nil {
		t.Fatal("reset created user")
	}
	if err := run("register"); err == nil {
		t.Fatal("unknown command accepted")
	}
}
