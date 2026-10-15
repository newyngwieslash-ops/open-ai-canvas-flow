"""Run with: python scripts/test_repack_branded_plugins.py (stdlib only)."""

import importlib.util
import json
import pathlib
import tempfile
import unittest
import zipfile
from unittest.mock import patch


PROJECT = pathlib.Path(__file__).resolve().parents[1]
SCRIPT = PROJECT / "scripts/repack-branded-plugins.py"
tool = None
if SCRIPT.exists():
    spec = importlib.util.spec_from_file_location("repack", SCRIPT)
    tool = importlib.util.module_from_spec(spec)
    spec.loader.exec_module(tool)


class RepackTest(unittest.TestCase):
    def setUp(self):
        self.assertIsNotNone(tool, "brand-preserving repacker is not implemented")
        cache = PROJECT / ".local/cache"
        cache.mkdir(parents=True, exist_ok=True)
        self.directory = tempfile.TemporaryDirectory(dir=cache)
        self.addCleanup(self.directory.cleanup)
        self.root = pathlib.Path(self.directory.name)
        source = self.root / "sample"
        (source / "docs").mkdir(parents=True)
        self.manifest = {"id": "stable-id", "version": "1.0", "author": "Vendor / 影策",
                         "documentation": "影策 API https://example.test/Yingce 摄影策略",
                         "contributes": {"providers": [{"id": "stable-provider", "path": "/v1/jobs"}]}}
        old = {"manifest.json": (json.dumps(self.manifest, ensure_ascii=False, indent=2) + "\n").encode(),
               "README.md": "影策\n".encode(), "docs/interface.md": "Yingce 合同 v1\n".encode(),
               "backend/provider": b"\x7fELF\x00\xff\x10", "assets/unknown.dat": b"\xff\x00\x01"}
        self.archive = self.root / "sample.yingce-plugin"
        with zipfile.ZipFile(self.archive, "w") as z:
            z.comment = b"original archive comment"
            for name, data in old.items():
                info = zipfile.ZipInfo(name, (2024, 2, 3, 4, 5, 6))
                info.create_system = 3
                info.external_attr = (0o100755 if name.startswith("backend/") else 0o100644) << 16
                info.compress_type = zipfile.ZIP_DEFLATED
                info.comment = b"entry comment"
                z.writestr(info, data)
        for name in ("manifest.json", "README.md", "docs/interface.md"):
            (source / name).write_bytes(old[name].replace("影策".encode(), "源铭振跃".encode()).replace(b"Yingce ", "源铭振跃 ".encode()))
        self.before = self.archive.read_bytes()

    def test_check_is_read_only_and_lists_brand_entries(self):
        result = tool.run(self.root)
        self.assertEqual(self.archive.read_bytes(), self.before)
        self.assertEqual(len(result), 1, "every archive must be checked")
        self.assertEqual(result[0]["changed_entries"], ["manifest.json", "README.md", "docs/interface.md"])

    def test_write_preserves_paths_metadata_binary_and_contract_despite_source_drift(self):
        source_manifest = self.root / "sample/manifest.json"
        source_manifest.write_text(source_manifest.read_text(encoding="utf-8").replace('"1.0"', '"2.0"'), encoding="utf-8")
        result = tool.run(self.root, write=True)
        self.assertEqual(len(result), 1, "every archive must be checked")
        self.assertIn("manifest.json", result[0]["source_differences"])
        with zipfile.ZipFile(self.archive) as z, zipfile.ZipFile(__import__("io").BytesIO(self.before)) as old:
            self.assertEqual(z.namelist(), old.namelist())
            self.assertEqual(z.comment, old.comment)
            for info, original in zip(z.infolist(), old.infolist()):
                self.assertEqual((info.external_attr, info.date_time, info.comment, info.create_system, info.compress_type),
                                 (original.external_attr, original.date_time, original.comment, original.create_system, original.compress_type))
                if info.filename.startswith(("backend/", "assets/")):
                    self.assertEqual(z.read(info), old.read(original))
            manifest = json.loads(z.read("manifest.json"))
            self.assertEqual(manifest["author"], "Vendor / 源铭振跃")
            self.assertEqual(manifest["documentation"], "源铭振跃 API https://example.test/Yingce 摄影策略")
            self.assertEqual(manifest["version"], "1.0")
            self.assertEqual(manifest["contributes"], self.manifest["contributes"])
            self.assertEqual(z.read("README.md"), "源铭振跃\n".encode())
        updated = self.archive.read_bytes()
        self.assertEqual(tool.run(self.root, write=True)[0]["changed_entries"], [])
        self.assertEqual(self.archive.read_bytes(), updated)

    def test_invalid_temporary_archive_does_not_replace_original(self):
        # Inject the validator's corruption signal; observe real file preservation.
        validate = zipfile.ZipFile.testzip
        def corrupted_temporary(archive):
            return "README.md" if str(archive.filename).endswith(".tmp") else validate(archive)
        with patch.object(zipfile.ZipFile, "testzip", corrupted_temporary):
            with self.assertRaises(ValueError):
                tool.run(self.root, write=True)
        self.assertEqual(self.archive.read_bytes(), self.before)
        self.assertEqual(list(self.root.glob("*.tmp")), [])

    def test_zero_external_attributes_remain_zero(self):
        with zipfile.ZipFile(self.archive) as old:
            entries = [(i, old.read(i)) for i in old.infolist()]
        with zipfile.ZipFile(self.archive, "w") as z:
            for info, data in entries:
                z.writestr(info, data)
                info.external_attr = 0
        try:
            tool.run(self.root, write=True)
        except ValueError as error:
            self.fail(f"valid zero-attribute archive must be preserved: {error}")
        with zipfile.ZipFile(self.archive) as z:
            self.assertTrue(all(i.external_attr == 0 for i in z.infolist()))

    def test_rejects_root_outside_project(self):
        with tempfile.TemporaryDirectory() as outside:
            with self.assertRaises(ValueError):
                tool.run(pathlib.Path(outside), write=True)

    def test_rejects_traversal_entry_without_writing(self):
        with zipfile.ZipFile(self.archive, "a") as z:
            z.writestr("../outside.md", b"unsafe")
        before = self.archive.read_bytes()
        with self.assertRaises(ValueError):
            tool.run(self.root, write=True)
        self.assertEqual(self.archive.read_bytes(), before)

    def test_missing_source_is_not_silently_accepted(self):
        self.archive.rename(self.root / "missing.yingce-plugin")
        with self.assertRaises(ValueError):
            tool.run(self.root, write=True)

    def test_source_symlink_outside_root_is_rejected(self):
        path = self.root / "sample/README.md"
        with tempfile.TemporaryDirectory() as outside:
            target = pathlib.Path(outside) / "README.md"
            target.write_text("source", encoding="utf-8")
            path.unlink()
            try:
                path.symlink_to(target)
            except OSError as error:
                self.skipTest(f"symlink unavailable: {error}")
            with self.assertRaises(ValueError):
                tool.run(self.root, write=True)
        self.assertEqual(self.archive.read_bytes(), self.before)


if __name__ == "__main__":
    unittest.main(verbosity=2)
