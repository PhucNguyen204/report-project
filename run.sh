#!/usr/bin/env bash
# Build from any working directory; publish the PDF only after success.
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
cd "$project_dir"

open_pdf=false
engine=auto
for arg in "$@"; do
  case "$arg" in
    --open) open_pdf=true ;;
    --tectonic) engine=tectonic ;;
    --xelatex) engine=xelatex ;;
    -h|--help)
      printf 'Usage: ./run.sh [--tectonic|--xelatex] [--open]\n'
      exit 0 ;;
    *) printf 'Unknown option: %s\n' "$arg" >&2; exit 2 ;;
  esac
done

if [ "$engine" = auto ]; then
  if command -v tectonic >/dev/null 2>&1; then
    engine=tectonic
  else
    engine=xelatex
  fi
fi
mkdir -p build output/pdf
if [ "$engine" = tectonic ]; then
  command -v tectonic >/dev/null 2>&1 || { printf 'Run ./setup.sh first.\n' >&2; exit 1; }
  tectonic --keep-logs --keep-intermediates --outdir build main.tex
else
  if ! command -v latexmk >/dev/null 2>&1 || ! command -v xelatex >/dev/null 2>&1; then
    printf 'No LaTeX compiler found. Run ./setup.sh first.\n' >&2
    exit 1
  fi
  latexmk -xelatex -interaction=nonstopmode -halt-on-error -outdir=build main.tex
fi
test -s build/main.pdf
cp build/main.pdf output/pdf/bao-cao.pdf
printf '\nPDF: %s/output/pdf/bao-cao.pdf\n' "$project_dir"
printf 'Build log: %s/build/main.log\n' "$project_dir"
if "$open_pdf"; then
  case "$(uname -s)" in
    Darwin) open output/pdf/bao-cao.pdf ;;
    Linux) xdg-open output/pdf/bao-cao.pdf ;;
    *) printf 'Open the PDF manually using the path above.\n' ;;
  esac
fi
