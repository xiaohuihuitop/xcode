//go:build unit

package architecture

import (
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"testing"
)

var sub2APILegacyBridgeTokens = []string{
	"legacyGinHandler",
	"legacyEndpointExecutor",
	"dispatchLegacyEndpoint",
	"ginContextCarrier",
	"GinContext()",
}

// This baseline is intentionally an allowlist of exact file totals, not a
// directory-wide exception. Each migration step must remove entries; a new
// legacy bridge call in any other file fails immediately.
var sub2APILegacyBridgeBaseline = map[string]int{
	"internal/handler/openai_live.go":                1,
	"internal/handler/sub2api_auxiliary_executor.go": 2,
	"internal/handler/sub2api_legacy_dispatch.go":    24,
	"internal/handler/sub2api_messages_executor.go":  3,
}

func TestSub2APIAdapterLegacyCallsitesMatchBaseline(t *testing.T) {
	actual := scanSub2APILegacyBridgeCallsites(t)
	if len(actual) != len(sub2APILegacyBridgeBaseline) {
		t.Fatalf("legacy bridge file set changed: got %v, want %v", sortedKeys(actual), sortedKeys(sub2APILegacyBridgeBaseline))
	}
	for path, want := range sub2APILegacyBridgeBaseline {
		if got := actual[path]; got != want {
			t.Errorf("legacy bridge count changed for %s: got %d, want %d", path, got, want)
		}
	}
}

func TestOpenAIImagesRuntimeDoesNotUseGinCompatibilityBridge(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	path := filepath.Join(backendRoot, "internal", "service", "gateway_runtime_exchange.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read gateway runtime exchange: %v", err)
	}
	source := string(content)
	start := strings.Index(source, "func (s *OpenAIGatewayService) ForwardImagesRuntime(")
	if start < 0 {
		t.Fatal("ForwardImagesRuntime not found")
	}
	rest := source[start+1:]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	method := rest[:end]
	for _, forbidden := range []string{"withRuntimeGinContext", "gin.Context", ".ForwardImages("} {
		if strings.Contains(method, forbidden) {
			t.Fatalf("ForwardImagesRuntime still depends on %q", forbidden)
		}
	}
}

func TestOpenAIAPIKeyCompatRuntimeFilesDoNotUseGinCompatibilityBridge(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	for _, name := range []string{
		"openai_gateway_chat_runtime.go",
		"openai_gateway_chat_preparation_runtime.go",
		"openai_gateway_chat_responses_runtime.go",
		"openai_gateway_messages_preparation_runtime.go",
		"openai_messages_session_runtime.go",
		"openai_gateway_anthropic_responses_runtime.go",
		"openai_gateway_compat_runtime.go",
		"openai_gateway_forward_runtime.go",
		"openai_gateway_forward_preparation_runtime.go",
		"openai_gateway_request_runtime.go",
		"openai_gateway_response_runtime.go",
		"openai_gateway_stream_runtime.go",
	} {
		path := filepath.Join(backendRoot, "internal", "service", name)
		content, err := os.ReadFile(path)
		if err != nil {
			t.Fatalf("read %s: %v", name, err)
		}
		source := string(content)
		for _, forbidden := range []string{
			"github.com/gin-gonic/gin",
			"withRuntimeGinContext",
			"runtimeGinContext",
			"*gin.Context",
		} {
			if strings.Contains(source, forbidden) {
				t.Fatalf("%s still depends on %q", name, forbidden)
			}
		}
	}
}

func TestOpenAIStreamingCoreDoesNotUseGinResponseSurface(t *testing.T) {
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	path := filepath.Join(backendRoot, "internal", "service", "openai_gateway_response_handling.go")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatalf("read OpenAI response handling: %v", err)
	}
	source := string(content)
	start := strings.Index(source, "func (s *OpenAIGatewayService) handleStreamingResponseWithReasoningCore(")
	if start < 0 {
		t.Fatal("streaming core not found")
	}
	rest := source[start+1:]
	end := strings.Index(rest, "\nfunc ")
	if end < 0 {
		end = len(rest)
	}
	core := rest[:end]
	for _, forbidden := range []string{
		"*gin.Context",
		"c.Writer",
		"c.JSON",
		"withRuntimeGinContext",
		"runtimeHTTPExchangeFromGinContext",
	} {
		if strings.Contains(core, forbidden) {
			t.Fatalf("OpenAI streaming core still depends on %q", forbidden)
		}
	}
}

func scanSub2APILegacyBridgeCallsites(t *testing.T) map[string]int {
	t.Helper()
	_, filename, _, ok := runtime.Caller(0)
	if !ok {
		t.Fatal("resolve architecture test path")
	}
	backendRoot := filepath.Clean(filepath.Join(filepath.Dir(filename), "..", ".."))
	handlerRoot := filepath.Join(backendRoot, "internal", "handler")
	actual := make(map[string]int)
	err := filepath.WalkDir(handlerRoot, func(path string, entry os.DirEntry, walkErr error) error {
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
		count := 0
		for _, token := range sub2APILegacyBridgeTokens {
			count += strings.Count(string(content), token)
		}
		if count == 0 {
			return nil
		}
		relative, err := filepath.Rel(backendRoot, path)
		if err != nil {
			return err
		}
		actual[filepath.ToSlash(relative)] = count
		return nil
	})
	if err != nil {
		t.Fatalf("scan Sub2API legacy bridge callsites: %v", err)
	}
	return actual
}

func sortedKeys(values map[string]int) []string {
	keys := make([]string, 0, len(values))
	for key := range values {
		keys = append(keys, key)
	}
	sort.Strings(keys)
	return keys
}
