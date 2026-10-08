#!/usr/bin/env bash
# Install the report compiler on macOS. Existing TeX Live is also supported.
set -euo pipefail

preview=false
diagrams=false
for arg in "$@"; do
  case "$arg" in
    --with-preview) preview=true ;;
    --with-diagrams) diagrams=true ;;
    -h|--help)
      printf 'Usage: ./setup.sh [--with-preview] [--with-diagrams]\n'
      exit 0 ;;
    *) printf 'Unknown option: %s\n' "$arg" >&2; exit 2 ;;
  esac
done

packages=()
if ! command -v tectonic >/dev/null 2>&1; then
  if ! command -v latexmk >/dev/null 2>&1 || ! command -v xelatex >/dev/null 2>&1; then
    packages+=(tectonic)
  fi
fi
if "$preview" && ! command -v pdftoppm >/dev/null 2>&1; then
  packages+=(poppler)
fi
if "$diagrams"; then
  command -v plantuml >/dev/null 2>&1 || packages+=(plantuml)
  command -v dot >/dev/null 2>&1 || packages+=(graphviz)
fi
if [ "${#packages[@]}" -gt 0 ]; then
  if ! command -v brew >/dev/null 2>&1; then
    printf 'Install Homebrew (https://brew.sh), then rerun this script.\n' >&2
    printf 'Alternatively, install TeX Live with XeLaTeX and latexmk.\n' >&2
    exit 1
  fi
  brew install "${packages[@]}"
fi
if [ "$(uname -s)" = Darwin ] && command -v brew >/dev/null 2>&1; then
  if ! brew list --cask font-freefont >/dev/null 2>&1; then
    brew install --cask font-freefont
  fi
fi
if command -v tectonic >/dev/null 2>&1; then
  tectonic --version
else
  latexmk -v
fi
printf '\nSetup complete. Run ./run.sh to create output/pdf/bao-cao.pdf.\n'
printf 'The first Tectonic build needs internet to download LaTeX packages.\n'
