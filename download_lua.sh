#!/bin/bash

# This script downloads the Lua source code, extracts it,
# and places the necessary files into the 'luasrc' directory.

set -e

LUA_VERSION="5.4.8"
LUA_URL="http://www.lua.org/ftp/lua-${LUA_VERSION}.tar.gz"
LUA_TARBALL="lua-${LUA_VERSION}.tar.gz"
LUA_DIR="lua-${LUA_VERSION}"

# Clean up previous downloads and directories if they exist
echo "Cleaning up old files..."
rm -f "${LUA_TARBALL}"
rm -rf "${LUA_DIR}"
rm -rf "luasrc/*.h"
rm -rf "luasrc/*.c"

echo "Downloading Lua ${LUA_VERSION} from ${LUA_URL}..."
curl -L -R -O "${LUA_URL}"

if [ ! -f "${LUA_TARBALL}" ]; then
    echo "Error: Failed to download Lua source."
    exit 1
fi

echo "Extracting source..."
tar zxf "${LUA_TARBALL}"

echo "Setting up 'src' directory..."
mkdir -p luasrc
mv "${LUA_DIR}/src/"*.h luasrc/
mv "${LUA_DIR}/src/"*.c luasrc/

# Remove the standalone interpreter and compiler, as we are embedding Lua
echo "Removing unused files (lua.c, luac.c)..."
rm -f luasrc/lua.c
rm -f luasrc/luac.c

echo "Cleaning up temporary files..."
rm -rf "${LUA_DIR}"
rm -f "${LUA_TARBALL}"

echo "Lua source code is ready in the 'src' directory."
