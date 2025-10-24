#!/bin/bash
# Version update script for MariaDB Backup Tool

if [ $# -eq 0 ]; then
    echo "Usage: $0 <new_version>"
    echo "Example: $0 1.0.1"
    exit 1
fi

NEW_VERSION=$1
VERSION_FILE="version.txt"

# Update version.txt
echo "$NEW_VERSION" > $VERSION_FILE
echo "✅ Updated $VERSION_FILE to $NEW_VERSION"

# Update README.md if it contains version info
if [ -f "README.md" ]; then
    sed -i.bak "s/[0-9]\+\.[0-9]\+\.[0-9]\+/$NEW_VERSION/g" README.md
    rm -f README.md.bak
    echo "✅ Updated README.md version references"
fi

# Update BUILD.md if it contains version info
if [ -f "BUILD.md" ]; then
    sed -i.bak "s/[0-9]\+\.[0-9]\+\.[0-9]\+/$NEW_VERSION/g" BUILD.md
    rm -f BUILD.md.bak
    echo "✅ Updated BUILD.md version references"
fi

echo ""
echo "🎉 Version updated to $NEW_VERSION!"
echo "📝 Don't forget to:"
echo "   - Commit the changes"
echo "   - Create a git tag: git tag v$NEW_VERSION"
echo "   - Push the tag: git push origin v$NEW_VERSION"
echo "   - Run build scripts to create new binaries"
