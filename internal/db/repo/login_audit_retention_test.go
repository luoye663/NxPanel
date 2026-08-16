package repo

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestLoginAuditPruneAgeCountAndDeterministicTie(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	now := time.Date(2026, 7, 26, 4, 0, 0, 0, time.UTC)
	rows := []struct {
		username string
		created  time.Time
	}{
		{"old", now.Add(-48 * time.Hour)},
		{"new-a", now.Add(-time.Hour)},
		{"new-b", now.Add(-time.Hour)},
		{"new-c", now.Add(-time.Hour)},
	}
	for _, row := range rows {
		if _, err := database.Exec(`INSERT INTO login_audit (username, ip, user_agent, created_at) VALUES (?, 'ip', 'ua', ?)`, row.username, row.created.Format(time.RFC3339)); err != nil {
			t.Fatalf("insert login audit: %v", err)
		}
	}

	deleted, err := NewLoginAuditRepo(database).Prune(context.Background(), now.Add(-24*time.Hour), 2)
	if err != nil {
		t.Fatalf("Prune() error = %v", err)
	}
	if deleted != 2 {
		t.Fatalf("Prune() deleted = %d, want 2", deleted)
	}
	items, total, err := NewLoginAuditRepo(database).List(1, 10)
	if err != nil {
		t.Fatalf("List() error = %v", err)
	}
	if total != 2 || items[0].Username != "new-c" || items[1].Username != "new-b" {
		t.Fatalf("remaining audits = %#v, total=%d", items, total)
	}
}

func TestLoginAuditRecordBoundsPersistedFields(t *testing.T) {
	database := setupTestDB(t)
	defer database.Close()
	r := NewLoginAuditRepo(database)
	if err := r.Record(&LoginAudit{
		Username:      strings.Repeat("u", LoginAuditUsernameMaxBytes+20),
		IP:            strings.Repeat("i", LoginAuditIPMaxBytes+20),
		UserAgent:     strings.Repeat("a", LoginAuditUAMaxBytes+20),
		FailureReason: strings.Repeat("r", LoginAuditReasonMaxBytes+20),
	}); err != nil {
		t.Fatalf("Record() error = %v", err)
	}
	var username, ip, ua, reason string
	if err := database.QueryRow(`SELECT username, ip, user_agent, failure_reason FROM login_audit`).Scan(&username, &ip, &ua, &reason); err != nil {
		t.Fatalf("query audit: %v", err)
	}
	if len(username) != LoginAuditUsernameMaxBytes || len(ip) != LoginAuditIPMaxBytes || len(ua) != LoginAuditUAMaxBytes || len(reason) != LoginAuditReasonMaxBytes {
		t.Fatalf("persisted lengths username=%d ip=%d ua=%d reason=%d", len(username), len(ip), len(ua), len(reason))
	}
}
