package main

import (
	"bytes"
	"crypto/ed25519"
	"fmt"
	"math"
	"os"
	"path/filepath"
	"time"

	"github.com/luoye663/nxpanel/internal/nginxmodule"
	"github.com/luoye663/nxpanel/internal/update"
)

func indexTrust() (map[string]ed25519.PublicKey, map[string]ed25519.PublicKey, error) {
	updateValue := os.Getenv("NXPANEL_UPDATE_TRUST_KEYS")
	trust, err := nginxmodule.Keys(os.Getenv("NXPANEL_MODULE_TRUST_KEYS"), updateValue)
	if err != nil {
		return nil, nil, err
	}
	updateKeys, err := update.Keys(updateValue)
	return trust, updateKeys, err
}

func indexFloor(dir string, trust, updateKeys map[string]ed25519.PublicKey, bootstrap bool) (nginxmodule.Floor, error) {
	raw, err := readBuildFile(filepath.Join(dir, nginxmodule.IndexName), nginxmodule.MaxEnvelope)
	if err != nil {
		return nginxmodule.Floor{}, err
	}
	signature, err := readBuildFile(filepath.Join(dir, nginxmodule.IndexSigName), nginxmodule.MaxEnvelope)
	if bootstrap && os.IsNotExist(err) {
		var entries []nginxmodule.IndexEntry
		if update.Decode(bytes.NewReader(raw), &entries) == nil && entries != nil && len(entries) == 0 {
			return nginxmodule.Floor{}, nil
		}
		return nginxmodule.Floor{}, fmt.Errorf("an unsigned base index must be an empty JSON array")
	}
	if err != nil {
		return nginxmodule.Floor{}, err
	}
	_, floor, err := nginxmodule.VerifyIndex(raw, signature, trust, updateKeys, nginxmodule.Floor{}, false)
	return floor, err
}

func verifyIndex(input, base string) error {
	trust, updateKeys, err := indexTrust()
	if err != nil {
		return err
	}
	proposed, err := indexFloor(input, trust, updateKeys, false)
	if err != nil || base == "" {
		return err
	}
	current, err := indexFloor(base, trust, updateKeys, true)
	if err != nil {
		return fmt.Errorf("invalid main index: %w", err)
	}
	if proposed.Sequence <= current.Sequence {
		return fmt.Errorf("index sequence %d must exceed main's sequence %d; sign the PR again", proposed.Sequence, current.Sequence)
	}
	return nil
}

func signIndex(input, output, base string, sequence uint64) error {
	trust, updateKeys, err := indexTrust()
	if err != nil {
		return err
	}
	if base != "" {
		current, err := indexFloor(base, trust, updateKeys, true)
		if err != nil {
			return fmt.Errorf("invalid main index: %w", err)
		}
		if current.Sequence == math.MaxUint64 {
			return fmt.Errorf("main index sequence cannot be incremented")
		}
		sequence = max(uint64(time.Now().Unix()), current.Sequence+1)
	}
	keys, err := signingKeys(trust, "NXPANEL_MODULE_SIGNING_KEYS")
	if err != nil {
		return err
	}
	raw, err := readBuildFile(input, nginxmodule.MaxEnvelope)
	if err != nil {
		return err
	}
	files, err := signedIndex(raw, sequence, keys, trust, updateKeys, false)
	if err != nil {
		return err
	}
	return writeFiles(output, files)
}
