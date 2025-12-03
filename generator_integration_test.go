package main

import (
	"fmt"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"testing"
)

var (
	generatedOnce sync.Once
	generatedCode map[string]string
	generateErr   error
)

func TestPluginGeneratesExpectedTools(t *testing.T) {
	code := generateForProto(t, "test/proto/catalog.proto")

	// Tool naming and registration.
	mustContain(t, code, `const ToolCatalogGetWeatherTool genkitai.ToolName = "get_weather"`)
	mustContain(t, code, "defineToolCatalogGetWeatherTool(g, impl)")
	mustNotContain(t, code, "UndocumentedTool")

	// Input schema renders required fields and descriptions.
	mustContain(t, code, `"required": []string{"city"}`)
	mustContain(t, code, `"description": "City and optional units"`)
	mustContain(t, code, `"example": "metric"`)

	// Coercion and error handling.
	mustContain(t, code, `errors.New("get_weather requires input")`)
	mustContain(t, code, `return impl.GetWeather(ctx, req)`)
}

func TestInvoiceGeneration(t *testing.T) {
	code := generateForProto(t, "test/proto/invoice/v1/invoice.proto")

	mustContain(t, code, `const InvoiceServiceCreateInvoiceTool genkitai.ToolName = "create_invoice"`)
	mustContain(t, code, `"required": []string{"invoice"}`)
	mustContain(t, code, `"description": "info to create invoice"`)
	mustContain(t, code, `return impl.CreateInvoice(ctx, req)`)
	mustContain(t, code, `errors.New("create_invoice requires input")`)
	mustContain(t, code, "\"invoice\": map[string]any{\"description\": \"The invoice to create.\", \"properties\": map[string]any{\"customer_id\": map[string]any{\"type\": \"string\"")
	mustContain(t, code, "\"line_items\": map[string]any{\"items\": map[string]any{\"properties\": map[string]any{\"line_item_id\": map[string]any{\"type\": \"string\"")
	mustContain(t, code, "\"tags\": map[string]any{\"properties\": map[string]any{\"tag\": map[string]any{\"items\": map[string]any{\"type\": \"string\"}, \"type\": \"array\"}}, \"type\": \"object\"}")
}

func generateForProto(t *testing.T, targetProto string) string {
	t.Helper()

	generatedOnce.Do(func() {
		generatedCode, generateErr = runGeneration(t, []string{
			"test/proto/catalog.proto",
			"test/proto/invoice/v1/invoice.proto",
		})
	})

	if generateErr != nil {
		t.Fatalf("generate protos: %v", generateErr)
	}
	code, ok := generatedCode[targetProto]
	if !ok {
		t.Fatalf("missing generated output for %s", targetProto)
	}
	return code
}

func runGeneration(t *testing.T, targets []string) (map[string]string, error) {
	t.Helper()

	tempDir, err := os.MkdirTemp("", "bufgen")
	if err != nil {
		return nil, err
	}
	t.Cleanup(func() { _ = os.RemoveAll(tempDir) })

	binDir := filepath.Join(tempDir, "bin")
	workspace := filepath.Join(tempDir, "workspace")
	outDir := filepath.Join(workspace, "out")
	if err := os.MkdirAll(binDir, 0o755); err != nil {
		return nil, err
	}
	if err := os.MkdirAll(workspace, 0o755); err != nil {
		return nil, err
	}

	if err := buildBinary(t, binDir, "protoc-gen-go-genkit-tools", "."); err != nil {
		return nil, err
	}
	if err := buildBinary(t, binDir, "protoc-gen-go", "google.golang.org/protobuf/cmd/protoc-gen-go"); err != nil {
		return nil, err
	}

	// Prepare a self-contained Buf workspace in temp.
	if err := copyFile(t, "proto/genkit/tool/v1/tool_metadata.proto", filepath.Join(workspace, "test/proto/genkit/tool/v1/tool_metadata.proto")); err != nil {
		return nil, err
	}
	if err := copyDir(t, "test/proto", filepath.Join(workspace, "test/proto")); err != nil {
		return nil, err
	}
	if err := copyFile(t, "test/buf.yaml", filepath.Join(workspace, "buf.yaml")); err != nil {
		return nil, err
	}
	if err := copyFile(t, "test/buf.gen.yaml", filepath.Join(workspace, "buf.gen.yaml")); err != nil {
		return nil, err
	}

	args := []string{"generate"}
	for _, p := range targets {
		args = append(args, "--path", p)
	}
	bufGen := exec.Command("buf", args...)
	bufGen.Dir = workspace
	bufGen.Env = prependPath(os.Environ(), binDir)
	if err := runCmd(bufGen); err != nil {
		return nil, err
	}

	out := make(map[string]string)
	for _, p := range targets {
		relProto := strings.TrimPrefix(p, "test/proto/")
		outFile := filepath.Join(outDir, strings.TrimSuffix(relProto, ".proto")+"_genkit.tools.go")
		content, err := os.ReadFile(outFile)
		if err != nil {
			return nil, err
		}
		copyBack := filepath.Join("test", "out", strings.TrimSuffix(relProto, ".proto")+"_genkit.tools.go.txt")
		if err := copyFile(t, outFile, copyBack); err != nil {
			return nil, err
		}
		out[p] = string(content)
	}

	return out, nil
}

func buildBinary(t *testing.T, binDir, name, target string) error {
	t.Helper()
	cmd := exec.Command("go", "build", "-o", filepath.Join(binDir, name), target)
	cmd.Env = append(os.Environ(), "GOTOOLCHAIN=local")
	return runCmd(cmd)
}

func runCmd(cmd *exec.Cmd) error {
	if out, err := cmd.CombinedOutput(); err != nil {
		return fmt.Errorf("%s failed: %v\n%s", strings.Join(cmd.Args, " "), err, out)
	}
	return nil
}

func prependPath(env []string, dir string) []string {
	prefix := "PATH=" + dir + string(os.PathListSeparator)
	for i, kv := range env {
		if strings.HasPrefix(kv, "PATH=") {
			env[i] = prefix + strings.TrimPrefix(kv, "PATH=")
			return env
		}
	}
	return append(env, prefix)
}

func mustContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if !strings.Contains(haystack, needle) {
		t.Fatalf("expected generated code to contain %q", needle)
	}
}

func mustNotContain(t *testing.T, haystack, needle string) {
	t.Helper()
	if strings.Contains(haystack, needle) {
		t.Fatalf("expected generated code to NOT contain %q", needle)
	}
}

func copyDir(t *testing.T, src, dst string) error {
	t.Helper()
	err := filepath.Walk(src, func(path string, info os.FileInfo, err error) error {
		if err != nil {
			return err
		}
		rel, err := filepath.Rel(src, path)
		if err != nil {
			return err
		}
		target := filepath.Join(dst, rel)
		if info.IsDir() {
			return os.MkdirAll(target, 0o755)
		}
		return copyFile(t, path, target)
	})
	return err
}

func copyFile(t *testing.T, src, dst string) error {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(dst), 0o755); err != nil {
		return err
	}
	in, err := os.Open(src)
	if err != nil {
		return err
	}
	defer in.Close()

	out, err := os.Create(dst)
	if err != nil {
		return err
	}
	defer out.Close()

	if _, err := io.Copy(out, in); err != nil {
		return err
	}
	return nil
}
