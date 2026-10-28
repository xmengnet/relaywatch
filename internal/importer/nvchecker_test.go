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

func TestParseEcosystemsAndPacman(t *testing.T) {
	const tomlContent = `
[test-repology]
source = "repology"
repology = "ripgrep"
repo = "arch"
subrepo = "extra"

[test-anitya]
source = "anitya"
anitya = "13815"

[test-gems]
source = "gems"
gems = "rails"
include_prereleases = true

[test-maven]
source = "maven"
group = "org.apache.commons"
artifact = "commons-lang3"

[test-packagist]
source = "packagist"
packagist = "monolog/monolog"

[test-pacman]
source = "pacman"
pacman = "neovim"

[test-alpm]
source = "alpm"
alpm = "gcc"
repo = "core"
`
	res, err := Parse(tomlContent)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if len(res.Items) != 7 {
		t.Fatalf("items = %d, want 7; ignored=%v", len(res.Items), res.Ignored)
	}

	byName := map[string]Item{}
	for _, it := range res.Items {
		byName[it.Name] = it
	}

	if it := byName["test-repology"]; it.SourceType != "repology" || it.SourceConfig["repo"] != "arch" {
		t.Errorf("repology item invalid: %+v", it)
	}
	if it := byName["test-anitya"]; it.SourceType != "anitya" || it.SourceConfig["project"] != "13815" {
		t.Errorf("anitya item invalid: %+v", it)
	}
	if it := byName["test-gems"]; it.SourceType != "rubygems" || it.SourceConfig["include_prerelease"] != true {
		t.Errorf("gems item invalid: %+v", it)
	}
	if it := byName["test-maven"]; it.SourceType != "maven" || it.SourceConfig["group"] != "org.apache.commons" {
		t.Errorf("maven item invalid: %+v", it)
	}
	if it := byName["test-packagist"]; it.SourceType != "packagist" || it.SourceConfig["package"] != "monolog/monolog" {
		t.Errorf("packagist item invalid: %+v", it)
	}
	if it := byName["test-pacman"]; it.SourceType != "arch_official" || it.SourceConfig["pkgname"] != "neovim" {
		t.Errorf("pacman item not converted to arch_official: %+v", it)
	}
	if it := byName["test-alpm"]; it.SourceType != "arch_official" || it.SourceConfig["pkgname"] != "gcc" || it.SourceConfig["repo"] != "core" {
		t.Errorf("alpm item not converted to arch_official: %+v", it)
	}
}

func TestParseP1Sources(t *testing.T) {
	const tomlContent = `
[test-git]
source = "git"
git = "https://git.sr.ht/~user/repo"
branch = "master"
use_commit = true

[test-httpheader]
source = "httpheader"
url = "https://github.com/user/repo/releases/latest"
header = "Location"
regex = 'tag/v?([0-9.]+)'

[test-bitbucket]
source = "bitbucket"
bitbucket = "atlassian/repo"

[test-pagure]
source = "pagure"
pagure = "fedora/package"
`
	res, err := Parse(tomlContent)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if len(res.Items) != 4 {
		t.Fatalf("items = %d, want 4; ignored=%v", len(res.Items), res.Ignored)
	}

	byName := map[string]Item{}
	for _, it := range res.Items {
		byName[it.Name] = it
	}

	if it := byName["test-git"]; it.SourceType != "git_tag" || it.SourceConfig["branch"] != "master" || it.SourceConfig["use_commit"] != true {
		t.Errorf("git item invalid: %+v", it)
	}
	if it := byName["test-httpheader"]; it.SourceType != "http_header" || it.SourceConfig["header"] != "Location" {
		t.Errorf("httpheader item invalid: %+v", it)
	}
	if it := byName["test-bitbucket"]; it.SourceType != "bitbucket" || it.SourceConfig["repo"] != "atlassian/repo" {
		t.Errorf("bitbucket item invalid: %+v", it)
	}
	if it := byName["test-pagure"]; it.SourceType != "pagure" || it.SourceConfig["repo"] != "fedora/package" {
		t.Errorf("pagure item invalid: %+v", it)
	}
}

