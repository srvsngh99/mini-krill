#!/usr/bin/env bash
set -euo pipefail

REPO="srvsngh99/mini-krill"
BINARY="minikrill"
DEFAULT_INSTALL_DIR="/usr/local/bin"
FALLBACK_INSTALL_DIR="${HOME}/.local/bin"

# ---------------------------------------------------------------------------
# Helpers
# ---------------------------------------------------------------------------

print_krill() {
  cat <<'ART'

       ___
      /   \
  ~~~|  o  |~~~
      \___/
     /||||||\
    / |||||| \
   ~~~~~~~~~~~~~

   M I N I   K R I L L

ART
}

info()  { echo "[info]  $*"; }
error() { echo "[error] $*" >&2; exit 1; }

need_cmd() {
  command -v "$1" > /dev/null 2>&1 || error "Required command not found: $1"
}

# ---------------------------------------------------------------------------
# Detect platform
# ---------------------------------------------------------------------------

detect_os() {
  local os
  os="$(uname -s | tr '[:upper:]' '[:lower:]')"
  case "$os" in
    linux*)  echo "linux"  ;;
    darwin*) echo "darwin" ;;
    *)       error "Unsupported OS: $os" ;;
  esac
}

detect_arch() {
  local arch
  arch="$(uname -m)"
  case "$arch" in
    x86_64|amd64)   echo "amd64" ;;
    aarch64|arm64)  echo "arm64" ;;
    *)              error "Unsupported architecture: $arch" ;;
  esac
}

# ---------------------------------------------------------------------------
# Download helper - uses curl or wget
# ---------------------------------------------------------------------------

download() {
  local url="$1" dest="$2"
  if command -v curl > /dev/null 2>&1; then
    curl -fsSL -o "$dest" "$url"
  elif command -v wget > /dev/null 2>&1; then
    wget -q -O "$dest" "$url"
  else
    error "Neither curl nor wget found. Install one and retry."
  fi
}

# ---------------------------------------------------------------------------
# Resolve latest release tag
# ---------------------------------------------------------------------------

get_latest_version() {
  local url="https://api.github.com/repos/${REPO}/releases/latest"
  local tag
  if command -v curl > /dev/null 2>&1; then
    tag="$(curl -fsSL "$url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  elif command -v wget > /dev/null 2>&1; then
    tag="$(wget -qO- "$url" | grep '"tag_name"' | head -1 | sed -E 's/.*"tag_name": *"([^"]+)".*/\1/')"
  else
    error "Neither curl nor wget found. Install one and retry."
  fi
  [ -z "$tag" ] && error "Could not determine latest release version."
  echo "$tag"
}

# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------

main() {
  print_krill

  local os arch version archive_name url install_dir tmpdir

  os="$(detect_os)"
  arch="$(detect_arch)"
  version="$(get_latest_version)"

  info "Detected platform: ${os}/${arch}"
  info "Latest version:    ${version}"

  archive_name="${BINARY}_${version#v}_${os}_${arch}.tar.gz"
  url="https://github.com/${REPO}/releases/download/${version}/${archive_name}"

  # Determine install directory
  install_dir="${INSTALL_DIR:-}"
  if [ -z "$install_dir" ]; then
    if [ -w "$DEFAULT_INSTALL_DIR" ]; then
      install_dir="$DEFAULT_INSTALL_DIR"
    else
      install_dir="$FALLBACK_INSTALL_DIR"
      mkdir -p "$install_dir"
    fi
  fi

  # Download and extract
  tmpdir="$(mktemp -d)"
  trap 'rm -rf "$tmpdir"' EXIT

  info "Downloading ${url} ..."
  download "$url" "${tmpdir}/${archive_name}"

  info "Extracting to ${install_dir} ..."
  tar -xzf "${tmpdir}/${archive_name}" -C "$tmpdir"
  mv "${tmpdir}/${BINARY}" "${install_dir}/${BINARY}"
  chmod +x "${install_dir}/${BINARY}"

  # Verify
  if ! "${install_dir}/${BINARY}" --version > /dev/null 2>&1; then
    info "Binary installed but --version check skipped (may need PATH update)."
  fi

  echo ""
  info "Mini Krill ${version} installed to ${install_dir}/${BINARY}"
  echo ""

  # Check PATH
  case ":${PATH}:" in
    *":${install_dir}:"*) ;;
    *)
      echo "  NOTE: ${install_dir} is not in your PATH."
      echo "  Add it with:"
      echo ""
      echo "    export PATH=\"${install_dir}:\$PATH\""
      echo ""
      ;;
  esac

  echo "  Run 'minikrill init' to get started."
  echo ""
}

main "$@"
