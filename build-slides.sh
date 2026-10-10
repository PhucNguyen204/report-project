#!/usr/bin/env bash
# Build the filled HUST Beamer template and publish only after success.
set -euo pipefail
project_dir="$(cd -- "$(dirname -- "${BASH_SOURCE[0]}")" && pwd)"
mkdir -p "$project_dir/build/slides" "$project_dir/output/pdf"
cd "$project_dir/HUST_THEME_BEAMER___LATEX_BY_FRC"
tectonic --keep-logs --keep-intermediates --outdir "$project_dir/build/slides" Slide_HUST.tex
test -s "$project_dir/build/slides/Slide_HUST.pdf"
cp "$project_dir/build/slides/Slide_HUST.pdf" "$project_dir/output/pdf/slide-chuong-1-2.pdf"
printf '\nPDF: %s/output/pdf/slide-chuong-1-2.pdf\n' "$project_dir"