func TestParseP2Sources(t *testing.T) {
	const tomlContent = `
[test-container]
source = "container"
container = "ghcr.io/owner/repo"
registry = "ghcr.io"

[test-debianpkg]
source = "debianpkg"
debianpkg = "ripgrep"
suite = "bookworm"
strip_release = true

[test-ubuntupkg]
source = "ubuntupkg"
ubuntupkg = "curl"
suite = "noble"

[test-cpan]
source = "cpan"
cpan = "Mojolicious"

[test-cran]
source = "cran"
cran = "ggplot2"

[test-hackage]
source = "hackage"
hackage = "pandoc"
`
	res, err := Parse(tomlContent)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if len(res.Items) != 6 {
		t.Fatalf("items = %d, want 6; ignored=%v", len(res.Items), res.Ignored)
	}

	byName := map[string]Item{}
	for _, it := range res.Items {
		byName[it.Name] = it
	}

	if it := byName["test-container"]; it.SourceType != "container" || it.SourceConfig["registry"] != "ghcr.io" {
		t.Errorf("container item invalid: %+v", it)
	}
	if it := byName["test-debianpkg"]; it.SourceType != "debianpkg" || it.SourceConfig["suite"] != "bookworm" || it.SourceConfig["strip_release"] != true {
		t.Errorf("debianpkg item invalid: %+v", it)
	}
	if it := byName["test-ubuntupkg"]; it.SourceType != "ubuntupkg" || it.SourceConfig["suite"] != "noble" {
		t.Errorf("ubuntupkg item invalid: %+v", it)
	}
	if it := byName["test-cpan"]; it.SourceType != "cpan" || it.SourceConfig["module"] != "Mojolicious" {
		t.Errorf("cpan item invalid: %+v", it)
	}
	if it := byName["test-cran"]; it.SourceType != "cran" || it.SourceConfig["package"] != "ggplot2" {
		t.Errorf("cran item invalid: %+v", it)
	}
	if it := byName["test-hackage"]; it.SourceType != "hackage" || it.SourceConfig["package"] != "pandoc" {
		t.Errorf("hackage item invalid: %+v", it)
	}
}

func TestParseP3Sources(t *testing.T) {
	const tomlContent = `
[test-openvsx]
source = "openvsx"
openvsx = "rust-lang.rust-analyzer"

[test-vsmarketplace]
source = "vsmarketplace"
vsmarketplace = "golang.go"

[test-snapcraft]
source = "snapcraft"
snapcraft = "canonical-livepatch"
channel = "stable"

[test-manual]
source = "manual"
manual = "1.0.0"

[test-cmd]
source = "cmd"
cmd = "echo 1.2.3"
`
	res, err := Parse(tomlContent)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	// 默认下 cmd 被拦截，产生 1 条 Ignored
	if len(res.Items) != 4 {
		t.Fatalf("items = %d, want 4; ignored=%v", len(res.Items), res.Ignored)
	}
	if len(res.Ignored) != 1 || !strings.Contains(res.Ignored[0], "出于安全考虑已跳过本地 shell 命令") {
		t.Errorf("unexpected ignored: %v", res.Ignored)
	}

	byName := map[string]Item{}
	for _, it := range res.Items {
		byName[it.Name] = it
	}

	if it := byName["test-openvsx"]; it.SourceType != "openvsx" || it.SourceConfig["extension"] != "rust-lang.rust-analyzer" {
		t.Errorf("openvsx invalid: %+v", it)
	}
	if it := byName["test-vsmarketplace"]; it.SourceType != "vsmarketplace" || it.SourceConfig["extension"] != "golang.go" {
		t.Errorf("vsmarketplace invalid: %+v", it)
	}
	if it := byName["test-snapcraft"]; it.SourceType != "snapcraft" || it.SourceConfig["snap"] != "canonical-livepatch" {
		t.Errorf("snapcraft invalid: %+v", it)
	}
	if it := byName["test-manual"]; it.SourceType != "manual" || it.SourceConfig["version"] != "1.0.0" {
		t.Errorf("manual invalid: %+v", it)
	}
}

func TestParsePatternRewriting(t *testing.T) {
	const tomlContent = `
[date-pkg]
source = "github"
github = "owner/repo"
use_latest_release = true
from_pattern = '(\d{4})(\d{2})(\d{2})'
to_pattern = '\1.\2.\3'
`
	res, err := Parse(tomlContent)
	if err != nil {
		t.Fatalf("Parse err: %v", err)
	}
	if len(res.Items) != 1 {
		t.Fatalf("items = %d, want 1", len(res.Items))
	}
	it := res.Items[0]
	if it.FromPattern != `(\d{4})(\d{2})(\d{2})` {
		t.Errorf("from_pattern = %q", it.FromPattern)
	}
	if it.ToPattern != `\1.\2.\3` {
		t.Errorf("to_pattern = %q", it.ToPattern)
	}
	if len(res.Warnings) != 0 {
		t.Errorf("expected 0 warnings, got: %v", res.Warnings)
	}
}
