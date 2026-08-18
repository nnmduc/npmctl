#!/usr/bin/env bash
# npmctl installer
# Usage:
#   curl -fsSL https://raw.githubusercontent.com/nnmduc/npmctl/main/install.sh | bash
# Or with options:
#   curl -fsSL https://raw.githubusercontent.com/nnmduc/npmctl/main/install.sh | VERSION=v0.1.0 INSTALL_DIR=~/.local/bin bash

set -euo pipefail

REPO="nnmduc/npmctl"
BINARY_NAME="npmctl"
TMP_DIR=""

cleanup() {
  if [ -n "${TMP_DIR:-}" ] && [ -d "${TMP_DIR:-}" ]; then
    rm -rf "$TMP_DIR"
  fi
}
trap cleanup EXIT INT TERM

# Color helpers
setup_colors() {
  if [ -t 1 ]; then
    RED='\033[0;31m'
    GREEN='\033[0;32m'
    YELLOW='\033[0;33m'
    BLUE='\033[0;34m'
    BOLD='\033[1m'
    NC='\033[0m'
  else
    RED=''
    GREEN=''
    YELLOW=''
    BLUE=''
    BOLD=''
    NC=''
  fi
}

info() {
  printf "${BLUE}==>${NC} ${BOLD}%s${NC}\n" "$1"
}

success() {
  printf "${GREEN}==>${NC} ${BOLD}%s${NC}\n" "$1"
}

warn() {
  printf "${YELLOW}warning:${NC} %s\n" "$1"
}

error() {
  printf "${RED}error:${NC} %s\n" "$1" >&2
  exit 1
}

# 1. Detect OS
detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    darwin) echo "darwin" ;;
    linux) echo "linux" ;;
    msys*|mingw*|cygwin*) echo "windows" ;;
    *) error "Unsupported operating system: $os" ;;
  esac
}

# 2. Detect Architecture
detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64) echo "amd64" ;;
    arm64|aarch64) echo "arm64" ;;
    *) error "Unsupported architecture: $arch" ;;
  esac
}

# 3. HTTP downloader helper
download_file() {
  local url="$1"
  local dest="$2"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url" -o "$dest"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO "$dest" "$url"
  else
    error "Neither curl nor wget is available."
  fi
}

fetch_json() {
  local url="$1"
  if command -v curl >/dev/null 2>&1; then
    curl -fsSL "$url"
  elif command -v wget >/dev/null 2>&1; then
    wget -qO- "$url"
  else
    error "Neither curl nor wget is available."
  fi
}

