#!/usr/bin/env python3
"""Build, package, verify, and index DSH engine archives.

A DSH archive is CLIProxyAPI_<version>_dsh_<platform>-<arch>.tar.gz with exactly three
regular files: the engine binary (cli-proxy-api or cli-proxy-api.exe), LICENSE, and
manifest.json = {sourceSHA, platform, arch, filename, sha256}. platform and arch use
Node.js names. sha256 is the digest of the binary as shipped (after any code signing).
The DeepSeek Harness account-pool installer (product/packages/account-pool/scripts/
build-engine.mjs) extracts those three files and rejects any identity mismatch.

Archives are deterministic for identical binary bytes: members are ordered, owned by
0:0, carry fixed modes, and use the source commit time as mtime; the gzip header has no
name or timestamp.
"""

from __future__ import annotations

import argparse
import gzip
import hashlib
import io
import json
import os
import re
import subprocess
import sys
import tarfile
from pathlib import Path

TARGETS = (
    ("darwin", "arm64"),
    ("darwin", "x64"),
    ("linux", "arm64"),
    ("linux", "x64"),
    ("win32", "x64"),
)
GOOS = {"darwin": "darwin", "linux": "linux", "win32": "windows"}
GOARCH = {"arm64": "arm64", "x64": "amd64"}
SHA256_RE = re.compile(r"^[a-f0-9]{64}$")
COMMIT_RE = re.compile(r"^[a-f0-9]{40}$")
TAG_RE = re.compile(r"^v[0-9A-Za-z.-]+$")
MANIFEST_KEYS = ("sourceSHA", "platform", "arch", "filename", "sha256")


def fail(message: str) -> None:
    raise SystemExit(f"dsh-archive: {message}")


def check_target(platform: str, arch: str) -> str:
    """Return the binary filename for a supported Node platform/arch pair."""
    if (platform, arch) not in TARGETS:
        fail(f"unsupported target {platform}-{arch}")
    return "cli-proxy-api.exe" if platform == "win32" else "cli-proxy-api"


def asset_name(version: str, platform: str, arch: str) -> str:
    return f"CLIProxyAPI_{version}_dsh_{platform}-{arch}.tar.gz"


def sha256_file(path: Path) -> str:
    digest = hashlib.sha256()
    with path.open("rb") as handle:
        for chunk in iter(lambda: handle.read(1 << 20), b""):
            digest.update(chunk)
    return digest.hexdigest()


def git(*args: str) -> str:
    return subprocess.run(["git", *args], check=True, capture_output=True, text=True).stdout.strip()


def cmd_build(args: argparse.Namespace) -> None:
    """Cross-compile the engine with CGO disabled so every target builds on any runner."""
    filename = check_target(args.platform, args.arch)
    commit = git("rev-parse", "HEAD")
    commit_date = git("show", "-s", "--format=%cI", "HEAD")
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    env = dict(os.environ, CGO_ENABLED="0", GOOS=GOOS[args.platform], GOARCH=GOARCH[args.arch])
    ldflags = f"-s -w -X main.Version={args.version} -X main.Commit={commit[:8]} -X main.BuildDate={commit_date}"
    subprocess.run(
        ["go", "build", "-trimpath", "-ldflags", ldflags, "-o", str(out / filename), "./cmd/server"],
        check=True,
        env=env,
    )
    print(json.dumps({"binary": str(out / filename), "sourceSHA": commit}))


def cmd_package(args: argparse.Namespace) -> None:
    filename = check_target(args.platform, args.arch)
    if not COMMIT_RE.match(args.source_sha):
        fail("--source-sha must be a full 40-character commit SHA")
    binary = Path(args.binary)
    if binary.name != filename:
        fail(f"binary must be named {filename}")
    manifest = {
        "sourceSHA": args.source_sha,
        "platform": args.platform,
        "arch": args.arch,
        "filename": filename,
        "sha256": sha256_file(binary),
    }
    members = (
        (filename, binary.read_bytes(), 0o644 if args.platform == "win32" else 0o755),
        ("LICENSE", Path(args.license).read_bytes(), 0o644),
        ("manifest.json", (json.dumps(manifest, indent=2) + "\n").encode(), 0o644),
    )
    out = Path(args.out)
    out.mkdir(parents=True, exist_ok=True)
    archive = out / asset_name(args.version, args.platform, args.arch)
    with archive.open("wb") as raw, gzip.GzipFile(filename="", mode="wb", fileobj=raw, mtime=0) as zipped:
        with tarfile.open(fileobj=zipped, mode="w", format=tarfile.USTAR_FORMAT) as tar:
            for name, data, mode in members:
                info = tarfile.TarInfo(name)
                info.size = len(data)
                info.mode = mode
                info.mtime = args.mtime
                info.uid = info.gid = 0
                info.uname = info.gname = ""
                tar.addfile(info, io.BytesIO(data))
    identity = dict(manifest, asset=archive.name, assetSha256=sha256_file(archive))
    print(json.dumps(identity))


