#!/bin/sh
# eme installer — https://eme.jinmu.me
#
#   curl -fsSL https://eme.jinmu.me/install.sh | sh
#   curl -fsSL https://eme.jinmu.me/install.sh | sh -s -- v0.1.0   # pin a version
#   EME_INSTALL_DIR="$HOME/bin" curl -fsSL https://eme.jinmu.me/install.sh | sh
#
# Downloads the matching release archive from GitHub, verifies its checksum,
# and installs the `eme` binary. macOS and Linux, amd64 and arm64.
set -eu

REPO="JinmuGo/eme"
BIN="eme"

# --- output ---------------------------------------------------------------
if [ -t 1 ]; then
  BOLD=$(printf '\033[1m'); DIM=$(printf '\033[2m'); RED=$(printf '\033[31m')
  GREEN=$(printf '\033[32m'); AMBER=$(printf '\033[33m'); RESET=$(printf '\033[0m')
else
  BOLD=''; DIM=''; RED=''; GREEN=''; AMBER=''; RESET=''
fi
say()  { printf '%s\n' "$*"; }
warn() { printf '%swarning:%s %s\n' "$AMBER" "$RESET" "$*" >&2; }
die()  { printf '%serror:%s %s\n' "$RED" "$RESET" "$*" >&2; exit 1; }
have() { command -v "$1" >/dev/null 2>&1; }

fetch()    { if have curl; then curl -fsSL "$1"; else wget -qO- "$1"; fi; }
download() { if have curl; then curl -fsSL -o "$2" "$1"; else wget -qO "$2" "$1"; fi; }

main() {
  have curl || have wget || die "need curl or wget"
  have tar || die "need tar"

  # --- detect platform (matches GoReleaser archive names) ---
  os=$(uname -s)
  arch=$(uname -m)
  case "$os" in
    Darwin) os="Darwin" ;;
    Linux)  os="Linux" ;;
    *) die "unsupported OS: $os (eme supports macOS and Linux)" ;;
  esac
  case "$arch" in
    x86_64|amd64)  arch="x86_64" ;;
    arm64|aarch64) arch="arm64" ;;
    *) die "unsupported architecture: $arch" ;;
  esac

  # --- resolve version (arg > env > latest release) ---
  version="${1:-${EME_VERSION:-}}"
  if [ -z "$version" ]; then
    say "${DIM}resolving latest release...${RESET}"
    version=$(fetch "https://api.github.com/repos/$REPO/releases/latest" 2>/dev/null \
      | grep -m1 '"tag_name"' \
      | sed -E 's/.*"tag_name"[[:space:]]*:[[:space:]]*"([^"]+)".*/\1/')
    case "$version" in
      v*) ;;
      *) die "no published release found for $REPO yet — pass one explicitly: sh -s -- v0.1.0" ;;
    esac
  fi

  asset="${BIN}_${os}_${arch}.tar.gz"
  base="https://github.com/$REPO/releases/download/$version"

  tmp=$(mktemp -d 2>/dev/null || mktemp -d -t eme)
  trap 'rm -rf "$tmp"' EXIT INT TERM

  say "${BOLD}eme${RESET} ${version}  ${DIM}(${os}/${arch})${RESET}"
  say "${DIM}> $base/$asset${RESET}"
  download "$base/$asset" "$tmp/$asset" || die "download failed: $base/$asset"

  # --- verify checksum (best effort) ---
  if download "$base/checksums.txt" "$tmp/checksums.txt" 2>/dev/null; then
    if have sha256sum; then sum=$(sha256sum "$tmp/$asset" | awk '{print $1}')
    elif have shasum; then sum=$(shasum -a 256 "$tmp/$asset" | awk '{print $1}')
    else sum=''; fi
    if [ -n "$sum" ]; then
      grep -q "$sum" "$tmp/checksums.txt" || die "checksum mismatch for $asset"
      say "${DIM}checksum ok${RESET}"
    fi
  fi

  tar -xzf "$tmp/$asset" -C "$tmp" || die "failed to extract $asset"
  [ -f "$tmp/$BIN" ] || die "archive did not contain $BIN"
  chmod +x "$tmp/$BIN"

  # --- choose install dir ---
  dir="${EME_INSTALL_DIR:-}"
  if [ -z "$dir" ]; then
    if [ -w /usr/local/bin ]; then dir="/usr/local/bin"; else dir="$HOME/.local/bin"; fi
  fi
  mkdir -p "$dir" 2>/dev/null || true

  if [ -w "$dir" ]; then
    mv "$tmp/$BIN" "$dir/$BIN"
  elif have sudo; then
    warn "$dir is not writable; using sudo"
    sudo mkdir -p "$dir" && sudo mv "$tmp/$BIN" "$dir/$BIN"
  else
    die "$dir is not writable and sudo is unavailable — set EME_INSTALL_DIR to a writable directory"
  fi

  say "${GREEN}installed${RESET} ${BOLD}$BIN${RESET} -> $dir/$BIN"

  # --- runtime prerequisites ---
  have tmux || warn "tmux not found — eme needs tmux >= 3.0 at runtime (e.g. brew install tmux)"
  have git  || warn "git not found — eme needs git >= 2.30 at runtime"

  # --- PATH hint ---
  case ":$PATH:" in
    *":$dir:"*) : ;;
    *) warn "$dir is not on your PATH — add it: export PATH=\"$dir:\$PATH\"" ;;
  esac

  say ""
  say "  run ${BOLD}eme${RESET} in a git repo to start  ${DIM}(eme --help)${RESET}"
}

main "$@"