# 4. Resolve version
resolve_version() {
  if [ -n "${VERSION:-}" ]; then
    echo "$VERSION"
    return
  fi
  if [ -n "${NPMCTL_VERSION:-}" ]; then
    echo "$NPMCTL_VERSION"
    return
  fi

  local api_url="https://api.github.com/repos/${REPO}/releases/latest"
  local tag
  tag="$(fetch_json "$api_url" | grep -o '"tag_name": *"[^"]*"' | head -n 1 | cut -d'"' -f4 || true)"
  if [ -z "$tag" ]; then
    tag="v0.1.0"
  fi
  echo "$tag"
}

# 5. Checksum verification
verify_checksum() {
  local file="$1"
  local checksums="$2"
  local filename
  filename="$(basename "$file")"

  local expected_hash
  expected_hash="$(grep -F "$filename" "$checksums" | awk '{print $1}' || true)"

  if [ -z "$expected_hash" ]; then
    warn "No checksum found for $filename in checksums.txt, skipping checksum verification."
    return 0
  fi

  local actual_hash=""
  if command -v sha256sum >/dev/null 2>&1; then
    actual_hash="$(sha256sum "$file" | awk '{print $1}')"
  elif command -v shasum >/dev/null 2>&1; then
    actual_hash="$(shasum -a 256 "$file" | awk '{print $1}')"
  fi

  if [ -n "$actual_hash" ]; then
    if [ "$actual_hash" != "$expected_hash" ]; then
      error "Checksum mismatch for $filename!\nExpected: $expected_hash\nActual:   $actual_hash"
    fi
  fi
}

# 6. Main execution
main() {
  setup_colors

  local os arch tag ver_raw archive_name archive_ext
  os="$(detect_os)"
  arch="$(detect_arch)"
  tag="$(resolve_version)"
  ver_raw="${tag#v}"

  info "Installing npmctl ${tag} (${os}/${arch})..."

  if [ "$os" = "windows" ]; then
    archive_ext="zip"
  else
    archive_ext="tar.gz"
  fi

  archive_name="npmctl_${ver_raw}_${os}_${arch}.${archive_ext}"
  local download_url="https://github.com/${REPO}/releases/download/${tag}/${archive_name}"
  local checksums_url="https://github.com/${REPO}/releases/download/${tag}/checksums.txt"

  TMP_DIR="$(mktemp -d 2>/dev/null || mktemp -d -t 'npmctl-install')"

  info "Downloading ${archive_name}..."
  download_file "$download_url" "${TMP_DIR}/${archive_name}"
  download_file "$checksums_url" "${TMP_DIR}/checksums.txt"

  info "Verifying SHA256 checksum..."
  verify_checksum "${TMP_DIR}/${archive_name}" "${TMP_DIR}/checksums.txt"

  info "Extracting archive..."
  if [ "$archive_ext" = "zip" ]; then
    unzip -q "${TMP_DIR}/${archive_name}" -d "$TMP_DIR"
  else
    tar -xzf "${TMP_DIR}/${archive_name}" -C "$TMP_DIR"
  fi

  # Determine install location
  local target_dir
  if [ -n "${INSTALL_DIR:-}" ]; then
    target_dir="$INSTALL_DIR"
  elif [ -n "${BINDIR:-}" ]; then
    target_dir="$BINDIR"
  elif [ -w "/usr/local/bin" ]; then
    target_dir="/usr/local/bin"
  elif [ -d "$HOME/.local/bin" ] || mkdir -p "$HOME/.local/bin" 2>/dev/null; then
    target_dir="$HOME/.local/bin"
  elif [ -d "$HOME/bin" ] || mkdir -p "$HOME/bin" 2>/dev/null; then
    target_dir="$HOME/bin"
  else
    target_dir="/usr/local/bin"
  fi

  local target_bin="${target_dir}/${BINARY_NAME}"
  if [ "$os" = "windows" ]; then
    target_bin="${target_bin}.exe"
  fi

  info "Installing to ${target_bin}..."
  if [ -w "$target_dir" ] || [ ! -e "$target_bin" -a -w "$(dirname "$target_dir")" ]; then
    mkdir -p "$target_dir"
    cp -f "${TMP_DIR}/${BINARY_NAME}" "$target_bin"
    chmod 755 "$target_bin"
  else
    if command -v sudo >/dev/null 2>&1; then
      warn "Elevated permissions required to write to ${target_dir}. Running sudo..."
      sudo mkdir -p "$target_dir"
      sudo cp -f "${TMP_DIR}/${BINARY_NAME}" "$target_bin"
      sudo chmod 755 "$target_bin"
    else
      error "Cannot write to ${target_dir}. Set INSTALL_DIR=~/.local/bin to install without root."
    fi
  fi

  success "npmctl ${tag} installed successfully to ${target_bin}!"

  # Check PATH
  case ":$PATH:" in
    *":${target_dir}:"*) ;;
    *)
      warn "${target_dir} is not in your \$PATH."
      printf "Add it to your shell configuration:\n"
      printf "  export PATH=\"%s:\$PATH\"\n\n" "$target_dir"
      ;;
  esac

  # Verify executable
  if [ -x "$target_bin" ]; then
    printf "\n"
    "$target_bin" version || true
    printf "\n"
    info "To connect to your Nginx Proxy Manager instance, run:"
    printf "  npmctl login https://npm.yourdomain.com\n\n"
    info "To install agent skill for Claude Code / Antigravity / Codex:"
    printf "  npmctl skill install\n\n"
  fi
}

main "$@"
