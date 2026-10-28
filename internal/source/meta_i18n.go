package source

// metaEN 是内置监控源的英文元数据。键为源类型，fields 的键为字段 key。
// 只在 Registry.Metas() 输出时叠加，源实现本身保持单语，避免 33 个文件各写两套。
type fieldEN struct {
	label       string
	help        string
	placeholder string
}

type metaTranslation struct {
	label  string
	fields map[string]fieldEN
}

var metaEN = map[string]metaTranslation{
	"github_release": {
		label: "GitHub Release",
		fields: map[string]fieldEN{
			"repo":               {label: "Repository", help: "e.g. xmengnet/relaywatch"},
			"include_prerelease": {label: "Include pre-releases"},
			"token":              {label: "Access token", help: "Leave empty for public repos; set it for private repos or to avoid rate limits"},
		},
	},
	"github_tag": {
		label: "GitHub Tag",
		fields: map[string]fieldEN{
			"repo":        {label: "Repository"},
			"tag_pattern": {label: "Tag pattern", help: `Only tags matching this regex, e.g. ^v\d+\.\d+\.\d+$`},
			"token":       {label: "Access token"},
		},
	},
	"aur": {
		label: "AUR",
		fields: map[string]fieldEN{
			"pkgname": {label: "Package name"},
		},
	},
	"arch_official": {
		label: "Arch Official",
		fields: map[string]fieldEN{
			"pkgname": {label: "Package name"},
			"repo":    {label: "Repository", help: "Leave empty to search all official repositories"},
		},
	},
	"gitlab_release": {
		label: "GitLab Release",
		fields: map[string]fieldEN{
			"host":    {label: "GitLab URL", help: "Leave empty to use the default site from Site Settings"},
			"project": {label: "Project", placeholder: "group/project or numeric ID"},
			"token":   {label: "Access token", help: "Required for private projects"},
		},
	},
	"gitea_release": {
		label: "Gitea / Forgejo",
		fields: map[string]fieldEN{
			"host":  {label: "Site URL", help: "Leave empty to use the default site from Site Settings"},
			"repo":  {label: "Repository"},
			"token": {label: "Access token"},
		},
	},
	"pypi": {
		label: "PyPI",
		fields: map[string]fieldEN{
			"project": {label: "Project name"},
		},
	},
	"npm": {
		label: "npm",
		fields: map[string]fieldEN{
			"package":  {label: "Package name"},
			"dist_tag": {label: "Dist tag", help: "Defaults to latest; can be next, beta, etc."},
		},
	},
	"crates": {
		label: "crates.io",
		fields: map[string]fieldEN{
			"crate": {label: "Crate name"},
		},
	},
	"go_module": {
		label: "Go Module",
		fields: map[string]fieldEN{
			"module": {label: "Module path"},
		},
	},
	"http_json": {
		label: "HTTP JSON",
		fields: map[string]fieldEN{
			"url":     {label: "URL"},
			"path":    {label: "Version field path", help: "Dot-separated; use numeric indexes for arrays"},
			"headers": {label: "Request headers", help: "Format: Key: Value; Key2: Value2"},
		},
	},
	"http_regex": {
		label: "HTTP Regex",
		fields: map[string]fieldEN{
			"url":     {label: "URL"},
			"regex":   {label: "Regex"},
			"group":   {label: "Capture group", help: "Defaults to 1 (the first capture group)"},
			"headers": {label: "Request headers"},
		},
	},
	"docker_tag": {
		label: "Container Image",
		fields: map[string]fieldEN{
			"image":     {label: "Image", placeholder: "nginx or user/repo or ghcr.io/owner/repo"},
			"tag_regex": {label: "Tag pattern", help: `Only tags matching this regex, e.g. ^v\d+\.\d+\.\d+$`},
			"registry":  {label: "Registry", help: "Leave empty to infer from the image name; defaults to Docker Hub"},
		},
	},
	"repology": {
		label: "Repology",
		fields: map[string]fieldEN{
			"project": {label: "Project name", help: "Project name on Repology"},
			"repo":    {label: "Target repository", help: "e.g. arch, aur, debian_unstable"},
			"subrepo": {label: "Sub-repository", help: "Optional, e.g. extra"},
		},
	},
	"anitya": {
		label: "Anitya (Release Monitoring)",
		fields: map[string]fieldEN{
			"project": {label: "Project ID or slug", help: "Project ID or name on release-monitoring.org"},
		},
	},
	"rubygems": {
		label: "RubyGems",
		fields: map[string]fieldEN{
			"gem":                {label: "Gem name"},
			"include_prerelease": {label: "Include pre-releases"},
		},
	},
	"maven": {
		label: "Maven",
		fields: map[string]fieldEN{
			"group":    {label: "Group ID"},
			"artifact": {label: "Artifact ID"},
			"repo":     {label: "Maven repository URL", help: "Defaults to Maven Central"},
		},
	},
	"packagist": {
		label: "Packagist (PHP)",
		fields: map[string]fieldEN{
			"package":            {label: "Package name", help: "Format: vendor/name"},
			"include_prerelease": {label: "Include pre-releases"},
		},
	},
	"git_tag": {
		label: "Git (generic)",
		fields: map[string]fieldEN{
			"url":         {label: "Repository URL", help: "Supports https:// or git:// links"},
			"branch":      {label: "Branch name", help: "Optional; if set, tracks the latest commit SHA on that branch"},
			"use_commit":  {label: "Track commit", help: "Track the commit SHA of the branch or default HEAD instead of tags"},
			"tag_pattern": {label: "Tag pattern", help: "Only tags matching this regex, e.g. ^v[0-9.]+$"},
		},
	},
	"http_header": {
		label: "HTTP Header (redirect)",
		fields: map[string]fieldEN{
			"url":     {label: "URL"},
			"header":  {label: "Response header", help: "Defaults to Location (commonly captures the real version after a 30x redirect)"},
			"regex":   {label: "Regex extraction", help: "Optional; leave empty to use the whole header value"},
			"group":   {label: "Capture group", help: "Defaults to 1 (when a regex with groups is configured)"},
			"method":  {label: "Request method"},
			"headers": {label: "Custom request headers", help: "Format: Key: Value; Key2: Value2"},
		},
	},
	"bitbucket": {
		label: "Bitbucket",
		fields: map[string]fieldEN{
			"repo":        {label: "Repository"},
			"branch":      {label: "Branch", help: "Optional; if set, tracks the latest commit on that branch"},
			"tag_pattern": {label: "Tag pattern"},
			"token":       {label: "Access token", help: "Set for private repositories"},
		},
	},
	"pagure": {
		label: "Pagure",
		fields: map[string]fieldEN{
			"repo":        {label: "Repository name"},
			"host":        {label: "Site domain", help: "Defaults to https://pagure.io"},
			"tag_pattern": {label: "Tag pattern"},
		},
	},
	"container": {
		label: "Container Image (OCI Registry)",
		fields: map[string]fieldEN{
			"image":     {label: "Image name", help: "May include the registry hostname", placeholder: "nginx or library/nginx or ghcr.io/owner/repo"},
			"registry":  {label: "Registry host", help: "Leave empty to infer from the image name; defaults to Docker Hub", placeholder: "registry-1.docker.io"},
			"tag_regex": {label: "Tag pattern", help: "Only tags matching this regex, e.g. ^v[0-9.]+$"},
		},
	},
	"debianpkg": {
		label: "Debian Package",
		fields: map[string]fieldEN{
			"pkgname":       {label: "Source package name"},
			"suite":         {label: "Suite / release", help: "e.g. sid, bookworm, trixie; defaults to sid"},
			"strip_release": {label: "Strip release suffix", help: "Strip Debian revisions such as -1"},
		},
	},
	"ubuntupkg": {
		label: "Ubuntu Package",
		fields: map[string]fieldEN{
			"pkgname":       {label: "Source package name"},
			"suite":         {label: "Suite / series", help: "e.g. noble, jammy; leave empty for the default primary series"},
			"strip_release": {label: "Strip release suffix", help: "Strip Ubuntu revisions"},
		},
	},
	"cpan": {
		label: "CPAN (Perl)",
		fields: map[string]fieldEN{
			"module": {label: "Module or distribution name"},
		},
	},
	"cran": {
		label: "CRAN (R)",
		fields: map[string]fieldEN{
			"package": {label: "Package name"},
		},
	},
	"hackage": {
		label: "Hackage (Haskell)",
		fields: map[string]fieldEN{
			"package": {label: "Package name"},
		},
	},
	"openvsx": {
		label: "Open VSX",
		fields: map[string]fieldEN{
			"extension": {label: "Extension ID", help: "Format: publisher.name"},
		},
	},
	"vsmarketplace": {
		label: "VS Code Marketplace",
		fields: map[string]fieldEN{
			"extension": {label: "Extension ID", help: "Format: publisher.name"},
		},
	},
	"snapcraft": {
		label: "Snapcraft Store",
		fields: map[string]fieldEN{
			"snap":    {label: "Snap name"},
			"channel": {label: "Release channel", help: "e.g. stable, candidate, beta, edge; defaults to stable"},
		},
	},
	"manual": {
		label: "Manual version",
		fields: map[string]fieldEN{
			"version": {label: "Version"},
			"url":     {label: "Reference URL"},
		},
	},
	"cmd": {
		label: "Local command (restricted)",
		fields: map[string]fieldEN{
			"cmd":     {label: "Shell command", help: "Runs the command and takes the first line of stdout"},
			"timeout": {label: "Timeout (seconds)", help: "Defaults to 15 seconds"},
		},
	},
}

// applyMetaEN 把英文元数据叠加到 Meta 上；未收录的字段保持空，由测试兜底。
func applyMetaEN(m *Meta) {
	tr, ok := metaEN[m.Type]
	if !ok {
		return
	}
	m.LabelEn = tr.label
	for i := range m.Fields {
		f := &m.Fields[i]
		ft, ok := tr.fields[f.Key]
		if !ok {
			continue
		}
		f.LabelEn = ft.label
		f.HelpEn = ft.help
		f.PlaceholderEn = ft.placeholder
	}
}
