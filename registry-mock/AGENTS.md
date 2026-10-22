
# Project: Alfa Hack Registry Mock

## Stack
- Go 1.26
- Standard library net/http
- JSON-based synthetic data
- REST API

## Architecture
Strict separation of responsibilities:
Handler -> Service -> Repository

- Handler handles HTTP only.
- Service performs validation and business logic.
- Repository reads and searches stored data.
- Dependencies are injected via constructors.
- Interfaces are defined where they are consumed.

## Engineering rules
- Prefer simple, idiomatic Go.
- Follow YAGNI and avoid overengineering.
- Do not introduce unnecessary dependencies.
- Do not add features outside the requested scope.
- Preserve existing public API contracts.
- Propagate context.Context.
- Use errors.Is for expected errors.
- Keep HTTP status codes out of the service layer.
- Use deterministic synthetic test data.

## Testing
- Write table-driven tests where appropriate.
- Use httptest for HTTP handlers.
- Run go test ./... after changes.
- Run go vet ./...
- Report failed checks explicitly.
