#!/bin/bash
#
# This script downloads the Lua source code, extracts it,
# and places the necessary files into the '$LUA_SRC_DIR' directory.
#
# last update: 2025.11.17.

set -e

LUA_VERSION="5.5.0"
LUA_URL="http://www.lua.org/ftp/lua-${LUA_VERSION}.tar.gz"
LUA_TARBALL="lua-${LUA_VERSION}.tar.gz"
LUA_DIR="lua-${LUA_VERSION}"
LUA_SRC_DIR="luasrc"

# Clean up previous downloads and directories if they exist
echo "Cleaning up old files..."
rm -f "${LUA_TARBALL}"
rm -rf "${LUA_DIR}"
rm -rf "${LUA_SRC_DIR}/*.h"
rm -rf "${LUA_SRC_DIR}/*.c"

echo "Downloading Lua ${LUA_VERSION} from ${LUA_URL}..."
curl -L -R -O "${LUA_URL}"

if [ ! -f "${LUA_TARBALL}" ]; then
    echo "Error: Failed to download Lua source."
    exit 1
fi

echo "Extracting source..."
tar zxf "${LUA_TARBALL}"

echo "Setting up '${LUA_SRC_DIR}' directory..."
mkdir -p "${LUA_SRC_DIR}"
mv "${LUA_DIR}/src/"*.h "${LUA_SRC_DIR}/"
mv "${LUA_DIR}/src/"*.c "${LUA_SRC_DIR}/"

# Remove the standalone interpreter and compiler, as we are embedding Lua
echo "Removing unused files (lua.c, luac.c)..."
rm -f "${LUA_SRC_DIR}/lua.c"
rm -f "${LUA_SRC_DIR}/luac.c"

echo "Cleaning up temporary files..."
rm -rf "${LUA_DIR}"
rm -f "${LUA_TARBALL}"

echo "Lua source code is ready in the '${LUA_SRC_DIR}' directory."
