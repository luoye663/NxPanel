package upstream

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"unicode/utf8"
)

const (
	maxServers             = 128
	maxAdvancedBytes       = 16 * 1024
	maxAdvancedStatements  = 64
	maxKeepalive           = 10000
	maxKeepaliveRequests   = 1000000
	maxKeepaliveTimeoutSec = 3600
)

var (
	nameRE            = regexp.MustCompile(`^[A-Za-z_][A-Za-z0-9_]{0,62}$`)
	hostnameRE        = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9.-]{0,251}[A-Za-z0-9])?$`)
	managedDirectives = map[string]struct{}{
		"server": {}, "least_conn": {}, "ip_hash": {}, "hash": {}, "random": {},
		"keepalive": {}, "keepalive_requests": {}, "keepalive_timeout": {},
	}
)

func NormalizeAndValidate(req *SaveRequest) error {
	req.Name = strings.TrimSpace(req.Name)
	if req.Name != "" && !nameRE.MatchString(req.Name) {
		return fmt.Errorf("name must match %s", nameRE.String())
	}
	if req.Algorithm == "" {
		req.Algorithm = "round_robin"
	}
	switch req.Algorithm {
	case "round_robin", "least_conn", "ip_hash", "hash":
	default:
		return fmt.Errorf("unsupported algorithm %q", req.Algorithm)
	}
	req.HashKey = strings.TrimSpace(req.HashKey)
	if req.Algorithm == "hash" {
		if req.HashKey == "" {
			return fmt.Errorf("hash_key is required for hash algorithm")
		}
		if err := validateDirectiveFragment(req.HashKey); err != nil {
			return fmt.Errorf("invalid hash_key: %w", err)
		}
	} else if req.HashKey != "" || req.Consistent {
		return fmt.Errorf("hash_key and consistent are only valid for hash algorithm")
	}
	if req.Keepalive < 0 || req.Keepalive > maxKeepalive || req.KeepaliveRequests < 0 || req.KeepaliveRequests > maxKeepaliveRequests || req.KeepaliveTimeoutSeconds < 0 || req.KeepaliveTimeoutSeconds > maxKeepaliveTimeoutSec {
		return fmt.Errorf("keepalive settings exceed allowed limits")
	}
	if req.Keepalive == 0 && (req.KeepaliveRequests != 0 || req.KeepaliveTimeoutSeconds != 0) {
		return fmt.Errorf("keepalive_requests and keepalive_timeout_seconds require keepalive")
	}
	if len(req.Servers) == 0 || len(req.Servers) > maxServers {
		return fmt.Errorf("servers must contain 1-%d entries", maxServers)
	}
	seen := make(map[string]struct{}, len(req.Servers))
	activePrimary := 0
	for i := range req.Servers {
		s := &req.Servers[i]
		s.Address = strings.TrimSpace(s.Address)
		if err := validateAddress(s.Address); err != nil {
			return fmt.Errorf("servers[%d].address: %w", i, err)
		}
		key := strings.ToLower(s.Address)
		if _, ok := seen[key]; ok {
			return fmt.Errorf("duplicate server address %q", s.Address)
		}
		seen[key] = struct{}{}
		if s.Weight == 0 {
			s.Weight = 1
		}
		if s.FailTimeoutSeconds == 0 {
			s.FailTimeoutSeconds = 10
		}
		if s.Weight < 1 || s.Weight > 256 || s.MaxFails < 0 || s.MaxFails > 100 || s.FailTimeoutSeconds < 1 || s.FailTimeoutSeconds > 3600 {
			return fmt.Errorf("servers[%d] has settings outside allowed limits", i)
		}
		if s.SortOrder < -100000 || s.SortOrder > 100000 {
			return fmt.Errorf("servers[%d].sort_order is outside allowed limits", i)
		}
		if s.Backup && s.Down {
			return fmt.Errorf("servers[%d] cannot be both backup and down", i)
		}
		if s.Backup && (req.Algorithm == "ip_hash" || req.Algorithm == "hash") {
			return fmt.Errorf("backup servers are not supported by %s", req.Algorithm)
		}
		if !s.Backup && !s.Down {
			activePrimary++
		}
	}
	if activePrimary == 0 {
		return fmt.Errorf("at least one server must be an active primary")
	}
	advanced, err := normalizeAdvancedDirectives(req.AdvancedDirectives)
	if err != nil {
		return err
	}
	req.AdvancedDirectives = advanced
	return nil
}

func validateAddress(address string) error {
	if address == "" || strings.HasPrefix(strings.ToLower(address), "unix:") {
		return fmt.Errorf("must be hostname:port, IPv4:port, or [IPv6]:port")
	}
	host, portText, err := net.SplitHostPort(address)
	if err != nil || host == "" {
		return fmt.Errorf("must include a valid host and port")
	}
	port, err := strconv.Atoi(portText)
	if err != nil || port < 1 || port > 65535 {
		return fmt.Errorf("port must be between 1 and 65535")
	}
	if ip := net.ParseIP(host); ip != nil {
		if strings.Contains(host, ":") && (!strings.HasPrefix(address, "[") || !strings.Contains(address, "]:")) {
			return fmt.Errorf("IPv6 addresses must use brackets")
		}
		return nil
	}
	if isNumericDottedHost(host) {
		return fmt.Errorf("invalid IPv4 address")
	}
	if !utf8.ValidString(host) || !isASCII(host) || !hostnameRE.MatchString(host) || strings.Contains(host, "..") {
		return fmt.Errorf("invalid hostname")
	}
	for _, label := range strings.Split(host, ".") {
		if label == "" || len(label) > 63 || label[0] == '-' || label[len(label)-1] == '-' {
			return fmt.Errorf("invalid hostname")
		}
	}
	return nil
}

func isNumericDottedHost(host string) bool {
	if !strings.Contains(host, ".") {
		return false
	}
	for i := 0; i < len(host); i++ {
		if (host[i] < '0' || host[i] > '9') && host[i] != '.' {
			return false
		}
	}
	return true
}

func normalizeAdvancedDirectives(input string) (string, error) {
	if len(input) > maxAdvancedBytes {
		return "", fmt.Errorf("advanced_directives exceeds %d bytes", maxAdvancedBytes)
	}
	statements, err := parseStatements(input)
	if err != nil {
		return "", fmt.Errorf("invalid advanced_directives: %w", err)
	}
	if len(statements) > maxAdvancedStatements {
		return "", fmt.Errorf("advanced_directives exceeds %d statements", maxAdvancedStatements)
	}
	var out strings.Builder
	for _, statement := range statements {
		tokens, err := tokenizeStatement(statement)
		if err != nil {
			return "", fmt.Errorf("invalid advanced_directives: %w", err)
		}
		if len(tokens) == 0 {
			return "", fmt.Errorf("invalid advanced_directives: empty statement")
		}
		directive := strings.ToLower(tokens[0])
		if directive == "include" {
			return "", fmt.Errorf("advanced_directives cannot contain include")
		}
		if _, managed := managedDirectives[directive]; managed {
			return "", fmt.Errorf("advanced_directives duplicates managed directive %s", tokens[0])
		}
		out.WriteString(strings.TrimSpace(statement))
		out.WriteString(";\n")
	}
	return out.String(), nil
}

func parseStatements(input string) ([]string, error) {
	var statements []string
	start := 0
	quote := byte(0)
	escaped := false
	for i := 0; i < len(input); i++ {
		c := input[i]
		if c == 0 {
			return nil, fmt.Errorf("NUL is not allowed")
		}
		if escaped {
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		switch c {
		case '#':
			return nil, fmt.Errorf("comments are not allowed")
		case '{', '}':
			return nil, fmt.Errorf("block braces are not allowed")
		case ';':
			statement := strings.TrimSpace(input[start:i])
			if statement == "" {
				return nil, fmt.Errorf("empty statement")
			}
			statements = append(statements, statement)
			start = i + 1
		}
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	if strings.TrimSpace(input[start:]) != "" {
		return nil, fmt.Errorf("every statement must end with a semicolon")
	}
	return statements, nil
}

func tokenizeStatement(statement string) ([]string, error) {
	var tokens []string
	var token strings.Builder
	quote := byte(0)
	escaped := false
	flush := func() {
		if token.Len() > 0 {
			tokens = append(tokens, token.String())
			token.Reset()
		}
	}
	for i := 0; i < len(statement); i++ {
		c := statement[i]
		if escaped {
			token.WriteByte(c)
			escaped = false
			continue
		}
		if c == '\\' {
			escaped = true
			continue
		}
		if quote != 0 {
			if c == quote {
				quote = 0
			} else {
				token.WriteByte(c)
			}
			continue
		}
		if c == '\'' || c == '"' {
			quote = c
			continue
		}
		if c == ' ' || c == '\t' || c == '\r' || c == '\n' {
			flush()
			continue
		}
		token.WriteByte(c)
	}
	if quote != 0 || escaped {
		return nil, fmt.Errorf("unterminated quote or escape")
	}
	flush()
	return tokens, nil
}

func validateDirectiveFragment(value string) error {
	if len(value) > 256 {
		return fmt.Errorf("value is too long")
	}
	if strings.Contains(value, ";") {
		return fmt.Errorf("semicolon is not allowed")
	}
	statements, err := parseStatements(value + ";")
	if err != nil || len(statements) != 1 {
		if err != nil {
			return err
		}
		return fmt.Errorf("invalid value")
	}
	return nil
}

func isASCII(value string) bool {
	for i := 0; i < len(value); i++ {
		if value[i] > 127 {
			return false
		}
	}
	return true
}
