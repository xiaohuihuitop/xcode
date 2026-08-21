//go:build unit

package architecture

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestSub2APIDriverPackageHasNoHandlerOrGinDependency(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	root := filepath.Join(filepath.Dir(filename), "..", "runtime", "sub2api")
	err := filepath.WalkDir(root, func(path string, entry os.DirEntry, walkErr error) error {
		if walkErr != nil {
			return walkErr
		}
		if entry.IsDir() || !strings.HasSuffix(entry.Name(), ".go") || strings.HasSuffix(entry.Name(), "_test.go") {
			return nil
		}
		content, err := os.ReadFile(path)
		if err != nil {
			return err
		}
		source := string(content)
		for _, forbidden := range []string{"github.com/gin-gonic/gin", "internal/handler", "ginContextCarrier", "GinContext()"} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s imports or references forbidden runtime dependency %q", path, forbidden)
			}
		}
		return nil
	})
	if err != nil {
		t.Fatalf("scan Sub2API driver package: %v", err)
	}
}

func TestProductionCompositionUsesPureOpenAIDriver(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	backendRoot := filepath.Join(filepath.Dir(filename), "..", "..")
	path := filepath.Join(backendRoot, "internal", "handler", "sub2api_runtime_composition.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read production composition: %v", err)
	}
	source := string(content)
	for _, required := range []string{"sub2api.OpenAIExecutor", "openAIDriver", "sub2APIOpenAIPort"} {
		if !strings.Contains(source, required) {
			t.Fatalf("production composition does not contain pure OpenAI driver marker %q", required)
		}
	}
}
