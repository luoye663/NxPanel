package nginxmodule

import (
	"cmp"
	"errors"
	"regexp"
	"strings"
)

type VersionRange struct {
	Min string `json:"min"`
	Max string `json:"max"`
}

type Compatibility struct {
	OSID           string       `json:"os_id"`
	Arch           string       `json:"arch"`
	Runtime        string       `json:"runtime"`
	OSVersion      VersionRange `json:"os_version"`
	RuntimeVersion VersionRange `json:"runtime_version"`
}

var (
	numericVersion = regexp.MustCompile(`^[0-9]+(\.[0-9]+)*$`)
	osIDPattern    = regexp.MustCompile(`^[a-z0-9][a-z0-9_-]{0,23}$`)
	ErrNoMatch     = errors.New("所选 Release 没有匹配当前系统、架构和 Nginx 运行时的构建")
	errAmbiguous   = errors.New("所选 Release 有多个不同的产物匹配当前环境，请联系模块维护者检查兼容声明")
)

func ManifestProtocol(assets []Asset) int {
	for _, asset := range assets {
		if asset.Compatibility != nil {
			return RangeProtocol
		}
	}
	return Protocol
}

func validPlatform(arch, runtime string) bool {
	return (arch == "amd64" || arch == "arm64") && (runtime == "nginx" || runtime == "openresty")
}

func validNumericVersion(version string) bool {
	return len(version) <= 64 && numericVersion.MatchString(version)
}

func compareVersion(left, right string) int {
	for left != "" || right != "" {
		leftPart, leftRest, _ := strings.Cut(left, ".")
		rightPart, rightRest, _ := strings.Cut(right, ".")
		leftPart, rightPart = strings.TrimLeft(leftPart, "0"), strings.TrimLeft(rightPart, "0")
		if order := cmp.Compare(len(leftPart), len(rightPart)); order != 0 {
			return order
		}
		if order := cmp.Compare(leftPart, rightPart); order != 0 {
			return order
		}
		left, right = leftRest, rightRest
	}
	return 0
}

func (bounds VersionRange) valid() bool {
	return validNumericVersion(bounds.Min) && validNumericVersion(bounds.Max) && compareVersion(bounds.Min, bounds.Max) <= 0
}

func (bounds VersionRange) contains(version string) bool {
	return compareVersion(bounds.Min, version) <= 0 && compareVersion(version, bounds.Max) <= 0
}

func validateCompatibility(rules []Compatibility) error {
	if rules == nil {
		return nil
	}
	if len(rules) == 0 || len(rules) > 16 {
		return errors.New("每个模块产物必须声明 1 至 16 条兼容规则，或省略该字段")
	}
	for _, rule := range rules {
		if !osIDPattern.MatchString(rule.OSID) || !validPlatform(rule.Arch, rule.Runtime) ||
			!rule.OSVersion.valid() || !rule.RuntimeVersion.valid() {
			return errors.New("无效的模块兼容规则或版本区间")
		}
	}
	return nil
}

func LegacyOS(value string) string {
	id, version, ok := strings.Cut(value, ":")
	if !ok {
		return value
	}
	switch id {
	case "debian", "rocky":
		version, _, _ = strings.Cut(version, ".")
	case "alpine":
		major, rest, _ := strings.Cut(version, ".")
		minor, _, _ := strings.Cut(rest, ".")
		version = major + "." + minor
	}
	return id + ":" + version
}

func (asset Asset) Matches(target Target) bool {
	if asset.Compatibility == nil {
		target.OS = LegacyOS(target.OS)
		return asset.Target == target
	}
	id, version, _ := strings.Cut(target.OS, ":")
	if !validNumericVersion(version) || !validNumericVersion(target.RuntimeVersion) {
		return false
	}
	for _, rule := range asset.Compatibility {
		if rule.OSID == id && rule.Arch == target.Arch && rule.Runtime == target.Runtime &&
			rule.OSVersion.contains(version) && rule.RuntimeVersion.contains(target.RuntimeVersion) {
			return true
		}
	}
	return false
}

func (manifest Manifest) Match(target Target) (Asset, error) {
	for _, asset := range manifest.Assets {
		if asset.Compatibility == nil && asset.Matches(target) {
			return asset, nil
		}
	}
	var selected Asset
	for _, asset := range manifest.Assets {
		if asset.Compatibility == nil || !asset.Matches(target) {
			continue
		}
		if selected.SHA256 != "" && selected.SHA256 != asset.SHA256 {
			return Asset{}, errAmbiguous
		}
		selected = asset
	}
	if selected.SHA256 == "" {
		return Asset{}, ErrNoMatch
	}
	return selected, nil
}
