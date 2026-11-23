package trust

import (
	"bytes"
	"crypto/ed25519"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"regexp"
	"strings"
	"time"
	"unicode"
	"unicode/utf8"
)

const (
	Path       = "catalog/trust/"
	Name       = "trust.json"
	Signature  = "trust.sig"
	MaxPolicy  = 128 << 10
	MaxSig     = 512
	MaxModules = 32
	Lifetime   = 180 * 24 * time.Hour
	purpose    = "nxpanel-trust-policy/v1\x00"
)

var RootPublicKey string

var (
	keyIDPattern = regexp.MustCompile(`^[a-zA-Z0-9][a-zA-Z0-9_.-]{0,63}$`)
	moduleID     = regexp.MustCompile(`^[a-z][a-z0-9_-]{0,39}$`)
	symbol       = regexp.MustCompile(`^ngx_[a-zA-Z0-9_]{1,100}_module$`)
	repository   = regexp.MustCompile(`^[A-Za-z0-9](?:[A-Za-z0-9-]{0,37}[A-Za-z0-9])?/[A-Za-z0-9_][A-Za-z0-9_.-]{0,99}$`)
)

type Signed struct {
	Policy    []byte `json:"policy"`
	Signature []byte `json:"signature"`
}

type Floor struct {
	Sequence uint64
	Digest   string
}

type Policy struct {
	Version   int       `json:"version"`
	Sequence  uint64    `json:"sequence"`
	IssuedAt  time.Time `json:"issued_at"`
	ExpiresAt time.Time `json:"expires_at"`
	Update    string    `json:"update_keys"`
	Build     string    `json:"build_keys"`
	Modules   []Module  `json:"modules"`
}

type Module struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Description string `json:"description"`
	Repository  string `json:"repository"`
	Symbol      string `json:"symbol"`
	Keys        string `json:"keys"`
}

func Keys(value string) (map[string]ed25519.PublicKey, error) {
	keys := make(map[string]ed25519.PublicKey)
	if value == "" {
		return keys, nil
	}
	entries := strings.Split(value, ",")
	if len(entries) > 8 {
		return nil, errors.New("too many trust keys")
	}
	for _, entry := range entries {
		id, encoded, _ := strings.Cut(entry, ":")
		key, err := base64.StdEncoding.Strict().DecodeString(encoded)
		if !keyIDPattern.MatchString(id) || err != nil || len(key) != ed25519.PublicKeySize || keys[id] != nil || base64.StdEncoding.EncodeToString(key) != encoded {
			return nil, errors.New("invalid trust public key")
		}
		keys[id] = key
	}
	return keys, nil
}

func PrivateKey(value string) (string, ed25519.PrivateKey, error) {
	id, encoded, _ := strings.Cut(strings.TrimSpace(value), ":")
	seed, err := base64.StdEncoding.Strict().DecodeString(encoded)
	if !keyIDPattern.MatchString(id) || err != nil || len(seed) != ed25519.SeedSize || base64.StdEncoding.EncodeToString(seed) != encoded {
		return "", nil, errors.New("invalid signing seed")
	}
	return id, ed25519.NewKeyFromSeed(seed), nil
}

func AuthorizedPrivate(value, public string) (string, ed25519.PrivateKey, error) {
	id, key, err := PrivateKey(value)
	keys, keyErr := Keys(public)
	if err != nil || keyErr != nil || !key.Public().(ed25519.PublicKey).Equal(keys[id]) {
		return "", nil, errors.New("signing key not authorized by trust policy")
	}
	return id, key, nil
}

func rootKey(root string) (ed25519.PublicKey, error) {
	keys, err := Keys(root)
	if err == nil && len(keys) == 1 {
		for _, key := range keys {
			return key, nil
		}
	}
	return nil, errors.New("exactly one NxPanel root public key is required")
}

func (signed Signed) Verify(root string, floor Floor, now time.Time) (Policy, Floor, error) {
	key, err := rootKey(root)
	if err != nil {
		return Policy{}, floor, err
	}
	signature, err := base64.StdEncoding.Strict().DecodeString(strings.TrimSpace(string(signed.Signature)))
	if len(signed.Policy) == 0 || len(signed.Policy) > MaxPolicy || len(signed.Signature) > MaxSig || err != nil || !ed25519.Verify(key, message(signed.Policy), signature) {
		return Policy{}, floor, errors.New("NxPanel trust policy signature is invalid")
	}
	policy, err := validate(signed.Policy, key)
	if err != nil {
		return Policy{}, floor, err
	}
	if now.Before(policy.IssuedAt) {
		return Policy{}, floor, errors.New("NxPanel trust policy is not yet valid")
	}
	next := Floor{Sequence: policy.Sequence, Digest: fmt.Sprintf("%x", sha256.Sum256(signed.Policy))}
	if next.Sequence < floor.Sequence || next.Sequence == floor.Sequence && next.Digest != floor.Digest {
		return Policy{}, floor, errors.New("NxPanel trust policy rollback or conflicting sequence")
	}
	return policy, next, nil
}

