#!/usr/bin/env python3
"""Update only product branding in published plugins; default is read-only."""

import argparse
import copy
import hashlib
import json
import os
import pathlib
import re
import tempfile
import zipfile


PROJECT = pathlib.Path(__file__).resolve().parents[1]
TEXT_ENTRIES = {"manifest.json", "README.md", "docs/interface.md"}
# ponytail: AutoDL has no source directory in this checkout. Its published archive
# is authoritative until that source is restored; no other missing source is allowed.
ARCHIVE_ONLY = {"autodl-comfyui"}
BRAND = re.compile(r'''https?://[^\s\\<>"'`]+|摄影策略|影策|Yingce''')
METADATA = ("filename", "date_time", "compress_type", "comment", "extra",
            "create_system", "create_version", "extract_version", "internal_attr", "external_attr")


def branded(data):
    return BRAND.sub(lambda m: "源铭振跃" if m[0] in ("影策", "Yingce") else m[0],
                     data.decode("utf-8")).encode("utf-8")


def run(root, write=False):
    root = pathlib.Path(root).resolve(strict=True)
    if root == PROJECT or not root.is_relative_to(PROJECT) or not root.is_dir():
        raise ValueError("package root must be a directory inside this project")
    archives = sorted(root.glob("*.yingce-plugin"))
    if not archives:
        raise ValueError("no plugin archives found")
    results = []
    for archive in archives:
        if archive.resolve().parent != root:
            raise ValueError(f"archive escapes package root: {archive.name}")
        source = (root / archive.stem).resolve()
        if source.parent != root:
            raise ValueError(f"source escapes package root: {archive.stem}")
        missing_source = not source.is_dir()
        if missing_source and archive.stem not in ARCHIVE_ONLY:
            raise ValueError(f"missing source directory: {archive.stem}")
        replacements, differences, missing = {}, [], []
        before_hash = hashlib.sha256(archive.read_bytes()).hexdigest()
        with zipfile.ZipFile(archive) as old:
            infos = old.infolist()
            names = [i.filename for i in infos]
            if len(names) != len(set(names)):
                raise ValueError(f"duplicate entries: {archive.name}")
            for info in infos:
                name = pathlib.PurePosixPath(info.filename)
                if name.is_absolute() or ".." in name.parts or "\\" in info.filename or ":" in info.filename:
                    raise ValueError(f"unsafe entry: {info.filename}")
                candidate = (source / info.filename).resolve()
                if not candidate.is_relative_to(source):
                    raise ValueError(f"source entry escapes directory: {info.filename}")
                if info.is_dir():
                    continue
                data = old.read(info)
                updated = branded(data) if info.filename in TEXT_ENTRIES else data
                if info.filename == "manifest.json":
                    original_manifest, updated_manifest = json.loads(data), json.loads(updated)
                    # Only the human-facing author and embedded documentation may change.
                    for field in ("author", "documentation"):
                        original_manifest.pop(field, None)
                        updated_manifest.pop(field, None)
                    if original_manifest != updated_manifest:
                        raise ValueError(f"branding would change protocol: {archive.name}")
                if candidate.is_file():
                    source_bytes = candidate.read_bytes()
                    if source_bytes != updated:
                        differences.append(info.filename)
                    if updated != data and (branded(source_bytes) != source_bytes or "源铭振跃".encode() not in source_bytes):
                        raise ValueError(f"source branding is not updated: {archive.stem}/{info.filename}")
                else:
                    missing.append(info.filename)
                    if updated != data and not missing_source:
                        raise ValueError(f"missing branded source entry: {archive.stem}/{info.filename}")
                if updated != data:
                    replacements[info.filename] = updated
            if old.testzip() is not None:
                raise ValueError(f"invalid original archive: {archive.name}")
            if write and replacements:
                with tempfile.NamedTemporaryFile(dir=root, prefix=f".{archive.stem}-", suffix=".tmp", delete=False) as handle:
                    temporary = pathlib.Path(handle.name)
                try:
                    with zipfile.ZipFile(temporary, "w") as new:
                        new.comment = old.comment
                        for info in infos:
                            entry = copy.copy(info)
                            new.writestr(entry, replacements.get(info.filename, old.read(info)))
                            # zipfile fills a zero external_attr with default permissions.
                            # Restore the original before it writes the central directory.
                            entry.external_attr = info.external_attr
                    with zipfile.ZipFile(temporary) as check:
                        if check.testzip() is not None or check.namelist() != names or check.comment != old.comment:
                            raise ValueError(f"invalid temporary archive: {archive.name}")
                        for original, actual in zip(infos, check.infolist()):
                            if any(getattr(original, k) != getattr(actual, k) for k in METADATA):
                                raise ValueError(f"changed entry metadata: {actual.filename}")
                            if check.read(actual) != replacements.get(original.filename, old.read(original)):
                                raise ValueError(f"changed entry bytes: {actual.filename}")
                    # Close the original before replacement (required on Windows).
                    old.close()
                    os.chmod(temporary, archive.stat().st_mode)
                    os.replace(temporary, archive)
                finally:
                    temporary.unlink(missing_ok=True)
        results.append({"package": archive.name, "source": None if missing_source else source.relative_to(PROJECT).as_posix(),
                        "source_status": "archive-only baseline; source unavailable" if missing_source else "mapped",
                        "changed_entries": list(replacements), "source_differences": differences,
                        "missing_source_entries": missing, "entries": len(infos),
                        "sha256_before": before_hash, "sha256_after": hashlib.sha256(archive.read_bytes()).hexdigest()})
    return results


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=pathlib.Path, default=PROJECT / "plugin-packages", help="package directory inside this project")
    parser.add_argument("--write", action="store_true", help="validate and atomically replace affected archives")
    args = parser.parse_args()
    results = run(args.root, args.write)
    print(json.dumps({"mode": "write" if args.write else "check", "total": len(results),
                      "affected": sum(bool(r["changed_entries"]) for r in results), "packages": results}, ensure_ascii=True, indent=2))
