# Development

## Build and Test

```bash
make build      # Build the static binary
make test       # Run fast, isolated unit tests (no network or Docker required)
make lint       # Check formatting and run go vet
```

## Testing Against a Real Instance

Unit tests mock the API layer. For end-to-end testing against a real Nginx Proxy Manager instance, see the [Lab Instance Guide](lab-instance.md).

Running tests against a live instance verifies real NPM behavior, such as header parsing, access list responses, and nginx reload checks.
