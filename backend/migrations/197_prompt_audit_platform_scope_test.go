package migrations

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestPromptAuditPlatformScopeMigrationDropsLegacyGroupColumns(t *testing.T) {
	content, err := os.ReadFile(filepath.Join("197_prompt_audit_platform_scope.sql"))
	if err != nil {
		t.Fatalf("read migration: %v", err)
	}
	sql := strings.ToLower(string(content))
	if !strings.Contains(sql, "platform_id") || !strings.Contains(sql, "platform_name") {
		t.Fatal("migration must add platform snapshots")
	}
	if !strings.Contains(sql, "drop column if exists group_id") || !strings.Contains(sql, "drop column if exists group_name") {
		t.Fatal("migration must remove legacy group snapshot columns")
	}
}
