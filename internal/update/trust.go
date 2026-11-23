package update

import (
	"bytes"
	"encoding/json"
	"errors"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/luoye663/nxpanel/internal/trust"
)

const trustRecordMax = 2 * (trust.MaxPolicy + trust.MaxSig)

type preparedTrust struct {
	ID        string `json:"id"`
	Manifest  []byte `json:"manifest"`
	Signature []byte `json:"signature"`
}

func (u *Updater) readTrustRecord(name string, target any) error {
	file, err := os.Open(filepath.Join(u.private(), name))
	if err != nil {
		return err
	}
	defer file.Close()
	body, err := io.ReadAll(io.LimitReader(file, trustRecordMax+1))
	if err != nil || len(body) > trustRecordMax {
		return errors.New("cannot read updater trust record")
	}
	return Decode(bytes.NewReader(body), target)
}

func (u *Updater) writeTrustRecord(name string, data any) error {
	return replaceFile(u.private(), name, 0600, func(file *os.File) error {
		return json.NewEncoder(file).Encode(data)
	})
}

func (u *Updater) trustedPolicy(now time.Time) (trust.Policy, trust.Floor, error) {
	var signed trust.Signed
	if err := u.readTrustRecord("trust.json", &signed); err != nil {
		return trust.Policy{}, trust.Floor{}, err
	}
	return signed.Verify(ActiveRoot(), trust.Floor{}, now)
}

func (u *Updater) acceptTrust(signed trust.Signed, now time.Time) error {
	_, floor, err := u.trustedPolicy(now)
	if err != nil && !errors.Is(err, os.ErrNotExist) {
		return err
	}
	_, next, err := signed.Verify(ActiveRoot(), floor, now)
	if err != nil || next == floor {
		return err
	}
	return u.writeTrustRecord("trust.json", signed)
}

func (u *Updater) verifyWithTrust(raw, signature []byte, now time.Time) (Manifest, Asset, error) {
	policy, _, err := u.trustedPolicy(now)
	if err != nil {
		return Manifest{}, Asset{}, err
	}
	return VerifyWithPolicy(raw, signature, policy, now)
}

func VerifyWithPolicy(raw, signature []byte, policy trust.Policy, now time.Time) (Manifest, Asset, error) {
	if err := policy.RequireCurrent(now); err != nil {
		return Manifest{}, Asset{}, err
	}
	keys, err := trust.Keys(policy.Update)
	if err != nil {
		return Manifest{}, Asset{}, err
	}
	return Verify(raw, signature, keys)
}

func (u *Updater) verifyPrepared(state State) error {
	var prepared preparedTrust
	if err := u.readTrustRecord("prepared.json", &prepared); err != nil || prepared.ID != state.ID {
		return errors.New("missing or invalid staged update authorization")
	}
	manifest, _, err := u.verifyWithTrust(prepared.Manifest, prepared.Signature, time.Now())
	if err != nil || manifest.Version != state.Version {
		return errors.New("staged update is no longer authorized")
	}
	return nil
}