def verify_archive(archive: Path, platform: str, arch: str, source_sha: str) -> dict:
    """Check one archive against the installer's requirements and return its target identity."""
    filename = check_target(platform, arch)
    expected_names = {filename, "LICENSE", "manifest.json"}
    contents: dict[str, bytes] = {}
    modes: dict[str, int] = {}
    with tarfile.open(archive, mode="r:gz") as tar:
        for member in tar.getmembers():
            if not member.isfile() or member.name not in expected_names or member.name in contents:
                fail(f"{archive.name}: unexpected member {member.name!r}")
            extracted = tar.extractfile(member)
            if extracted is None:
                fail(f"{archive.name}: unreadable member {member.name!r}")
            contents[member.name] = extracted.read()
            modes[member.name] = member.mode
    if set(contents) != expected_names:
        fail(f"{archive.name}: members {sorted(contents)} != {sorted(expected_names)}")
    manifest = json.loads(contents["manifest.json"])
    if not isinstance(manifest, dict) or tuple(manifest) != MANIFEST_KEYS:
        fail(f"{archive.name}: manifest.json keys must be {list(MANIFEST_KEYS)}")
    binary_sha = hashlib.sha256(contents[filename]).hexdigest()
    expected = {"sourceSHA": source_sha, "platform": platform, "arch": arch, "filename": filename, "sha256": binary_sha}
    if manifest != expected:
        fail(f"{archive.name}: manifest {manifest} != {expected}")
    if platform != "win32" and not modes[filename] & 0o111:
        fail(f"{archive.name}: {filename} is not executable")
    if not contents["LICENSE"].strip():
        fail(f"{archive.name}: LICENSE is empty")
    return dict(manifest, asset=archive.name, assetSha256=sha256_file(archive))


def cmd_verify(args: argparse.Namespace) -> None:
    print(json.dumps(verify_archive(Path(args.archive), args.platform, args.arch, args.source_sha)))


def cmd_index(args: argparse.Namespace) -> None:
    """Verify all five archives in --dir and write dsh-cliproxyapi.json and checksums.txt there."""
    if not TAG_RE.match(args.tag):
        fail(f"invalid tag {args.tag!r}")
    if not COMMIT_RE.match(args.source_sha):
        fail("--source-sha must be a full 40-character commit SHA")
    directory = Path(args.dir)
    version = args.tag[1:]
    targets = []
    for platform, arch in TARGETS:
        archive = directory / asset_name(version, platform, arch)
        if not archive.is_file():
            fail(f"missing {archive.name}")
        targets.append(verify_archive(archive, platform, arch, args.source_sha))
    index = {"tag": args.tag, "sourceSHA": args.source_sha, "repository": args.repository, "targets": targets}
    (directory / "dsh-cliproxyapi.json").write_text(json.dumps(index, indent=2) + "\n")
    lines = [f"{target['assetSha256']}  {target['asset']}" for target in sorted(targets, key=lambda item: item["asset"])]
    (directory / "checksums.txt").write_text("\n".join(lines) + "\n")
    print(json.dumps(index, indent=2))


def main(argv: list[str]) -> None:
    parser = argparse.ArgumentParser(description=__doc__.splitlines()[0])
    commands = parser.add_subparsers(dest="command", required=True)

    build = commands.add_parser("build", help="compile one target binary from the current checkout")
    build.add_argument("--platform", required=True)
    build.add_argument("--arch", required=True)
    build.add_argument("--version", required=True)
    build.add_argument("--out", required=True)
    build.set_defaults(func=cmd_build)

    package = commands.add_parser("package", help="write one DSH archive from a finished binary")
    package.add_argument("--platform", required=True)
    package.add_argument("--arch", required=True)
    package.add_argument("--version", required=True)
    package.add_argument("--source-sha", required=True)
    package.add_argument("--binary", required=True)
    package.add_argument("--license", default="LICENSE")
    package.add_argument("--mtime", type=int, required=True, help="member mtime, normally the source commit time")
    package.add_argument("--out", required=True)
    package.set_defaults(func=cmd_package)

    verify = commands.add_parser("verify", help="check one archive against the installer contract")
    verify.add_argument("--archive", required=True)
    verify.add_argument("--platform", required=True)
    verify.add_argument("--arch", required=True)
    verify.add_argument("--source-sha", required=True)
    verify.set_defaults(func=cmd_verify)

    index = commands.add_parser("index", help="verify all five archives and write release metadata")
    index.add_argument("--tag", required=True)
    index.add_argument("--source-sha", required=True)
    index.add_argument("--repository", required=True)
    index.add_argument("--dir", required=True)
    index.set_defaults(func=cmd_index)

    args = parser.parse_args(argv)
    args.func(args)


if __name__ == "__main__":
    main(sys.argv[1:])
