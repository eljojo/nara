#!/bin/bash
# Generate API documentation from Go source code
# Output: docs/src/content/docs/api/*.md
#
# Automatically discovers packages in:
# - identity/, messages/, runtime/, utilities/, types/
# - services/*/ (all service subdirectories)

set -e

API_DIR="docs/src/content/docs/api"
mkdir -p "$API_DIR"

# Check if gomarkdoc is available
if ! command -v gomarkdoc &> /dev/null; then
    echo "gomarkdoc not found, skipping API docs generation"
    echo "Install with: go install github.com/princjef/gomarkdoc/cmd/gomarkdoc@latest"
    exit 0
fi

echo "Generating API docs..."

# Find all documented packages
# - Top-level directories with .go files (identity, messages, runtime, utilities, types)
# - Service packages (services/*/  subdirectories)
packages=()

# Top-level packages
for dir in identity messages runtime utilities types; do
    if [ -d "$dir" ] && ls "$dir"/*.go &> /dev/null; then
        packages+=("$dir")
    fi
done

# Service packages
if [ -d "services" ]; then
    for service_dir in services/*/; do
        if [ -d "$service_dir" ] && ls "$service_dir"*.go &> /dev/null; then
            packages+=("$service_dir")
        fi
    done
fi

# Generate docs for each package
for pkg in "${packages[@]}"; do
    # Get package name for output file
    # For services/stash/ -> stash
    # For identity/ -> identity
    pkg_name=$(basename "$pkg")
    output_file="$API_DIR/${pkg_name}.md"

    echo "  Generating docs for $pkg..."
    gomarkdoc --output "$output_file" "./$pkg" 2>/dev/null || true
done

# Add frontmatter for Starlight
for file in "$API_DIR"/*.md; do
    if [ -f "$file" ]; then
        # Get package name from filename
        name=$(basename "$file" .md)
        # Capitalize first letter (portable way)
        title="$(echo "$name" | awk '{print toupper(substr($0,1,1)) substr($0,2)}')"

        # Check if frontmatter already exists
        if ! head -1 "$file" | grep -q "^---"; then
            tmpfile=$(mktemp)
            cat > "$tmpfile" << EOF
---
title: $title
description: API reference for the $name package
---

EOF
            cat "$file" >> "$tmpfile"
            mv "$tmpfile" "$file"
        fi
    fi
done

echo "Generated API docs in $API_DIR for ${#packages[@]} packages"
