# Contributing to Steward

Thank you for your interest in contributing! 🎉

## Development Setup

```bash
# Clone
git clone https://github.com/brooqs/steward.git
cd steward

# Build
go build -o steward ./cmd/steward
go build -o steward-satellite ./cmd/satellite

# Run checks
go vet ./...
go test ./...
```

## Pull Request Process

1. Fork the repository
2. Create a feature branch: `git checkout -b feature/my-feature`
3. Make your changes
4. Run `go vet ./...` and `go test ./...`
5. Commit with clear messages
6. Push and open a PR against `main`

## Adding a New LLM Provider

1. Create `internal/provider/yourprovider.go`
2. Implement the `Provider` interface
3. Add a case in `internal/provider/factory.go`
4. Add validation in `internal/config/config.go`

## Adding a New Integration

1. Create `internal/integration/yourservice/yourservice.go`
2. Implement the `Integration` interface
3. Add an `init()` function calling `integration.Register()`
4. Add a blank import in `cmd/steward/main.go`
5. Create `config/integrations/yourservice.yml.example`

## Code Guidelines

- Follow standard Go conventions (`gofmt`, `go vet`)
- Use `log/slog` for structured logging
- Keep packages focused — one responsibility per package
- Security defaults: features that could be risky should be **disabled by default**

## Reporting Issues

- Use GitHub Issues
- Include: Go version, OS, config (redact API keys), and error logs
