#!/bin/sh
# Installs a pinned development toolchain in an explicitly supplied directory.
# Does not edit shell profiles, install system packages, or select signing keys.
set -eu
task_tools=${1:?absolute project-local tool directory required}
case "$task_tools" in /*) ;; *) echo 'absolute tool directory required' >&2; exit 1 ;; esac
case "$(uname -s)-$(uname -m)" in Darwin-arm64) task_host=aarch64-apple-darwin ;; *) echo 'unsupported development host' >&2; exit 1 ;; esac
mkdir -p "$task_tools"
task_installer="$task_tools/rustup-init"
if [ ! -x "$task_tools/cargo/bin/rustup" ]; then
  task_url="https://static.rust-lang.org/rustup/dist/$task_host/rustup-init"
  curl --fail --location --proto '=https' --tlsv1.2 "$task_url" -o "$task_installer"
  task_expected=$(curl --fail --location --proto '=https' --tlsv1.2 "$task_url.sha256" | cut -d ' ' -f 1)
  task_actual=$(shasum -a 256 "$task_installer" | cut -d ' ' -f 1)
  [ "$task_actual" = "$task_expected" ] || { echo 'rustup checksum mismatch' >&2; exit 1; }
  chmod 700 "$task_installer"
  CARGO_HOME="$task_tools/cargo" RUSTUP_HOME="$task_tools/rustup" "$task_installer" -y --no-modify-path --profile minimal --default-toolchain 1.99.1
fi
CARGO_HOME="$task_tools/cargo" RUSTUP_HOME="$task_tools/rustup" "$task_tools/cargo/bin/rustup" target add --toolchain 1.99.1 aarch64-apple-ios-sim
CARGO_HOME="$task_tools/cargo" RUSTUP_HOME="$task_tools/rustup" "$task_tools/cargo/bin/rustc" --version
