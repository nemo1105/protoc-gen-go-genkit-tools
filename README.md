# protoc-gen-go-genkit-tools

Protoc/Buf plugin that turns proto methods annotated with AI tool options into Genkit-ready helpers:

- Generate tools once, use everywhere: turn your protobuf RPCs into Genkit tool registrations and typed wrappers.
- Built for Genkit’s Go SDK: emits `ToolRef`s, tool name constants, and JSON Schema metadata so UIs/agents can introspect inputs.
- Works with Buf or vanilla protoc: ship a single plugin binary and keep generated code checked in for consumers.

- Generates per-service tool interfaces, registration helpers, and tool name constants.
- Emits JSON Schema for tool inputs based on custom field options.
- Works directly with `genkitai.WithTools(...)` via `ToolRef` helpers.

## Layout
- `proto/genkit/tool/v1/tool_metadata.proto`: custom options `(genkit.tool.v1.tool_doc)` and `(genkit.tool.v1.field_doc)`.
- `buf.yaml` / `buf.gen.yaml`: Buf module + codegen config (Go stubs into `.`).
- `main.go`: plugin implementation.

## Usage
1) Install the plugin:
   ```sh
   go install github.com/nemo1105/protoc-gen-go-genkit-tools@latest
   ```
   Ensure `$GOPATH/bin` (or your Go install bin dir) is on `PATH`. Alternatively, build from the repo root with `go build -o "$GOPATH/bin/protoc-gen-go-genkit-tools" .` and keep `$GOPATH/bin` on `PATH`.

2) Annotate your proto RPCs with options:
   ```proto
   import "genkit/tool/v1/tool_metadata.proto";

   service ToolCatalog {
     rpc GetWeather(GetWeatherRequest) returns (GetWeatherResponse) {
       option (genkit.tool.v1.tool_doc) = { name: "get_weather" description: "Fetch weather" };
     }
   }

   message GetWeatherRequest {
     string city = 1 [(genkit.tool.v1.field_doc) = { desc: "City name" required: true }];
     string units = 2 [(genkit.tool.v1.field_doc) = { desc: "Units: metric|imperial" example: "metric" }];
   }

   message GetWeatherResponse {
     double temperature = 1;
     string summary = 2;
   }
   ```

3) Wire up Buf config and generate:
   - In `buf.yaml`, add the tool options module:
     ```yaml
     deps:
       - buf.build/genkit/tool-options
     ```
   - In `buf.gen.yaml`, register the plugin and keep the imported tool options from rewriting `go_package`:
     ```yaml
     plugins:
       - local: protoc-gen-go-genkit-tools
         out: .
         opt: paths=source_relative
     managed:
       enabled: true
       disable:
         - file_option: go_package
           module: buf.build/genkit/tool-options
     ```
   - Run `buf dep update`、`buf generate`. 
   - With Buf/Protobuf IDE plugins installed, the `import "genkit/tool/v1/tool_metadata.proto";` line will resolve cleanly and no longer show as missing after `buf dep update`.

4) Use generated helpers:
   ```go
   import (
     genkitai "github.com/firebase/genkit/go/ai"
     "github.com/firebase/genkit/go/genkit"
     catalog "github.com/your/module/path/to/your/generated/package"
   )

   tools, _ := catalog.RegisterToolCatalogToolRefs(g, impl) // impl implements GetWeather(...)
   genkit.Generate(ctx, g, genkitai.WithTools(tools...))
   // or pick a single tool by name constant:
   genkit.Generate(ctx, g, genkitai.WithTools(catalog.ToolCatalogGetWeatherTool))
   ```

## Releasing
- Tag the repo: `git tag v0.1.0 && git push origin v0.1.0`
- GitHub Actions:
  - On push/PR: `go test`, `buf lint`, `buf generate` (sanity).
  - On tag: same tests + `buf push` to BSR using `BUF_TOKEN` secret.

## Notes
- BSR module is declared in `buf.yaml` as `buf.build/genkit/tool-options`; adjust to your org before publishing.
