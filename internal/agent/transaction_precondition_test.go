package agent

import (
	"bytes"
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"github.com/luoye663/nxpanel/internal/app"
	"github.com/luoye663/nxpanel/internal/nginxpatch"
)

func TestTransactionPreconditionsBeforeAnyWrite(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.conf")
	if err := os.WriteFile(path, []byte("current"), 0640); err != nil {
		t.Fatal(err)
	}
	policy := NewPathPolicy([]string{dir})
	tx, err := NewTransaction("preconditions", filepath.Join(dir, "backups"), policy, "", "")
	if err != nil {
		t.Fatal(err)
	}
	aux := filepath.Join(dir, "new-key")
	stale := fmt.Sprintf("%x", sha256.Sum256([]byte("old")))
	err = tx.Apply(context.Background(), []FileChange{{Type: "write", Path: aux, Content: []byte("secret")}, {Type: "write", Path: path, Content: []byte("new"), ExpectedHash: stale}})
	if !errors.Is(err, ErrFileConflict) {
		t.Fatalf("expected conflict, got %v", err)
	}
	if _, err := os.Stat(aux); !os.IsNotExist(err) {
		t.Fatal("auxiliary file written before precondition validation")
	}
	if len(tx.Backups) != 0 {
		t.Fatal("backups created before preconditions")
	}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("current")))
	if err := tx.Apply(context.Background(), []FileChange{{Type: "write", Path: path, Content: []byte("new"), ExpectedHash: hash, Perm: 0640}}); err != nil {
		t.Fatal(err)
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	data, _ := os.ReadFile(path)
	info, _ := os.Stat(path)
	if string(data) != "current" || info.Mode().Perm() != 0640 {
		t.Fatal("rollback changed content or permissions")
	}
}

