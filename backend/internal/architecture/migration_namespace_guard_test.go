//go:build unit

package architecture

import (
	"crypto/sha256"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"runtime"
	"sort"
	"strconv"
	"strings"
	"testing"
)

var migrationFilePattern = regexp.MustCompile(`^(\d+)[a-z]?_.*\.sql$`)

var frozenMigrationChecksums = map[string]string{
	"192_subscription_billing_redesign.sql":          "e4bff3777da6eef4673064951d706450ac1cc9777bf3844035e14e559bf6ecd1",
	"193_subscription_redeem_code_plan_snapshot.sql": "2c9997f315497cbea376c16c76ab9680aabdb65b186498e89895ba77817ec1f3",
	"194_platform_assets_expand.sql":                 "3adcb8eae04cc2b22af5c978ba6077bb7f4e064f82592bc8ccedcf36eb1b0c90",
	"195_platform_endpoint_capabilities.sql":         "8315ba2a0dbe34853eff250e383aad381fc7a212a70053ac2b4e18b635807e83",
	"196_model_pricing_overrides.sql":                "f911b0bccdacb9388bc5f716b99d9cf43d1c1b0de2eeb8762d71e2737dfd63f3",
	"197_prompt_audit_platform_scope.sql":            "94b54cc3e799b800e57980a7da78ad0ef6f48814d428e3f33e67eaf94bb348d1",
	"198_content_moderation_platform_scope.sql":      "f8bbba2ba51b1b8fa05a39f2f9d520a56dd20371aa6bd7c2e5520e7cb4fedba3",
	"199_backfill_platform_catalog.sql":              "24a2fbac08ccbd2b4aff18559f182317c2ee470533055021f9caee11d6d100be",
	"200_remove_legacy_configuration.sql":            "94628ba0d91318bd5f1328daeccf9ed96abdc25c2305815ab88ab7a03c6495ba",
}

func TestMigrationNamespaceAndFrozenChecksums(t *testing.T) {
	root := migrationDirectory(t)
	entries, err := os.ReadDir(root)
	requireNoError(t, err)

	seen := make(map[string]struct{})
	var names []string
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".sql") {
			continue
		}
		if _, duplicate := seen[entry.Name()]; duplicate {
			t.Fatalf("duplicate migration filename: %s", entry.Name())
		}
		seen[entry.Name()] = struct{}{}
		names = append(names, entry.Name())
	}
	sort.Strings(names)

	for name, expected := range frozenMigrationChecksums {
		path := filepath.Join(root, name)
		actual := migrationSHA256(t, path)
		if actual != expected {
			t.Fatalf("frozen migration checksum changed: %s want=%s got=%s", name, expected, actual)
		}
	}

	for _, name := range names {
		match := migrationFilePattern.FindStringSubmatch(name)
		if len(match) != 2 {
			t.Fatalf("migration filename must start with a numeric namespace: %s", name)
		}
		number, err := strconv.Atoi(match[1])
		requireNoError(t, err)
		content, err := os.ReadFile(filepath.Join(root, name))
		requireNoError(t, err)
		if strings.TrimSpace(string(content)) == "" {
			t.Fatalf("migration must not be empty: %s", name)
		}
		if number > 200 && number < 8000 {
			t.Fatalf("migration namespace %d is reserved; use 8000-8999 for runtime or 9000-9999 for ProductCore: %s", number, name)
		}
		if number >= 8000 && hasRemovedLegacyTableCreation(string(content)) {
			t.Fatalf("new migration restores a removed Group/Channel table: %s", name)
		}
	}
}

func migrationDirectory(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve migration guard path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", "..", "migrations"))
}

func migrationSHA256(t *testing.T, path string) string {
	t.Helper()
	content, err := os.ReadFile(path)
	requireNoError(t, err)
	sum := sha256.Sum256(content)
	return fmt.Sprintf("%x", sum[:])
}

func hasRemovedLegacyTableCreation(content string) bool {
	compact := strings.ToLower(strings.Join(strings.Fields(content), " "))
	for _, fragment := range []string{
		"create table groups",
		"create table if not exists groups",
		"create table channels",
		"create table if not exists channels",
		"create table account_groups",
		"create table if not exists account_groups",
		"create table api_key_allowed_groups",
		"create table if not exists api_key_allowed_groups",
	} {
		if strings.Contains(compact, fragment) {
			return true
		}
	}
	return false
}

func requireNoError(t *testing.T, err error) {
	t.Helper()
	if err != nil {
		t.Fatal(err)
	}
}
