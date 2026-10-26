package configpreflight

import (
	"errors"
	"fmt"
)

const MaxWarnings = 100

type Warning struct {
	Path      string `json:"path,omitempty"`
	Line      int    `json:"line,omitempty"`
	Directive string `json:"directive,omitempty"`
	Message   string `json:"message"`
}

type Report struct {
	Warnings  []Warning `json:"warnings"`
	Complete  bool      `json:"complete"`
	Truncated bool      `json:"truncated"`
}

func (r *Report) Add(w Warning) {
	if len(r.Warnings) >= MaxWarnings {
		r.Truncated = true
		return
	}
	r.Warnings = append(r.Warnings, w)
}

// Incomplete keeps findings already collected, without treating an unfinished
// diagnostic as evidence that the configuration has no risks.
func (r *Report) Incomplete(err error) {
	r.Complete = false
	w := Warning{Message: "配置诊断未完成：" + err.Error()}
	var located *locationError
	if errors.As(err, &located) {
		w.Path, w.Line = located.path, located.line
	}
	r.Add(w)
}

type locationError struct {
	path string
	line int
	err  error
}

func (e *locationError) Error() string {
	if e.line > 0 {
		return fmt.Sprintf("%s:%d: %v", e.path, e.line, e.err)
	}
	return fmt.Sprintf("%s: %v", e.path, e.err)
}

func (e *locationError) Unwrap() error { return e.err }

type Request struct {
	SitePath   string `json:"site_path"`
	GlobalPath string `json:"global_path"`
	Main       string `json:"main"`
}
