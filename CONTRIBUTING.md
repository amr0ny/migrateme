# Contributing

Thanks for contributing to `migrateme`.

## Development Setup

1. Install Go `1.24+`.
2. Clone repository.
3. Build CLI:

```bash
go build -o migrateme ./cmd/migrateme
```

## Before Opening a Pull Request

Run the local checks:

```bash
gofmt -w .
go test ./...
go vet ./...
```

## Guidelines

- Keep changes focused and small.
- Prefer deterministic behavior in migration generation.
- Add/adjust tests for behavior changes.
- Do not commit secrets or local environment files.

## Commit and PR Notes

- Use clear commit messages that explain the reason for the change.
- In PR description, include:
  - what changed,
  - why it changed,
  - how to validate (commands and expected result).
