# Security Policy

## Supported Versions

We provide security updates for the latest release:

| Version | Supported          |
| ------- | ------------------ |
| latest  | :white_check_mark: |
| < 1.0.0 | :x:                |

## Reporting a Vulnerability

We take the security of `npmctl` seriously. If you discover a security vulnerability, please do not report it through a public GitHub issue.

Instead, please report it through one of the following methods:

1. **GitHub Security Advisory** (Preferred): Submit a private report via the [Security Advisories tab](https://github.com/nnmduc/npmctl/security/advisories/new).
2. **Direct Contact**: If private reporting is unavailable, reach out to the project maintainers directly.

### Information to Include

When reporting a vulnerability, please provide:
- A clear description of the vulnerability.
- Steps or a minimal proof of concept to reproduce the behavior.
- The potential impact or threat scenario.
- Any suggested mitigations or patches, if available.

### Response Timeline

- **Acknowledgment**: Within 48 hours of receipt.
- **Assessment & Fix**: We will assess the issue and prepare a fix or advisory as quickly as possible.
- **Disclosure**: Public disclosure will be coordinated after a patched release is published.

## Security Architecture & Threat Model

For details on how `npmctl` protects against accidental execution, handles secret redaction, and isolates write operations, please consult [docs/security.md](docs/security.md).
