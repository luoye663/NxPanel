// Package nginxpatch locates explicit server directives without converting an
// imported configuration to panel markers. Offsets always refer to the original
// file; ambiguous or unsupported syntax is rejected rather than guessed.
package nginxpatch

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"path/filepath"
	"strings"
)

const MaxConfigBytes = 2 << 20

type token struct {
	value      string
	start, end int
	sigil      byte
}

type directive struct {
	words      []token
	children   []directive
	start, end int
	block      bool
}

// CertificateReference contains paths and identity only, never PEM material.
type CertificateReference struct {
	FileHash       string `json:"file_hash"`
	ServerHash     string `json:"server_hash,omitempty"`
	CertPath       string `json:"cert_path,omitempty"`
	KeyPath        string `json:"key_path,omitempty"`
	ReadOnlyReason string `json:"read_only_reason,omitempty"`
}

func Hash(content string) string {
	h := sha256.Sum256([]byte(content))
	return hex.EncodeToString(h[:])
}

// InspectCertificate requires a unique certificate target for an explicit name.
// Plain HTTP siblings are ignored; includes, inherited references and
// multi-certificate servers remain read-only.
func InspectCertificate(content, domain string) CertificateReference {
	ref := CertificateReference{FileHash: Hash(content)}
	s, err := locateCertificate(content, domain)
	if err != nil {
		ref.ReadOnlyReason = err.Error()
		return ref
	}
	ref.ServerHash = Hash(content[s.start:s.end])
	cert, key, err := certificatePair(s)
	if err != nil {
		ref.ReadOnlyReason = err.Error()
		return ref
	}
	ref.CertPath, ref.KeyPath = cert.value, key.value
	return ref
}

// ReplaceCertificate changes only the two argument spans from the confirmed
// file version. Callers must also pass expectedHash to the Agent transaction.
func ReplaceCertificate(content, domain, expectedHash, certPath, keyPath string) (string, error) {
	if expectedHash == "" || Hash(content) != expectedHash {
		return "", fmt.Errorf("configuration changed since preview")
	}
	if !staticPath(certPath) || !staticPath(keyPath) || certPath == keyPath {
		return "", fmt.Errorf("replacement requires distinct static absolute paths")
	}
	s, err := locateCertificate(content, domain)
	if err != nil {
		return "", err
	}
	cert, key, err := certificatePair(s)
	if err != nil {
		return "", err
	}
	// Replace right-to-left so the second span retains its original offsets.
	if cert.start > key.start {
		return content[:key.start] + keyPath + content[key.end:cert.start] + certPath + content[cert.end:], nil
	}
	return content[:cert.start] + certPath + content[cert.end:key.start] + keyPath + content[key.end:], nil
}

func staticPath(path string) bool {
	if !filepath.IsAbs(path) || filepath.Clean(path) != path {
		return false
	}
	for _, c := range path {
		if c <= ' ' || strings.ContainsRune("$;{}#\\\"'", c) {
			return false
		}
	}
	return true
}

func certificatePair(s directive) (token, token, error) {
	return certificatePairResolved(s, false)
}

func certificatePairResolved(s directive, resolved bool) (token, token, error) {
	var certs, keys []token
	var walk func([]directive, bool) error
	walk = func(ds []directive, nested bool) error {
		for _, d := range ds {
			name := d.words[0].value
			if strings.Contains(name, "_certificate_by_lua") {
				return fmt.Errorf("scripted TLS certificate selection is read-only")
			}
			if name == "include" && !resolved {
				return fmt.Errorf("server contains include directives with unresolved ownership")
			}
			if name == "ssl_certificate" || name == "ssl_certificate_key" {
				if nested || d.block || len(d.words) != 2 {
					return fmt.Errorf("certificate directives are not a direct static pair")
				}
				if name == "ssl_certificate" {
					certs = append(certs, d.words[1])
				} else {
					keys = append(keys, d.words[1])
				}
			}
			if err := walk(d.children, true); err != nil {
				return err
			}
		}
		return nil
	}
	if err := walk(s.children, false); err != nil {
		return token{}, token{}, err
	}
	if len(certs) != 1 || len(keys) != 1 {
		return token{}, token{}, fmt.Errorf("missing, inherited or multiple certificate references")
	}
	if !staticPath(certs[0].value) || !staticPath(keys[0].value) || certs[0].value == keys[0].value {
		return token{}, token{}, fmt.Errorf("certificate references require distinct static absolute paths (no variables)")
	}
	return certs[0], keys[0], nil
}

func ValidateCertificateRuntime(dump string) error {
	if len(dump) > 32<<20 {
		return fmt.Errorf("runtime configuration exceeds inspection budget")
	}
	tokens, err := lex(dump)
	if err != nil {
		return err
	}
	for _, token := range tokens {
		if strings.Contains(token.value, "_certificate_by_lua") {
			return fmt.Errorf("inherited scripted TLS certificate selection is read-only")
		}
	}
	return nil
}

