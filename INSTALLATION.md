# Installation

## One-line installer (macOS & Linux)

Install the latest static binary directly from the repository:

```bash
curl -fsSL https://raw.githubusercontent.com/nnmduc/npmctl/main/install.sh | bash
```

### Custom directory or specific version

```bash
curl -fsSL https://raw.githubusercontent.com/nnmduc/npmctl/main/install.sh | INSTALL_DIR=~/.local/bin VERSION=v0.1.0 bash
```

## Go Install

If you have Go installed on your system:

```bash
go install github.com/nnmduc/npmctl/cmd/npmctl@latest
```

## Pre-built Binaries

Download pre-compiled static binaries from [Releases](https://github.com/nnmduc/npmctl/releases/latest).

Static binaries have no external runtime dependencies:
- macOS (arm64, amd64)
- Linux (amd64, arm64)
- Windows (amd64)

## Build from Source

```bash
git clone https://github.com/nnmduc/npmctl.git
cd npmctl
make build
```
