package nginx

import (
	"os"
	"os/exec"
	"regexp"
	"strings"
)

var nginxBinCandidates = []string{
	"/usr/local/openresty/bin/openresty",
	"/usr/sbin/nginx",
	"/usr/sbin/openresty",
}

func FindNginxBin() string {
	if path, err := exec.Command("which", "nginx").Output(); err == nil {
		if p := strings.TrimSpace(string(path)); p != "" {
			return p
		}
	}
	if path, err := exec.Command("which", "openresty").Output(); err == nil {
		if p := strings.TrimSpace(string(path)); p != "" {
			return p
		}
	}
	for _, p := range nginxBinCandidates {
		if fi, err := os.Stat(p); err == nil && !fi.IsDir() {
			return p
		}
	}
	return ""
}

var confPathRe = regexp.MustCompile(`configuration file\s+(\S+)\s+(?:syntax is ok|test is successful)`)

func ParseConfPathFromTestOutput(output string) string {
	m := confPathRe.FindStringSubmatch(output)
	if len(m) >= 2 {
		return m[1]
	}
	return ""
}

func ParseVersion(output string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if strings.HasPrefix(line, "nginx version:") {
			version := strings.TrimPrefix(line, "nginx version:")
			return strings.TrimSpace(version)
		}
	}
	return ""
}

func ParseConfigurePath(output string) string {
	return extractConfigureParam(output, "--conf-path=")
}

func ParsePrefix(output string) string {
	return extractConfigureParam(output, "--prefix=")
}

func extractConfigureParam(output, paramPrefix string) string {
	for _, line := range strings.Split(output, "\n") {
		line = strings.TrimSpace(line)
		if !strings.HasPrefix(line, "configure arguments:") {
			continue
		}

		args := line[len("configure arguments:"):]
		for _, arg := range strings.Split(args, " ") {
			arg = strings.TrimSpace(arg)
			if strings.HasPrefix(arg, paramPrefix) {
				return strings.TrimPrefix(arg, paramPrefix)
			}
		}
	}
	return ""
}
