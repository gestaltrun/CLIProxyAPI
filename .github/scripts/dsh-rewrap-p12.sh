#!/usr/bin/env bash
# Re-wrap a PKCS#12 file whose export password is empty so macOS `security import` accepts it.
#
# Usage: DSH_P12_PASSWORD=<new password> dsh-rewrap-p12.sh <input.p12> <output.p12>
#
# `security import` rejects some empty-password PKCS#12 files with "MAC verification failed",
# including OpenSSL 3 default exports (AES-256 and PBKDF2 with a SHA-256 MAC). This script
# reads <input.p12> with an empty password, then with no password, with and without -legacy,
# using each available openssl in turn: Homebrew OpenSSL 3, openssl on PATH, and
# /usr/bin/openssl (LibreSSL on macOS). It writes <output.p12> encrypted with
# DSH_P12_PASSWORD using PBE-SHA1-3DES and a SHA-1 MAC, which `security import` accepts, and
# checks that the output reads back with that password.
#
# The decrypted key exists only in a mode-0700 temporary directory removed on exit. openssl
# output is discarded; only the selected openssl version and flags are printed.
set -euo pipefail

if [[ $# -ne 2 ]]; then
  echo "usage: DSH_P12_PASSWORD=<password> $0 <input.p12> <output.p12>" >&2
  exit 2
fi
input="$1"
output="$2"
if [[ -z "${DSH_P12_PASSWORD:-}" ]]; then
  echo "DSH_P12_PASSWORD must be set to the new export password" >&2
  exit 2
fi

umask 077
tmp="$(mktemp -d)"
trap 'rm -rf "$tmp"' EXIT
pem="$tmp/bundle.pem"

candidates=()
if command -v brew >/dev/null 2>&1; then
  brew_prefix="$(brew --prefix openssl@3 2>/dev/null || true)"
  if [[ -n "$brew_prefix" ]]; then candidates+=("$brew_prefix/bin/openssl"); fi
fi
if command -v openssl >/dev/null 2>&1; then candidates+=("$(command -v openssl)"); fi
candidates+=(/usr/bin/openssl)

# Each entry is one set of password arguments for reading the input.
read_passwords=("-passin pass:" "")
read_extras=("" "-legacy")
write_flags=(
  "-certpbe PBE-SHA1-3DES -keypbe PBE-SHA1-3DES -macalg sha1"
  "-certpbe PBE-SHA1-3DES -keypbe PBE-SHA1-3DES"
)

tried=()
for ossl in "${candidates[@]}"; do
  [[ -x "$ossl" ]] || continue
  for seen in "${tried[@]+"${tried[@]}"}"; do [[ "$seen" == "$ossl" ]] && continue 2; done
  tried+=("$ossl")
  for password_args in "${read_passwords[@]}"; do
    for extra in "${read_extras[@]}"; do
      read -r -a read_args <<<"$password_args $extra"
      rm -f "$pem"
      "$ossl" pkcs12 -in "$input" "${read_args[@]}" -nodes -out "$pem" </dev/null >/dev/null 2>&1 || continue
      grep -q -- '-----BEGIN CERTIFICATE-----' "$pem" || continue
      grep -Eq -- '-----BEGIN (RSA |EC |ENCRYPTED )?PRIVATE KEY-----' "$pem" || continue
      for flags in "${write_flags[@]}"; do
        read -r -a write_args <<<"$flags"
        rm -f "$output"
        "$ossl" pkcs12 -export -in "$pem" -out "$output" -passout env:DSH_P12_PASSWORD "${write_args[@]}" </dev/null >/dev/null 2>&1 || continue
        "$ossl" pkcs12 -in "$output" -passin env:DSH_P12_PASSWORD -noout </dev/null >/dev/null 2>&1 || continue
        echo "Re-wrapped the empty-password PKCS#12 with $("$ossl" version 2>/dev/null) (read: ${password_args:-no password}${extra:+ $extra}; write: $flags)."
        exit 0
      done
    done
  done
done

rm -f "$output"
echo "Could not read CSC_LINK as a PKCS#12 file with an empty password using: ${tried[*]:-no openssl found}" >&2
exit 1
