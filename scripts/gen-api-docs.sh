#!/bin/bash
# Generate API documentation from Go source code
# Output: docs/src/content/docs/api/*.md

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

# Generate docs for messages package
gomarkdoc --output "$API_DIR/messages.md" ./messages 2>/dev/null || true

# Generate docs for runtime package
gomarkdoc --output "$API_DIR/runtime.md" ./runtime 2>/dev/null || true

# Generate docs for utilities package
gomarkdoc --output "$API_DIR/utilities.md" ./utilities 2>/dev/null || true

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

echo "Generated API docs in $API_DIR"
