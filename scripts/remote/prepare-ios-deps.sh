#!/bin/sh
# Project-local tools only. Existing Homebrew libraries are read, never changed.
set -eu
umask 077
export LANG=en_US.UTF-8
task_repo=$(CDPATH= cd -- "$(dirname -- "$0")/../.." && pwd -P)
task_prefix="$task_repo/.local/toolchains/ios-deps"
task_lock="$task_repo/tools/mobile-deps.lock.json"
test "$(uname -s)" = Darwin || { printf 'This preparation requires macOS.\n' >&2; exit 2; }
test "$(uname -m)" = arm64 || { printf 'This profile is scoped to the configured arm64 Mac.\n' >&2; exit 2; }
test -f "$task_lock" || { printf 'Missing mobile dependency lock.\n' >&2; exit 2; }
test -x /opt/homebrew/opt/openssl@3/bin/openssl || { printf 'Existing OpenSSL 3 is missing; no installer was run.\n' >&2; exit 2; }
for task_pkg in libplist libimobiledevice-glue libusbmuxd libtasn1 libtatsu; do
  test -d "/opt/homebrew/opt/$task_pkg/lib/pkgconfig" || { printf 'Existing %s development files are missing; no installer was run.\n' "$task_pkg" >&2; exit 2; }
done
mkdir -p "$task_prefix"
task_prefix=$(CDPATH= cd -- "$task_prefix" && pwd -P)
case "$task_prefix" in "$task_repo"/.local/toolchains/*) ;; *) printf 'Tool directory leaves the project.\n' >&2; exit 2 ;; esac
task_guard="$task_prefix/.prepare-lock"
if ! mkdir "$task_guard" 2>/dev/null; then printf 'Another preparation or its retained lock exists: %s\n' "$task_guard" >&2; exit 2; fi
printf '%s\n' "$$" > "$task_guard/pid"
trap 'rm -f "$task_guard/pid"; rmdir "$task_guard" 2>/dev/null || true' EXIT HUP INT TERM
mkdir -p "$task_prefix/downloads" "$task_prefix/build" "$task_prefix/logs" "$task_prefix/native"
task_native="$task_prefix/native"

value() {
  /usr/bin/python3 -c 'import json,sys; print(json.load(open(sys.argv[1]))[sys.argv[2]][sys.argv[3]])' "$task_lock" "$1" "$2"
}
archive() {
  task_name=$1
  task_url=$(value "$task_name" url)
  task_sha=$(value "$task_name" sha256)
  task_file="$task_prefix/downloads/${task_url##*/}"
  if ! test -f "$task_file"; then
    printf 'Downloading %s\n' "$task_name" >&2
    curl --proto '=https' --tlsv1.2 --fail --location --silent --show-error --retry 2 "$task_url" -o "$task_file.partial"
    mv "$task_file.partial" "$task_file"
  fi
  task_actual=$(shasum -a 256 "$task_file" | awk '{print $1}')
  test "$task_actual" = "$task_sha" || { printf '%s checksum mismatch; nothing was extracted.\n' "$task_name" >&2; exit 2; }
  printf '%s\n' "$task_file"
}
source_dir() {
  task_name=$1
  task_version=$(value "$task_name" version)
  task_file=$(archive "$task_name")
  task_dir="$task_prefix/build/$task_name-$task_version"
  if test -d "$task_dir"; then
    test -f "$task_dir/.archive-sha256" && test "$(cat "$task_dir/.archive-sha256")" = "$(value "$task_name" sha256)" || { printf 'Existing source directory has no matching marker: %s\n' "$task_dir" >&2; exit 2; }
  else
    mkdir "$task_dir"
    tar -xf "$task_file" -C "$task_dir" --strip-components=1
    value "$task_name" sha256 > "$task_dir/.archive-sha256"
  fi
  printf '%s\n' "$task_dir"
}

