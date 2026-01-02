# CLIProxyAPI Coding Standards & Guidelines

This document provides technical guidance for contributors to the CLIProxyAPI project. It defines the project's organization, coding style, and core implementation patterns to ensure consistency across the codebase.

> [!NOTE]
> This document complements the [Project Outline](file:///Users/user/Documents/Muhsinun/Projects/GitHub/CLIProxyAPI/notes/project_outline.md). Refer to the outline for high-level architecture and data flow.

---

## 1. Project Organization

### 1.1 Directory Structure
- `cmd/server/`: Entry point for the application. Minimize logic here; it should primarily handle configuration loading and server initialization.
- `internal/`: Private application code. This is where most business logic, translators, and internal utilities reside.
- `sdk/`: Public-facing SDK components. Logic here should be reusable and generic enough to be embedded in other projects.
- `notes/`: Documentation, research, and technical guides.

### 1.2 Package Naming
- Use single-word, lowercase names for packages (e.g., `translator`, `registry`, `auth`).
- Avoid underscores or mixed case in package names.

---

## 2. Coding Style & Conventions

### 2.1 Go Standards
- **Formatting**: Always run `gofmt` (or `goimports`) before committing.
- **Naming**:
    - **Exported**: PascalCase (e.g., `BaseAPIHandler`, `ModelInfo`).
    - **Unexported**: camelCase (e.g., `applyAccessConfig`).
    - **Interfaces**: Use descriptive names, often ending in `-er` (e.g., `TokenStore`, `RequestLogger`).
- **Receiver Names**: Use short, consistent names for method receivers (e.g., `s *Server`, `t *Translator`).

### 2.2 Error Handling
- **Return Early**: Use the "guard clause" pattern to handle errors and return early.
- **Wrapping**: Wrap errors with context to aid debugging: `fmt.Errorf("failed to process request: %w", err)`.
- **API Errors**: Use structured error responses for client-facing handlers.

### 2.3 Documentation
- Provide package-level comments at the top of each file.
- Document all exported functions, types, and constants with descriptive comments.

---

## 3. Core Implementation Patterns

### 3.1 Registration Pattern
The project heavily uses `init()` functions for self-registering components. This is common for translators and auth providers.

```go
func init() {
    translator.Register(
        constant.OpenAI,        // Source Format
        constant.Antigravity,   // Target Provider
        ConvertRequest,         // Request conversion logic
        interfaces.TranslateResponse{
            Stream:    ConvertStreamResponse,
            NonStream: ConvertNonStreamResponse,
        },
    )
}
```
> [!IMPORTANT]
> Ensure new translators or providers are blank-imported in `internal/translator/init.go` to trigger their registration.

### 3.2 JSON Manipulation
Use [gjson](https://github.com/tidwall/gjson) and [sjson](https://github.com/tidwall/sjson) for efficient JSON parsing and modification instead of heavy struct mapping when possible.

- Use `gjson.GetBytes(raw, "path.to.field")` for extraction.
- Use `sjson.SetBytes(raw, "path.to.field", value)` for modification.

### 3.3 Streaming Responses
Most API endpoints support Server-Sent Events (SSE). Use channels to pipe upstream chunks through a translator and back to the client.

```go
dataChan := make(chan []byte)
errChan := make(chan *interfaces.ErrorMessage)

go func() {
    defer close(dataChan)
    for chunk := range upstream {
        translated := translateChunk(chunk)
        dataChan <- translated
    }
}()

return dataChan, errChan
```

---

## 4. Adding a New Provider

To add support for a new AI provider, follow these steps:

1.  **Constants**: Add the provider name to `internal/constant/`.
2.  **Auth**: Implement auth handling in `internal/auth/<provider>/`.
3.  **Translators**:
    -   Create `internal/translator/<provider>/`.
    -   Implement `ConvertRequest` and `TranslateResponse`.
    -   Register them in an `init()` function.
    -   Add a blank import in `internal/translator/init.go`.
4.  **Models**: Define available models in `internal/registry/model_definitions.go`.
5.  **Executor**: Add an executor in `internal/runtime/executor/` if the provider requires specific HTTP/WebSocket handling.
6.  **Config**: Update `internal/config/config.go` to support any new configuration options.

---

## 5. Testing & Verification

- **Unit Tests**: Place tests in `_test.go` files within the same package.
- **Integration Tests**: Use the `test/` directory for end-to-end or multi-component tests.
- **Simulations**: Use the scripts in `notes/` (e.g., `test_proxy.sh`) to verify live proxy behavior against the server.

---

## 6. Logging Standards

- Use `logrus` for logging.
- **Debug**: Use for verbose implementation details. Enabled via `debug: true` in config.
- **Info**: Use for major lifecycle events (server start, provider registration).
- **Warn/Error**: Use for recoverable and unrecoverable issues, respectively. Always include the error context: `.WithError(err).Error("...")`.