func TestTransactionMaintenanceGuardAndReadOnlyChecks(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.conf")
	key := filepath.Join(dir, "private.key")
	base := `server { server_name example.com; ssl_certificate /old/cert; ssl_certificate_key /old/key; }`
	on, err := nginxpatch.SetMaintenance(base, "example.com", nil, "site", filepath.Join(dir, "page.html"), true)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(on), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(key, []byte("private material"), 0600); err != nil {
		t.Fatal(err)
	}
	tx, err := NewTransaction("maintenance", filepath.Join(dir, "backups"), NewPathPolicy([]string{dir}), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(context.Background(), []FileChange{{Type: "remove", Path: path}}); err == nil {
		t.Fatal("removed active maintenance config")
	}
	if err := tx.Apply(context.Background(), []FileChange{{Type: "write", Path: path, Content: []byte(base), ExpectedHash: nginxpatch.Hash(on)}}); err == nil {
		t.Fatal("removed maintenance without explicit transition")
	}
	updated, err := nginxpatch.ReplaceCertificate(on, "example.com", nginxpatch.Hash(on), "/new/cert", "/new/key")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(context.Background(), []FileChange{{Type: "check", Path: key, ExpectedHash: nginxpatch.Hash("private material")}, {Type: "write", Path: path, Content: []byte(updated), ExpectedHash: nginxpatch.Hash(on)}}); err != nil {
		t.Fatal(err)
	}
	if len(tx.Backups) != 1 || tx.Backups[0].FilePath != path {
		t.Fatal("read-only source check backed up private key")
	}
	info, err := os.Stat(path)
	if err != nil || info.Mode().Perm() != 0600 {
		t.Fatal("config permissions changed")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	b, _ := os.ReadFile(path)
	if string(b) != on {
		t.Fatal("rollback did not restore active maintenance config")
	}
}

func TestHashCheckedAliasPatchPreservesSymlinkAndWhitelist(t *testing.T) {
	dir := t.TempDir()
	target := filepath.Join(dir, "source.conf")
	link := filepath.Join(dir, "enabled.conf")
	if err := os.WriteFile(target, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(target, link); err != nil {
		t.Fatal(err)
	}
	tx, err := NewTransaction("alias", filepath.Join(dir, "backups"), NewPathPolicy([]string{dir}), "", "")
	if err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(context.Background(), []FileChange{{Type: "write", Path: link, ExpectedHash: nginxpatch.Hash("old"), FollowSymlink: true, Content: []byte("new")}}); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(link)
	if err != nil || info.Mode()&os.ModeSymlink == 0 {
		t.Fatal("patch switched the enabled alias")
	}
	actual, _ := os.ReadFile(target)
	if string(actual) != "new" {
		t.Fatal("did not patch actual config target")
	}
	if len(tx.Backups) != 1 || tx.Backups[0].FilePath != target {
		t.Fatal("did not back up actual config target")
	}
	if err := tx.Rollback(context.Background()); err != nil {
		t.Fatal(err)
	}
	actual, _ = os.ReadFile(link)
	if string(actual) != "old" {
		t.Fatal("alias rollback did not preserve original target")
	}
	outside := filepath.Join(t.TempDir(), "outside.conf")
	if err := os.WriteFile(outside, []byte("external"), 0600); err != nil {
		t.Fatal(err)
	}
	escaped := filepath.Join(dir, "escape.conf")
	if err := os.Symlink(outside, escaped); err != nil {
		t.Fatal(err)
	}
	if err := tx.Apply(context.Background(), []FileChange{{Type: "write", Path: escaped, ExpectedHash: nginxpatch.Hash("external"), FollowSymlink: true, Content: []byte("bad")}}); err == nil {
		t.Fatal("alias following expanded the whitelist")
	}
	actual, _ = os.ReadFile(outside)
	if string(actual) != "external" {
		t.Fatal("outside target changed")
	}
}

func TestConcurrentTransactionPrecondition(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "site.conf")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	s := &Server{cfg: &app.Config{Nginx: app.NginxConfig{PanelDir: dir}}, policy: NewPathPolicy([]string{dir})}
	hash := fmt.Sprintf("%x", sha256.Sum256([]byte("old")))
	start := make(chan struct{})
	results := make(chan int, 2)
	var wg sync.WaitGroup
	for i := 0; i < 2; i++ {
		wg.Add(1)
		go func(i int) {
			defer wg.Done()
			<-start
			body, _ := json.Marshal(TransactionRequest{OperationID: fmt.Sprintf("concurrent_%d", i), Changes: []FileChange{{Type: "write", Path: path, ExpectedHash: hash, ContentBase64: base64.StdEncoding.EncodeToString([]byte(fmt.Sprintf("new_%d", i)))}}})
			rec := httptest.NewRecorder()
			s.handleTransactionApply(rec, httptest.NewRequest("POST", "/", bytes.NewReader(body)))
			results <- rec.Code
		}(i)
	}
	close(start)
	wg.Wait()
	close(results)
	counts := make(map[int]int)
	for code := range results {
		counts[code]++
	}
	if counts[http.StatusOK] != 1 || counts[http.StatusConflict] != 1 {
		t.Fatalf("concurrent statuses: %v", counts)
	}
}

func TestTransactionHashRejectsSymlinkAndInvalidHash(t *testing.T) {
	dir := t.TempDir()
	path := filepath.Join(dir, "file")
	link := filepath.Join(dir, "link")
	if err := os.WriteFile(path, []byte("old"), 0600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(path, link); err != nil {
		t.Fatal(err)
	}
	tx, err := NewTransaction("hash", filepath.Join(dir, "backups"), NewPathPolicy([]string{dir}), "", "")
	if err != nil {
		t.Fatal(err)
	}
	for _, ch := range []FileChange{
		{Type: "write", Path: path, ExpectedHash: "malformed"},
		{Type: "write", Path: link, ExpectedHash: fmt.Sprintf("%x", sha256.Sum256([]byte("old")))},
		{Type: "remove", Path: path, ExpectedHash: fmt.Sprintf("%x", sha256.Sum256([]byte("old")))},
	} {
		if err := tx.Apply(context.Background(), []FileChange{ch}); err == nil {
			t.Fatal("accepted invalid precondition")
		}
	}
}
