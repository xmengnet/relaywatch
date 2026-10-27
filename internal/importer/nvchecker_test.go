package importer

import (
	"strings"
	"testing"
)

const sample = `
[__config__]
oldver = "old_ver.json"

[mygo-cloudmusic]
source = "github"
github = "egoist/mygo-cloudmusic"
use_latest_release = true

[aria2-config]
source = "aur"
aur = "aria2-config"

[linux-kernel]
source = "archpkg"
archpkg = "linux"

[lib32-mesa]
source = "archpkg"
archpkg = "lib32-mesa"

[yazi-nightly]
source = "github"
github = "sxyazi/yazi"
use_max_tag = true
include_regex = "^v\\d+"

[weird-source]
source = "bitcoin"
bitcoin = "someone/somewhere"
`

func TestParseSample(t *testing.T) {
	res, err := Parse(sample)
	if err != nil {
		t.Fatalf("Parse: %v", err)
	}
	if len(res.Items) != 5 {
		t.Fatalf("items = %d, want 5; ignored=%v", len(res.Items), res.Ignored)
	}
	byName := map[string]Item{}
	for _, it := range res.Items {
		byName[it.Name] = it
	}
	if it := byName["mygo-cloudmusic"]; it.SourceType != "github_release" {
		t.Errorf("mygo source_type = %s", it.SourceType)
	}
	if it := byName["aria2-config"]; it.SourceType != "aur" {
		t.Errorf("aria2 source_type = %s", it.SourceType)
	}
	if it := byName["yazi-nightly"]; it.IncludeRegex == "" {
		t.Error("yazi include_regex lost")
	}
	if len(res.Ignored) != 1 || !strings.Contains(res.Ignored[0], "weird-source") {
		t.Errorf("ignored = %v", res.Ignored)
	}
	if len(res.Warnings) != 1 || !strings.Contains(res.Warnings[0], "__config__") {
		t.Errorf("warnings = %v", res.Warnings)
	}
}

func TestParseInvalidTOML(t *testing.T) {
	if _, err := Parse("not [valid toml ==="); err == nil {
		t.Error("expected error")
	}
}

func TestParseEmpty(t *testing.T) {
	res, err := Parse("")
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Items) != 0 {
		t.Errorf("items = %d, want 0", len(res.Items))
	}
}