// CertificateIncludes enumerates only literal includes owned by the selected
// server. Callers must read and hash every returned file through the Agent.
func CertificateIncludes(content, domain string) ([]string, error) {
	s, err := locateCertificate(content, domain)
	if err != nil {
		return nil, err
	}
	return includePaths(s.children)
}

// InspectIncludeFragment refuses certificate or server declarations in an
// included fragment. Relative, wildcard and variable includes remain read-only.
func InspectIncludeFragment(content string) ([]string, error) {
	ds, err := parse(content)
	if err != nil {
		return nil, err
	}
	var check func([]directive) error
	check = func(nodes []directive) error {
		for _, d := range nodes {
			n := d.words[0].value
			if n == "ssl_certificate" || n == "ssl_certificate_key" || n == "server" {
				return fmt.Errorf("included configuration declares a certificate or server")
			}
			if err := check(d.children); err != nil {
				return err
			}
		}
		return nil
	}
	if err := check(ds); err != nil {
		return nil, err
	}
	return includePaths(ds)
}
func includePaths(ds []directive) ([]string, error) {
	var paths []string
	var walk func([]directive) error
	walk = func(nodes []directive) error {
		for _, d := range nodes {
			if d.words[0].value == "include" {
				if d.block || len(d.words) != 2 || !staticPath(d.words[1].value) || strings.ContainsAny(d.words[1].value, "*?[") {
					return fmt.Errorf("include ownership cannot be resolved statically")
				}
				paths = append(paths, d.words[1].value)
			}
			if err := walk(d.children); err != nil {
				return err
			}
		}
		return nil
	}
	err := walk(ds)
	return paths, err
}

// InspectResolvedCertificate is safe only after checking the complete bounded
// include graph with InspectIncludeFragment and Agent path validation.
func InspectResolvedCertificate(content, domain string) CertificateReference {
	ref := CertificateReference{FileHash: Hash(content)}
	s, err := locateCertificate(content, domain)
	if err != nil {
		ref.ReadOnlyReason = err.Error()
		return ref
	}
	ref.ServerHash = Hash(content[s.start:s.end])
	cert, key, err := certificatePairResolved(s, true)
	if err != nil {
		ref.ReadOnlyReason = err.Error()
		return ref
	}
	ref.CertPath = cert.value
	ref.KeyPath = key.value
	return ref
}
func ReplaceResolvedCertificate(content, domain, expectedHash, certPath, keyPath string) (string, error) {
	if expectedHash == "" || Hash(content) != expectedHash {
		return "", fmt.Errorf("configuration changed since preview")
	}
	if !staticPath(certPath) || !staticPath(keyPath) || certPath == keyPath {
		return "", fmt.Errorf("invalid static certificate paths")
	}
	s, err := locateCertificate(content, domain)
	if err != nil {
		return "", err
	}
	cert, key, err := certificatePairResolved(s, true)
	if err != nil {
		return "", err
	}
	if cert.start > key.start {
		return content[:key.start] + keyPath + content[key.end:cert.start] + certPath + content[cert.end:], nil
	}
	return content[:cert.start] + certPath + content[cert.end:key.start] + keyPath + content[key.end:], nil
}

func locate(content, domain string) (directive, error) {
	return locateServer(content, domain, false)
}

func locateCertificate(content, domain string) (directive, error) {
	return locateServer(content, domain, true)
}

// Only discard demonstrably plain HTTP siblings. Includes or TLS directives
// may hide another certificate target and must retain the ambiguity check.
func plainHTTPServer(s directive) bool {
	var plain func([]directive) bool
	plain = func(nodes []directive) bool {
		for _, d := range nodes {
			name := d.words[0].value
			if name == "include" || name == "ssl" || strings.HasPrefix(name, "ssl_") {
				return false
			}
			if name == "listen" {
				for _, arg := range d.words[1:] {
					if arg.value == "ssl" || arg.value == "quic" {
						return false
					}
				}
			}
			if !plain(d.children) {
				return false
			}
		}
		return true
	}
	return plain(s.children)
}

func locateServer(content, domain string, certificate bool) (directive, error) {
	if domain == "" || strings.ContainsAny(domain, "*~$ \t\r\n") {
		return directive{}, fmt.Errorf("explicit server name required")
	}
	ds, err := parse(content)
	if err != nil {
		return directive{}, err
	}
	var matches []directive
	var walk func([]directive)
	walk = func(nodes []directive) {
		for _, d := range nodes {
			if d.block && len(d.words) == 1 && d.words[0].value == "server" {
				found := false
				for _, child := range d.children {
					if child.words[0].value != "server_name" || child.block {
						continue
					}
					for _, word := range child.words[1:] {
						if word.value == domain {
							found = true
						}
					}
				}
				if found {
					matches = append(matches, d)
				}
			} else if d.block && len(d.words) == 1 && d.words[0].value == "http" {
				walk(d.children)
			}
		}
	}
	walk(ds)
	if certificate && len(matches) > 1 {
		var tlsMatches []directive
		for _, match := range matches {
			if !plainHTTPServer(match) {
				tlsMatches = append(tlsMatches, match)
			}
		}
		matches = tlsMatches
	}
	if len(matches) != 1 {
		return directive{}, fmt.Errorf("server target is missing or ambiguous (%d matches)", len(matches))
	}
	return matches[0], nil
}

