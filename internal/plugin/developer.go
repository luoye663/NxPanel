package plugin

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"database/sql"
	"encoding/hex"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"
)

const developerModeSetting = "plugins.developer_mode"

var (
	ErrDeveloperModeDisabled       = errors.New("developer mode is disabled")
	ErrRiskAcknowledgementRequired = errors.New("developer mode risk acknowledgement is required")
	ErrDeveloperTokenInvalid       = errors.New("developer package token is invalid or expired")
	ErrOfficialIDReserved          = errors.New("plugin id is reserved by the official repository")
)

type DeveloperInspection struct {
	UploadToken        string    `json:"upload_token"`
	ExpiresAt          time.Time `json:"expires_at"`
	Filename           string    `json:"filename"`
	PackageSHA256      string    `json:"package_sha256"`
	Manifest           *Manifest `json:"manifest"`
	Unverified         bool      `json:"unverified"`
	VerificationStatus string    `json:"verification_status"`
}

type stagedDeveloperPackage struct {
	inspection  DeveloperInspection
	root        string
	packagePath string
	unpacked    string
	expiryTimer *time.Timer
}

type DeveloperManager struct {
	db      *sql.DB
	root    string
	catalog Catalog
	runtime Runtime
	now     func() time.Time
	mu      sync.Mutex
	staged  map[string]*stagedDeveloperPackage
}

func newDeveloperManager(db *sql.DB, pluginRoot string, catalog Catalog, runtime Runtime) *DeveloperManager {
	root := filepath.Join(pluginRoot, ".developer-staging")
	// Upload tokens intentionally do not survive a process restart.
	_ = os.RemoveAll(root)
	_ = os.MkdirAll(root, 0o700)
	return &DeveloperManager{db: db, root: root, catalog: catalog, runtime: runtime, now: time.Now, staged: make(map[string]*stagedDeveloperPackage)}
}

func (d *DeveloperManager) Enabled(ctx context.Context) (bool, error) {
	var value string
	err := d.db.QueryRowContext(ctx, `SELECT value FROM settings WHERE key=?`, developerModeSetting).Scan(&value)
	if errors.Is(err, sql.ErrNoRows) {
		return false, nil
	}
	return value == "true", err
}

func (d *DeveloperManager) SetEnabled(ctx context.Context, enabled, acknowledged bool) error {
	if enabled && !acknowledged {
		return ErrRiskAcknowledgementRequired
	}
	value := "false"
	if enabled {
		value = "true"
	}
	_, err := d.db.ExecContext(ctx, `INSERT INTO settings(key,value,updated_at) VALUES(?,?,?)
		ON CONFLICT(key) DO UPDATE SET value=excluded.value, updated_at=excluded.updated_at`, developerModeSetting, value, d.now().UTC().Format(time.RFC3339))
	return err
}

func (d *DeveloperManager) requireEnabled(ctx context.Context) error {
	enabled, err := d.Enabled(ctx)
	if err != nil {
		return err
	}
	if !enabled {
		return ErrDeveloperModeDisabled
	}
	return nil
}