export PATH="$task_native/bin:/opt/homebrew/bin:$PATH"
task_pc="$task_native/lib/pkgconfig:/opt/homebrew/opt/openssl@3/lib/pkgconfig:/opt/homebrew/opt/libplist/lib/pkgconfig:/opt/homebrew/opt/libimobiledevice-glue/lib/pkgconfig:/opt/homebrew/opt/libusbmuxd/lib/pkgconfig:/opt/homebrew/opt/libtasn1/lib/pkgconfig:/opt/homebrew/opt/libtatsu/lib/pkgconfig:/opt/homebrew/Library/Homebrew/os/mac/pkgconfig/26"
export PKG_CONFIG_PATH="$task_pc"
export PKG_CONFIG_LIBDIR="$task_pc"
if ! test -x "$task_native/bin/pkgconf"; then
  task_source=$(source_dir pkgconf)
  printf 'Building project-local pkgconf\n'
  (cd "$task_source" && ./configure --prefix="$task_native" --with-pkg-config-dir="$task_pc" && make -j2 && make install) > "$task_prefix/logs/pkgconf.log" 2>&1
fi
if ! test -e "$task_native/bin/pkg-config"; then ln -s pkgconf "$task_native/bin/pkg-config"; fi
if ! test -f "$task_native/lib/libyaml.dylib"; then
  task_source=$(source_dir libyaml)
  printf 'Building project-local libyaml\n'
  (cd "$task_source" && ./configure --prefix="$task_native" && make -j2 && make install) > "$task_prefix/logs/libyaml.log" 2>&1
fi
if ! test -x "$task_native/bin/idevicesyslog"; then
  task_source=$(source_dir libimobiledevice)
  printf 'Building project-local libimobiledevice with existing OpenSSL 3\n'
  (cd "$task_source" && ./configure --prefix="$task_native" --without-cython --enable-debug && make -j2 && make install) > "$task_prefix/logs/libimobiledevice.log" 2>&1
fi
if ! test -x "$task_prefix/ruby/bin/ruby"; then
  task_source=$(source_dir ruby)
  printf 'Building project-local Ruby; compiler output stays in logs/ruby.log\n'
  (cd "$task_source" && ./configure --prefix="$task_prefix/ruby" --disable-install-doc --disable-yjit --with-openssl-dir=/opt/homebrew/opt/openssl@3 --with-libyaml-dir="$task_native" && make -j2 && make install) > "$task_prefix/logs/ruby.log" 2>&1
fi
export GEM_HOME="$task_prefix/gems"
export GEM_PATH="$GEM_HOME:$task_prefix/ruby/lib/ruby/gems/3.4.0"
export GEM_SPEC_CACHE="$task_prefix/gem-cache"
export CP_HOME_DIR="$task_prefix/cocoapods"
export COCOAPODS_DISABLE_STATS=true
export COCOAPODS_SKIP_UPDATE_MESSAGE=true
export PATH="$task_prefix/ruby/bin:$task_prefix/bin:$PATH"
if test -f /etc/ssl/cert.pem; then export SSL_CERT_FILE=/etc/ssl/cert.pem; fi
if ! test -x "$task_prefix/bin/pod"; then
  task_gem=$(archive cocoapods)
  printf 'Installing CocoaPods into project-local gems\n'
  "$task_prefix/ruby/bin/gem" install "$task_gem" --install-dir "$GEM_HOME" --bindir "$task_prefix/bin" --no-document > "$task_prefix/logs/cocoapods.log" 2>&1
fi
"$task_native/bin/pkgconf" --version
"$task_native/bin/idevicesyslog" --version
"$task_prefix/ruby/bin/ruby" --version
"$task_prefix/bin/pod" --version
/usr/bin/python3 - "$task_prefix" > "$task_prefix/env.sh" <<'PY'
import shlex,sys
p=sys.argv[1]
values={"MEWDEX_IOS_TOOLS":p,"GEM_HOME":p+"/gems","GEM_PATH":p+"/gems:"+p+"/ruby/lib/ruby/gems/3.4.0","GEM_SPEC_CACHE":p+"/gem-cache","CP_HOME_DIR":p+"/cocoapods","COCOAPODS_DISABLE_STATS":"true","COCOAPODS_SKIP_UPDATE_MESSAGE":"true","SSL_CERT_FILE":"/etc/ssl/cert.pem","LANG":"en_US.UTF-8"}
for key,value in values.items(): print("export "+key+"="+shlex.quote(value))
print("export PATH="+shlex.quote(p+"/native/bin:"+p+"/ruby/bin:"+p+"/bin:/opt/homebrew/bin:")+"\"$PATH\"")
PY
printf 'Prepared project-local iOS tools: %s\n' "$task_prefix"