func ValidateWholeFileOwnership(content, domain string) error {
	ds, err := parse(content)
	if err != nil {
		return err
	}
	if len(ds) != 1 || !ds[0].block || len(ds[0].words) != 1 || ds[0].words[0].value != "server" {
		return fmt.Errorf("configuration does not belong exclusively to one server")
	}
	_, err = locate(content, domain)
	return err
}

func ServerLogPaths(content, domain string) (string, string, error) {
	s, err := locate(content, domain)
	if err != nil {
		return "", "", err
	}
	var access, errorLog string
	counts := map[string]int{}
	for _, d := range s.children {
		n := d.words[0].value
		if (n == "access_log" || n == "error_log") && !d.block && len(d.words) >= 2 {
			counts[n]++
			if counts[n] > 1 {
				return "", "", fmt.Errorf("multiple server log paths are ambiguous")
			}
			path := d.words[1].value
			if !staticPath(path) {
				path = ""
			}
			if n == "access_log" {
				access = path
			} else {
				errorLog = path
			}
		}
	}
	return access, errorLog, nil
}

func parse(content string) ([]directive, error) {
	if len(content) > MaxConfigBytes {
		return nil, fmt.Errorf("configuration exceeds size limit")
	}
	tokens, err := lex(content)
	if err != nil {
		return nil, err
	}
	i := 0
	var block func(int) ([]directive, error)
	block = func(depth int) ([]directive, error) {
		if depth > 64 {
			return nil, fmt.Errorf("configuration nesting exceeds limit")
		}
		var ds []directive
		for i < len(tokens) {
			if tokens[i].sigil == '}' {
				if depth == 0 {
					return nil, fmt.Errorf("unexpected closing brace")
				}
				i++
				return ds, nil
			}
			d := directive{start: tokens[i].start}
			for i < len(tokens) && tokens[i].sigil == 0 {
				d.words = append(d.words, tokens[i])
				i++
			}
			if len(d.words) == 0 || i == len(tokens) || tokens[i].sigil == '}' {
				return nil, fmt.Errorf("incomplete directive")
			}
			d.block = tokens[i].sigil == '{'
			d.end = tokens[i].end
			i++
			if d.block {
				var err error
				d.children, err = block(depth + 1)
				if err != nil {
					return nil, err
				}
				d.end = tokens[i-1].end
			}
			ds = append(ds, d)
		}
		if depth > 0 {
			return nil, fmt.Errorf("unclosed block")
		}
		return ds, nil
	}
	return block(0)
}

func lex(s string) ([]token, error) {
	var ts []token
	for i := 0; i < len(s); {
		c := s[i]
		if c == 0 {
			return nil, fmt.Errorf("NUL in configuration")
		}
		if strings.ContainsRune(" \t\r\n", rune(c)) {
			i++
			continue
		}
		if c == '#' {
			for i < len(s) && s[i] != '\n' {
				i++
			}
			continue
		}
		start := i
		if strings.ContainsRune("{};", rune(c)) {
			i++
			ts = append(ts, token{value: s[start:i], start: start, end: i, sigil: c})
		} else {
			var value strings.Builder
			var quote byte
			for i < len(s) {
				c = s[i]
				if c == 0 {
					return nil, fmt.Errorf("NUL in configuration")
				}
				if quote == 0 && strings.ContainsRune(" \t\r\n{};#", rune(c)) {
					break
				}
				if c == '\\' {
					i++
					if i == len(s) {
						return nil, fmt.Errorf("trailing escape")
					}
					// Escaped paths are deliberately not eligible for patching.
					value.WriteByte('\\')
					value.WriteByte(s[i])
					i++
					continue
				}
				if quote == 0 && (c == '\'' || c == '"') {
					quote = c
					i++
					continue
				}
				if quote != 0 && c == quote {
					quote = 0
					i++
					continue
				}
				value.WriteByte(c)
				i++
			}
			if quote != 0 {
				return nil, fmt.Errorf("unterminated quote")
			}
			ts = append(ts, token{value: value.String(), start: start, end: i})
		}
		if len(ts) > 100000 {
			return nil, fmt.Errorf("configuration token limit exceeded")
		}
	}
	return ts, nil
}
