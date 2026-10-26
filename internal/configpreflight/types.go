package configpreflight

type Request struct {
	SitePath   string `json:"site_path"`
	GlobalPath string `json:"global_path"`
	Main       string `json:"main"`
}
