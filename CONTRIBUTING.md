# Contributing to npmctl

Thank you for contributing to `npmctl`! We welcome bug reports, feature suggestions, documentation improvements, and code contributions.

## Code of Conduct

All contributors are expected to adhere to our [Code of Conduct](CODE_OF_CONDUCT.md).

## Getting Started

### Prerequisites

- [Go](https://go.dev/) 1.22+ (or latest stable)
- `make`
- (Optional) Docker for running local integration tests with a live NPM instance

### Clone & Build

```bash
git clone https://github.com/nnmduc/npmctl.git
cd npmctl

# Build binary
make build

# Run unit test suite (hermetic, no network required)
make test

# Format and vet
make lint
```

## Development Workflow

1. **Fork and Branch**: Create a feature branch from `main` (e.g. `feat/my-new-feature` or `fix/issue-description`).
2. **Make Changes**:
   - Keep changes focused and well-tested.
   - Maintain safety gates and write constraints (see [Safety Model](docs/safety.md)).
   - Ensure all output commands support `--json` and appropriate error codes.
3. **Run Checks**:
   - `make test` must pass cleanly.
   - `make lint` (`gofmt` + `go vet`) must produce no diffs or warnings.
4. **Commit Messages**:
   - Follow [Conventional Commits](https://www.conventionalcommits.org/):
     - `feat: ...` for new capabilities
     - `fix: ...` for bug fixes
     - `docs: ...` for documentation updates
     - `ci: ...` / `chore: ...` for maintenance
5. **Open a Pull Request**: Submit your PR targeting `main` with a clear description and testing steps.

## Testing Guidelines

- **Unit Tests**: Place unit tests in standard `*_test.go` files alongside the implementation. Use hermetic test doubles or the mock HTTP client so tests run quickly without network access.
- **End-to-End Testing**: For verifying changes against a live Nginx Proxy Manager instance, refer to the [Lab Instance Guide](docs/lab-instance.md).

## Reporting Issues & Feature Requests

- **Bug Reports**: Use the [Bug Report template](https://github.com/nnmduc/npmctl/issues/new?template=bug_report.yml). Please include reproducible steps, the command run, and any sanitized error output.
- **Feature Requests**: Use the [Feature Request template](https://github.com/nnmduc/npmctl/issues/new?template=feature_request.yml).
- **Security Vulnerabilities**: Please see [SECURITY.md](SECURITY.md) for private vulnerability reporting instructions.
