package source

// NewDefaultRegistry 返回注册了全部内置源的注册表。
// 新增源类型：实现 Source 后在此追加一行即可，main 无需改动。
func NewDefaultRegistry() *Registry {
	r := NewRegistry()
	r.Register(&GitHubReleaseSource{})
	r.Register(&GitHubTagSource{})
	r.Register(&AURSource{})
	r.Register(&ArchOfficialSource{})
	r.Register(&GitLabReleaseSource{})
	r.Register(&GiteaReleaseSource{})
	r.Register(&PyPISource{})
	r.Register(&NPMSource{})
	r.Register(&CratesSource{})
	r.Register(&GoModuleSource{})
	r.Register(&HTTPJSONSource{})
	r.Register(&HTTPRegexSource{})
	r.Register(&DockerTagSource{})
	return r
}
