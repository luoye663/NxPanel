package trust

import (
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"
)

const GitHubSource = "https://raw.githubusercontent.com/luoye663/NxPanel/main/" + Path

func ReadFile(path string, limit int64) ([]byte, error) {
	file, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, limit+1))
	if len(body) > int(limit) {
		return nil, errors.New("trust file exceeds size limit")
	}
	return body, err
}

func ReadDir(dir string) (Signed, error) {
	policy, err := ReadFile(filepath.Join(dir, Name), MaxPolicy)
	if err != nil {
		return Signed{}, err
	}
	signature, err := ReadFile(filepath.Join(dir, Signature), MaxSig)
	return Signed{Policy: policy, Signature: signature}, err
}

func CurrentDir(dir, root string, now time.Time) (Policy, error) {
	signed, err := ReadDir(dir)
	if err != nil {
		return Policy{}, err
	}
	policy, _, err := signed.Verify(root, Floor{}, now)
	if err != nil {
		return Policy{}, err
	}
	return policy, policy.RequireCurrent(now)
}
