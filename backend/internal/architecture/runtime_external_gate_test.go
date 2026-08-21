//go:build unit

package architecture

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestExternalRuntimeBridgeRequiresAllExplicitPorts(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	backendRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	specPath := filepath.Join(backendRoot, "..", "docs", "superpowers", "specs", "2026-08-20-runtimebridge-sub2api-separation-design.md")
	spec, err := os.ReadFile(specPath)
	if err != nil {
		t.Fatalf("read RuntimeBridge design: %v", err)
	}
	for _, requirement := range []string{"RuntimeStore/ControlPort", "HTTP/SSE transport", "单进程与外置模式的黑盒回归结果一致", "服务鉴权"} {
		if !strings.Contains(string(spec), requirement) {
			t.Fatalf("RuntimeBridge design is missing externalization gate %q", requirement)
		}
	}
	externalRoot := filepath.Join(backendRoot, "internal", "runtimebridge")
	err = filepath.WalkDir(externalRoot, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		if strings.Contains(strings.ToLower(entry.Name()), "external") {
			content, readErr := os.ReadFile(path)
			if readErr != nil {
				return readErr
			}
			source := string(content)
			if !strings.Contains(source, "RuntimeStore") || !strings.Contains(source, "ControlPort") {
				t.Fatalf("external RuntimeBridge file %s lacks RuntimeStore/ControlPort gate", path)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan external RuntimeBridge files: %v", err)
	}
}
