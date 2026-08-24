//go:build unit

package architecture

import (
	"go/parser"
	"go/token"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestOfficialRuntimeImportPolicyAllowsInternalRuntimeImports(t *testing.T) {
	if officialRuntimeImportForbidden("github.com/Wei-Shaw/sub2api/internal/runtime/sub2api/upstream/protocol") {
		t.Fatal("Official Runtime internal imports must not be treated as ProductCore dependencies")
	}
	if !officialRuntimeImportForbidden("github.com/Wei-Shaw/sub2api/internal/service/subscription") {
		t.Fatal("Official Runtime imports into ProductCore must be rejected")
	}
}

func TestOfficialRuntimeZoneHasOwnershipManifest(t *testing.T) {
	backendRoot := architectureBackendRoot(t)
	manifestPath := filepath.Join(backendRoot, "internal", "runtime", "sub2api", "upstream", "README.md")
	content, err := os.ReadFile(manifestPath)
	if err != nil {
		t.Fatalf("read Official Runtime zone manifest: %v", err)
	}
	for _, marker := range []string{"Official Runtime zone", "ProductCore", "RuntimeBridge v1", "direct_sync"} {
		if !strings.Contains(string(content), marker) {
			t.Fatalf("Official Runtime zone manifest is missing %q", marker)
		}
	}
}

func TestProductCoreDoesNotImportOfficialRuntimeZone(t *testing.T) {
	backendRoot := architectureBackendRoot(t)
	for _, relative := range []string{
		filepath.Join("internal", "service"),
		filepath.Join("internal", "handler"),
	} {
		scanGoImports(t, filepath.Join(backendRoot, relative), func(pathValue string) bool {
			return strings.Contains(pathValue, "internal/runtime/sub2api/upstream")
		})
	}
}

func TestSub2APIAdapterDoesNotImportOfficialRuntimeZoneDirectly(t *testing.T) {
	backendRoot := architectureBackendRoot(t)
	scanGoImports(t, filepath.Join(backendRoot, "internal", "runtime", "sub2api"), func(pathValue string) bool {
		return strings.Contains(pathValue, "internal/runtime/sub2api/upstream")
	}, filepath.Join(backendRoot, "internal", "runtime", "sub2api", "upstream"))
}

func TestOfficialRuntimeDoesNotImportProductCore(t *testing.T) {
	backendRoot := architectureBackendRoot(t)
	scanGoImports(t, filepath.Join(backendRoot, "internal", "runtime", "sub2api", "upstream"), officialRuntimeImportForbidden)
}

func architectureBackendRoot(t *testing.T) string {
	t.Helper()
	_, filename, _, ok := runtime.Caller(1)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	return filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
}

func officialRuntimeImportForbidden(pathValue string) bool {
	if strings.Contains(pathValue, "internal/runtime/sub2api/upstream") {
		return false
	}
	for _, marker := range []string{
		"internal/productcore",
		"internal/service/",
		"internal/handler/",
		"internal/payment/",
		"internal/subscription/",
		"internal/api_key/",
	} {
		if strings.Contains(pathValue, marker) {
			return true
		}
	}
	return false
}

func scanGoImports(t *testing.T, root string, forbidden func(string) bool, excludedRoots ...string) {
	t.Helper()
	err := filepath.Walk(root, func(path string, info os.FileInfo, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		for _, excluded := range excludedRoots {
			if filepath.Clean(path) == filepath.Clean(excluded) {
				return filepath.SkipDir
			}
		}
		if info.IsDir() || !strings.HasSuffix(info.Name(), ".go") || strings.HasSuffix(info.Name(), "_test.go") {
			return nil
		}
		file, err := parser.ParseFile(token.NewFileSet(), path, nil, 0)
		if err != nil {
			return err
		}
		for _, imported := range file.Imports {
			if forbidden(strings.Trim(imported.Path.Value, `"`)) {
				t.Errorf("%s imports forbidden Official Runtime zone", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Go imports under %s: %v", root, err)
	}
}
