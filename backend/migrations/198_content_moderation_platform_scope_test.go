package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestContentModerationPlatformScopeMigrationPreservesHistoryWithoutRevivingGroups(t *testing.T) {
	path := filepath.Join("198_content_moderation_platform_scope.sql")
	sql, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	source := strings.ToLower(string(sql))
	for _, fragment := range []string{
		"add column if not exists platform_id",
		"legacy_group_id",
		"platform_ids",
		"drop column if exists group_id",
		"drop column if exists group_name",
		"idx_content_moderation_logs_platform_created_at",
	} {
		if !strings.Contains(source, strings.ToLower(fragment)) {
			t.Fatalf("migration is missing %q", fragment)
		}
	}
	if strings.Contains(source, "drop table content_moderation_logs") {
		t.Fatal("content moderation migration must preserve audit history")
	}
	if strings.Contains(source, "insert into platforms") {
		t.Fatal("content moderation history must not revive legacy groups as platforms")
	}
	if strings.Contains(source, "raise exception") {
		t.Fatal("unmapped legacy moderation scope must not block one-way cleanup")
	}
}
