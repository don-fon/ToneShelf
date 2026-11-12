package tagwidth

import (
	"encoding/json"
	"os"
	"path/filepath"
	"testing"
)

func TestSharedWidthCases(t *testing.T) {
	data, err := os.ReadFile(filepath.Join("..", "..", "..", "..", "api", "tag-width-cases.json"))
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Name  string `json:"name"`
		Units int    `json:"units"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for _, test := range cases {
		if got := Units(test.Name); got != test.Units {
			t.Errorf("%q: got %d, want %d", test.Name, got, test.Units)
		}
	}
}