func (d *DeveloperManager) Inspect(ctx context.Context, filename string, src io.Reader) (*DeveloperInspection, error) {
	if err := d.requireEnabled(ctx); err != nil {
		return nil, err
	}
	if filepath.Base(filename) != filename || !strings.HasSuffix(strings.ToLower(filename), ".nxp") {
		return nil, errors.New("developer package must be a local .nxp file")
	}
	if err := os.MkdirAll(d.root, 0o700); err != nil {
		return nil, err
	}
	tokenBytes := make([]byte, 32)
	if _, err := rand.Read(tokenBytes); err != nil {
		return nil, err
	}
	token := hex.EncodeToString(tokenBytes)
	stageRoot := filepath.Join(d.root, token)
	if err := os.Mkdir(stageRoot, 0o700); err != nil {
		return nil, err
	}
	keep := false
	defer func() {
		if !keep {
			_ = os.RemoveAll(stageRoot)
		}
	}()
	packagePath := filepath.Join(stageRoot, "plugin.nxp")
	out, err := os.OpenFile(packagePath, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return nil, err
	}
	written, copyErr := io.Copy(out, io.LimitReader(src, DefaultPackageLimits.CompressedBytes+1))
	closeErr := out.Close()
	if copyErr != nil || closeErr != nil {
		return nil, errors.Join(copyErr, closeErr)
	}
	if written > DefaultPackageLimits.CompressedBytes {
		return nil, errors.New("plugin package exceeds compressed size limit")
	}
	unpacked := filepath.Join(stageRoot, "unpacked")
	manifest, err := VerifyPackage(ctx, packagePath, "", unpacked, DefaultPackageLimits)
	if err != nil {
		return nil, err
	}
	if strings.HasPrefix(manifest.ID, "org.nxpanel.") {
		return nil, ErrOfficialIDReserved
	}
	if err := d.checkOfficialID(ctx, manifest.ID); err != nil {
		return nil, err
	}
	validateCtx, cancel := context.WithTimeout(ctx, 30*time.Second)
	err = d.runtime.Validate(validateCtx, manifest, filepath.Join(unpacked, filepath.FromSlash(manifest.Backend)))
	cancel()
	if err != nil {
		return nil, err
	}
	hash, err := fileSHA256(packagePath)
	if err != nil {
		return nil, err
	}
	inspection := DeveloperInspection{UploadToken: token, ExpiresAt: d.now().Add(15 * time.Minute).UTC(), Filename: filename, PackageSHA256: hash, Manifest: manifest, Unverified: true, VerificationStatus: "developer_unverified"}
	d.mu.Lock()
	d.cleanupExpiredLocked()
	staged := &stagedDeveloperPackage{inspection: inspection, root: stageRoot, packagePath: packagePath, unpacked: unpacked}
	staged.expiryTimer = time.AfterFunc(15*time.Minute, func() { d.discard(token) })
	d.staged[token] = staged
	d.mu.Unlock()
	keep = true
	return &inspection, nil
}

func (d *DeveloperManager) checkOfficialID(ctx context.Context, id string) error {
	if d.catalog == nil {
		return errors.New("official catalog is unavailable; cannot verify plugin id")
	}
	entries, err := d.catalog.List(ctx)
	if err != nil {
		return fmt.Errorf("official catalog is unavailable; cannot verify plugin id: %w", err)
	}
	for _, entry := range entries {
		if entry.ID == id {
			return ErrOfficialIDReserved
		}
	}
	return nil
}

func (d *DeveloperManager) discard(token string) {
	d.mu.Lock()
	staged := d.staged[token]
	delete(d.staged, token)
	d.mu.Unlock()
	if staged != nil {
		if staged.expiryTimer != nil {
			staged.expiryTimer.Stop()
		}
		_ = os.RemoveAll(staged.root)
	}
}

func (d *DeveloperManager) take(ctx context.Context, token string) (*stagedDeveloperPackage, error) {
	if err := d.requireEnabled(ctx); err != nil {
		return nil, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	d.cleanupExpiredLocked()
	staged := d.staged[token]
	if staged == nil {
		return nil, ErrDeveloperTokenInvalid
	}
	delete(d.staged, token) // single use, including failed install attempts
	if staged.expiryTimer != nil {
		staged.expiryTimer.Stop()
	}
	return staged, nil
}

func (d *DeveloperManager) cleanupExpiredLocked() {
	now := d.now()
	for token, staged := range d.staged {
		if !now.Before(staged.inspection.ExpiresAt) {
			delete(d.staged, token)
			if staged.expiryTimer != nil {
				staged.expiryTimer.Stop()
			}
			_ = os.RemoveAll(staged.root)
		}
	}
}

func rehashPackage(path string) (string, error) {
	f, err := os.Open(path)
	if err != nil {
		return "", err
	}
	defer f.Close()
	h := sha256.New()
	if _, err := io.Copy(h, f); err != nil {
		return "", err
	}
	return hex.EncodeToString(h.Sum(nil)), nil
}