func Sign(raw []byte, private ed25519.PrivateKey) (Signed, error) {
	if len(raw) == 0 || len(raw) > MaxPolicy || len(private) != ed25519.PrivateKeySize {
		return Signed{}, errors.New("invalid policy size or root signing key")
	}
	if _, err := validate(raw, private.Public().(ed25519.PublicKey)); err != nil {
		return Signed{}, err
	}
	return Signed{Policy: raw, Signature: []byte(base64.StdEncoding.EncodeToString(ed25519.Sign(private, message(raw))) + "\n")}, nil
}

func WithDefaultValidity(raw []byte, now time.Time) ([]byte, error) {
	policy, err := decodePolicy(raw)
	if err != nil {
		return nil, err
	}
	if !policy.IssuedAt.IsZero() && !policy.ExpiresAt.IsZero() {
		return raw, nil
	}
	if policy.IssuedAt.IsZero() {
		policy.IssuedAt = now.UTC().Truncate(time.Second)
	}
	if policy.ExpiresAt.IsZero() {
		policy.ExpiresAt = policy.IssuedAt.Add(Lifetime)
	}
	return json.MarshalIndent(policy, "", "  ")
}

func message(raw []byte) []byte { return append([]byte(purpose), raw...) }

func (policy Policy) RequireCurrent(now time.Time) error {
	if now.Before(policy.IssuedAt) || !now.Before(policy.ExpiresAt) {
		return errors.New("NxPanel trust policy is not currently valid")
	}
	return nil
}

func (policy Policy) Module(id string) (Module, bool) {
	for _, module := range policy.Modules {
		if module.ID == id {
			return module, true
		}
	}
	return Module{}, false
}

func decodePolicy(raw []byte) (Policy, error) {
	var policy Policy
	if !utf8.Valid(raw) || !uniqueJSON(json.NewDecoder(bytes.NewReader(raw)), 0) {
		return Policy{}, errors.New("invalid or ambiguous trust policy JSON")
	}
	decoder := json.NewDecoder(bytes.NewReader(raw))
	decoder.DisallowUnknownFields()
	if decoder.Decode(&policy) != nil || decoder.Decode(new(json.RawMessage)) != io.EOF {
		return Policy{}, errors.New("invalid NxPanel trust policy JSON")
	}
	return policy, nil
}

func validate(raw []byte, root ed25519.PublicKey) (Policy, error) {
	policy, err := decodePolicy(raw)
	if err != nil {
		return Policy{}, err
	}
	if policy.Version != 1 || policy.Sequence == 0 ||
		policy.IssuedAt.IsZero() || !policy.ExpiresAt.After(policy.IssuedAt) || policy.ExpiresAt.Sub(policy.IssuedAt) > Lifetime || policy.Modules == nil || len(policy.Modules) > MaxModules {
		return Policy{}, errors.New("invalid NxPanel trust policy")
	}
	seenKeys := map[string]bool{string(root): true}
	addKeys := func(value string, required bool) bool {
		keys, err := Keys(value)
		if err != nil || required && len(keys) == 0 {
			return false
		}
		for _, key := range keys {
			if seenKeys[string(key)] {
				return false
			}
			seenKeys[string(key)] = true
		}
		return true
	}
	if !addKeys(policy.Update, false) || !addKeys(policy.Build, false) {
		return Policy{}, errors.New("invalid or reused update or build key")
	}
	ids, repos, symbols := map[string]bool{}, map[string]bool{}, map[string]bool{}
	for _, module := range policy.Modules {
		repo := strings.ToLower(module.Repository)
		if !moduleID.MatchString(module.ID) || !repository.MatchString(module.Repository) || !symbol.MatchString(module.Symbol) ||
			(module.ID == "geoip2") != (module.Symbol == "ngx_http_geoip2_module") || !validText(module.Name, 128) || !validText(module.Description, 768) ||
			ids[module.ID] || repos[repo] || symbols[module.Symbol] || !addKeys(module.Keys, true) {
			return Policy{}, fmt.Errorf("invalid or duplicate module authorization: %q", module.ID)
		}
		ids[module.ID], repos[repo], symbols[module.Symbol] = true, true, true
	}
	return policy, nil
}

func validText(value string, limit int) bool {
	return value != "" && strings.TrimSpace(value) == value && len(value) <= limit && strings.IndexFunc(value, func(char rune) bool {
		return unicode.IsControl(char) || unicode.Is(unicode.Cf, char)
	}) == -1
}

func uniqueJSON(decoder *json.Decoder, depth int) bool {
	if depth > 12 {
		return false
	}
	token, err := decoder.Token()
	if err != nil {
		return false
	}
	delim, container := token.(json.Delim)
	if !container {
		return true
	}
	seen := make(map[string]bool)
	for decoder.More() {
		if delim == '{' {
			token, err := decoder.Token()
			key, ok := token.(string)
			key = strings.ToLower(key)
			if err != nil || !ok || seen[key] {
				return false
			}
			seen[key] = true
		}
		if !uniqueJSON(decoder, depth+1) {
			return false
		}
	}
	_, err = decoder.Token()
	return err == nil
}
