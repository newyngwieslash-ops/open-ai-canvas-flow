#!/usr/bin/env python3
"""Read-only product brand audit; standard-library-only executable self-check."""

import argparse
import json
import os
import pathlib
import re
import subprocess
import sys
import tempfile
import zipfile


# Exact file + complete stripped line: adjacent display copy is never exempt.
EXCEPTIONS = {
    ("web/src/lib/brand-display.ts", 'export const SOURCE_UI_BRAND = "影策";'): "legacy default recognition; not display copy",
    ("web/test/brand-display.test.ts", 'expect(displayBrandName("影策")).toBe(UI_BRAND_NAME);'): "legacy value regression input",
    ("web/test/brand-display.test.ts", 'expect(SOURCE_UI_BRAND).toBe("影策");'): "legacy recognition regression assertion",
    ("web/test/brand-display.test.ts", 'expect(displayBrandName(" 影策 ")).toBe(UI_BRAND_NAME);'): "legacy whitespace regression input",
    ("web/test/brand-display.test.ts", 'expect(displayBrandName("影策工作室")).toBe("影策工作室");'): "custom brand preservation regression input",
    ("web/test/brand-display.test.ts", 'expect(displayBrandTitle("", "影策")).toBe(UI_BRAND_NAME);'): "legacy fallback regression input",
    ("web/test/brand-display.test.ts", 'expect(displayBrandTitle("影策 - 定制长标题", "影策")).toBe("影策 - 定制长标题");'): "custom title preservation regression input",
    ("web/test/brand-display.test.ts", 'expect(displayBrandTitle("HIMA Studio", "影策")).toBe("HIMA Studio");'): "custom title preservation regression input",
    ("web/test/brand-display.test.ts", 'const appearance = { brandName: "影策", seoTitle: "影策", footerCopyright: "© 2026 影策. All rights reserved." };'): "legacy configuration non-mutation regression input",
    ("docs/openspec/changes/rename-product-brand-to-yuanming-zhenyue/brand-inventory.md", "基线旧品牌检索词：`影策`、`YINGCE STUDIO`、`Open AI Canvas`。"): "historical audit evidence",
}
BRAND = re.compile(r"影策|yingce|open[ _-]ai[ _-]canvas", re.IGNORECASE)
TECHNICAL = re.compile(
    r"https?://[^\s<>\"'`]+|\.yingce-plugin(?![\w.-])"
    r"|(?<![\w.-])(?:yingce/backend(?:/[\w/.-]+)?|yingce-agent|YINGCE_[A-Z0-9_]+)(?![\w.-])"
    r"|(?<![\w.-])(?:yingce\.plugin/(?:v1|v2|package-v1)|yingce-system|yingce-short-drama-workflow)(?![\w./-])"
    r"|\b(?:brandSlug|BrandSlug|brand_slug|defaultAppearanceBrandSlug)[\"']?\s*[:=/]\s*[\"']?open-ai-canvas(?![\w.-])"
)
EXCLUDED_PARTS = {
    ".git", ".local", ".taste-skill", ".superpowers", ".agents", ".codex",
    ".claude", ".comet", "node_modules", "vendor", "dist", "build", ".next",
    "out", "coverage", "__pycache__", ".cache", ".worktrees", "worktrees",
    "open-storyboard-canvas",
}
USER_REFERENCES = {"ChatGPT 图像 2026年10月15日 21_12_36.png", "源铭振跃_UI品牌替换实现方案.txt"}


def audit_root(root: pathlib.Path) -> list[dict]:
    root = root.resolve()
    listed = subprocess.run(
        ["git", "-C", str(root), "ls-files", "--cached", "--others", "--exclude-standard", "-z"],
        check=True, stdout=subprocess.PIPE,
    )
    paths = {os.fsdecode(name) for name in listed.stdout.split(b"\0") if name}

    def excluded(path):
        relative = pathlib.PurePosixPath(path)
        if relative.is_absolute() or ".." in relative.parts:
            return True
        if set(relative.parts) & EXCLUDED_PARTS or path in USER_REFERENCES:
            return True
        # Never follow symlinks or another checkout, even if tracked by accident.
        return any((root / parent).is_symlink() or (root / parent / ".git").exists()
                   for parent in (relative, *tuple(relative.parents)[:-1]))

    findings = []

    def record(path, entry, line, text, category, reason):
        findings.append(dict(path=path, entry=entry, line=line, text=text, category=category, reason=reason))

    def walk_error(error):
        record(pathlib.Path(error.filename).relative_to(root).as_posix(), None, 0,
               str(error), "product", "audit error; coverage incomplete")

    # This repository ignores much of docs/. Current maintenance docs still count.
    # Include all on-disk docs; historical exceptions remain exact line matches.
    if (root / "docs").is_dir() and not excluded("docs"):
        for directory, directories, files in os.walk(root / "docs", followlinks=False, onerror=walk_error):
            directories[:] = sorted(name for name in directories
                                    if not excluded((pathlib.Path(directory) / name).relative_to(root).as_posix()))
            paths.update((pathlib.Path(directory) / name).relative_to(root).as_posix() for name in files)
    def scan_text(path, entry, number, text, decoded=None):
        searched = text if decoded is None else decoded
        emitted = set()
        for match in BRAND.finditer(searched):
            if any(m.start() <= match.start() and match.end() <= m.end() for m in re.finditer("摄影策略", searched)):
                category, reason = "non_brand", "ordinary photography language"
            elif number and entry is None and (path, text.strip()) in EXCEPTIONS:
                category, reason = "exception", EXCEPTIONS[path, text.strip()]
            elif any(m.start() <= match.start() and match.end() <= m.end() for m in TECHNICAL.finditer(searched)):
                category, reason = "exception", "stable technical token or actual upstream URL; retain identity"
            else:
                category, reason = "product", "old brand in " + ("name" if number == 0 else "text; review product visibility")
            if (category, reason) not in emitted:
                record(path, entry, number, text, category, reason)
                emitted.add((category, reason))

    def scan_bytes(path, entry, data):
        try:
            if b"\0" in data:
                raise UnicodeError("NUL byte")
            text = data.decode("utf-8-sig")
        except UnicodeError:
            record(path, entry, 0, "", "exception", "non-text; visual/binary review required")
            return False
        def decode_string(match):
            try:
                return json.dumps(json.loads(match.group()), ensure_ascii=False)
            except ValueError:
                return match.group()

        for number, line in enumerate(text.splitlines(), 1):
            # Decode JSON strings but retain quotes so URL exceptions stop at field boundaries.
            # Double-escaped backslashes remain literal, unlike unicode_escape.
            decoded = re.sub(r'"(?:[^"\\]|\\.)*"', decode_string, line) if "\\u" in line else line
            scan_text(path, entry, number, line, decoded)
        return True

    for path in sorted(paths):
        if excluded(path):
            continue
        source = root / path
        scan_text(path, None, 0, path)
        try:
            if source.suffix.lower() in (".yingce-plugin", ".zip"):
                with zipfile.ZipFile(source) as package:
                    text_count = binary_count = 0
                    for info in package.infolist():
                        if info.is_dir():
                            continue
                        scan_text(path, info.filename, 0, info.filename)
                        if scan_bytes(path, info.filename, package.read(info)):
                            text_count += 1
                        else:
                            binary_count += 1
                    record(path, None, 0, "", "exception",
                           f"archive inspected: {text_count} text entries, {binary_count} non-text entries; no pixel inspection")
            else:
                scan_bytes(path, None, source.read_bytes())
        except (OSError, zipfile.BadZipFile, RuntimeError, NotImplementedError, EOFError) as error:
            record(path, None, 0, str(error), "product", "audit error; coverage incomplete")
    return findings


def self_test():
    # A dropped input, broad exception or skipped archive must fail this check.
    with tempfile.TemporaryDirectory() as directory:
        root = pathlib.Path(directory)
        subprocess.run(["git", "init", "-q", str(root)], check=True)
        fixtures = {
            "copy.txt": "影策\nYINGCE STUDIO\nOpen AI Canvas\n",
            "影策.txt": "",
            "ordinary.txt": "摄影策略\n摄影策略与影策团队\n",
            "web/src/lib/brand-display.ts": 'export const SOURCE_UI_BRAND = "影策";\nconst title = "影策团队";\n',
            ".gitignore": "docs/*\n",
            "docs/content/docs/guide.mdx": "影策使用指南\n",
            "docs/current.md": "影策当前维护文档\n",
            "docs/openspec/unrelated.md": "影策产品输出\n",
            "docs/openspec/changes/rename-product-brand-to-yuanming-zhenyue/brand-inventory.md": "基线旧品牌检索词：`影策`、`YINGCE STUDIO`、`Open AI Canvas`。\n影策新产品介绍\n",
            "mixed.txt": "https://github.com/ddcat-ai/open-ai-canvas 影策团队\n",
            "manifest.json": '\n' + json.dumps({"author": "影策团队"}) + '\n',
            "compact.json": json.dumps({"url": "https://example.org", "author": "影策团队"}, separators=(",", ":")),
            "literal.json": json.dumps({"example": r"\u5f71\u7b56"}),
            "technical.txt": '\n'.join((
                'yingce-agent', 'sample.yingce-plugin', '.yingce-plugin',
                'yingce/backend', 'yingce/backend/bin', 'YINGCE_AGENT_URL',
                'prefixyingce-agent', 'yingce-agent-display', 'yingce-agentDisplay',
                '.yingce-plugin-display', '.yingce-pluginExtra',
                'prefixyingce/backend', 'yingce/backend-display',
                'prefixYINGCE_AGENT_URL', 'YINGCE_AGENT_URL-display', 'YINGCE_AGENT_URLdisplay',
            )),
            "stable.txt": '\n'.join((
                'yingce.plugin/v1', 'yingce.plugin/v2', 'yingce.plugin/package-v1',
                'yingce-system', 'yingce-short-drama-workflow',
                'brandSlug: "open-ai-canvas"', '"brandSlug": "open-ai-canvas"',
                'defaultAppearanceBrandSlug = "open-ai-canvas"',
                'yingce.plugin/v1 影策团队', 'brandSlug: "open-ai-canvas", label: "影策团队"',
                'yingce.plugin/v10', 'yingce-system-display',
            )),
            "backend/builtin/摄影策略/SKILL.md": "摄影策略\n",
            "binary.png": b"\x89PNG\x00\xff",
            ".local/cache/copy.txt": "影策",
            ".taste-skill/copy.txt": "影策",
            "node_modules/copy.txt": "影策",
            "web/dist/copy.txt": "影策",
            ".worktrees/other/copy.txt": "影策",
            "other-checkout/.git": "gitdir: /elsewhere\n",
            "other-checkout/copy.txt": "影策",
            "ChatGPT 图像 2026年10月15日 21_12_36.png": "影策",
            "源铭振跃_UI品牌替换实现方案.txt": "影策用户参考方案",
        }
        for name, content in fixtures.items():
            target = root / name
            target.parent.mkdir(parents=True, exist_ok=True)
            target.write_bytes(content if isinstance(content, bytes) else content.encode("utf-8"))
        # Tracked build/cache files must still be excluded.
        subprocess.run(["git", "-C", str(root), "add", "-f", ".local", "web/dist"], check=True)
        with zipfile.ZipFile(root / "sample.yingce-plugin", "w") as package:
            package.writestr("README.md", "影策团队\n")
            package.writestr("影策.json", '{"title": "影策"}')
            package.writestr("logo.png", b"\x89PNG\x00\xff")
            package.writestr("manifest.json", '\n' + json.dumps({"author": "影策团队"}) + '\n')
            package.writestr("compact.json", fixtures["compact.json"])
        with zipfile.ZipFile(root / "clean.yingce-plugin", "w") as package:
            package.writestr("README.md", "中性说明")
        findings = audit_root(root)
        assert any(x["path"] == "copy.txt" and x["line"] == 1 and x["category"] == "product" for x in findings), "source brand must be reported"
        assert any(x["path"] == "copy.txt" and x["line"] == 2 and x["category"] == "product" for x in findings), "English brand must be reported"
        assert any(x["path"] == "copy.txt" and x["line"] == 3 and x["category"] == "product" for x in findings), "English display title must be reported"
        assert any(x["path"] == "影策.txt" and x["line"] == 0 and x["category"] == "product" for x in findings), "filename must be reported"
        assert any(x["path"] == "sample.yingce-plugin" and x["entry"] == "README.md" and x["line"] == 1 and x["category"] == "product" for x in findings), "archive text must be reported"
        assert any(x["entry"] == "影策.json" and x["line"] == 0 and x["category"] == "product" for x in findings), "archive entry name must be reported"
        for path, entry in (("manifest.json", None), ("sample.yingce-plugin", "manifest.json")):
            assert any(x["path"] == path and x["entry"] == entry and x["line"] == 2 and x["category"] == "product" for x in findings), "escaped JSON brand must retain source location: " + path
        assert not any(x["path"] == "literal.json" and x["category"] == "product" for x in findings), "literal backslash-u must not be treated as a semantic brand"
        for path, entry in (("compact.json", None), ("sample.yingce-plugin", "compact.json")):
            assert any(x["path"] == path and x["entry"] == entry and x["line"] == 1 and x["category"] == "product" and x["text"] == fixtures["compact.json"] for x in findings), "compact JSON URL must not hide escaped author: " + path
        for line in range(1, 9):
            assert {x["category"] for x in findings if x["path"] == "stable.txt" and x["line"] == line} == {"exception"}, "stable identity must be a technical exception: line " + str(line)
        for line in (9, 10):
            assert {x["category"] for x in findings if x["path"] == "stable.txt" and x["line"] == line} == {"exception", "product"}, "stable identity must not hide adjacent display text"
        for line in (11, 12):
            assert any(x["path"] == "stable.txt" and x["line"] == line and x["category"] == "product" for x in findings), "technical identity exception must match the exact token"
        for line in range(1, 7):
            assert {x["category"] for x in findings if x["path"] == "technical.txt" and x["line"] == line} == {"exception"}, "complete technical tokens must remain exceptions: line " + str(line)
        for line in range(7, 17):
            assert any(x["path"] == "technical.txt" and x["line"] == line and x["category"] == "product" for x in findings), "technical exception must not hide prefixed/suffixed display values: line " + str(line)
        assert any(x["path"] == "ordinary.txt" and x["line"] == 1 and x["category"] == "non_brand" for x in findings), "photography language must be classified separately"
        assert not any(x["path"] == "ordinary.txt" and x["line"] == 1 and x["category"] == "product" for x in findings)
        assert any(x["path"] == "ordinary.txt" and x["line"] == 2 and x["category"] == "product" for x in findings), "photography text must not hide a brand on the same line"
        assert not any(x["path"].startswith("backend/builtin/摄影策略/") and x["category"] == "product" for x in findings), "photography directory must not taint descendants"
        assert any(x["path"] == "web/src/lib/brand-display.ts" and x["line"] == 1 and x["category"] == "exception" for x in findings), "exact legacy recognition exception must apply"
        assert any(x["path"] == "web/src/lib/brand-display.ts" and x["line"] == 2 and x["category"] == "product" for x in findings), "exception must not exempt its whole file"
        for path in ("docs/content/docs/guide.mdx", "docs/current.md", "docs/openspec/unrelated.md"):
            assert any(x["path"] == path and x["category"] == "product" for x in findings), "ignored documentation must remain covered: " + path
        assert any(x["path"].endswith("brand-inventory.md") and x["line"] == 1 and x["category"] == "exception" for x in findings), "exact historical evidence must be classified separately"
        assert any(x["path"].endswith("brand-inventory.md") and x["line"] == 2 and x["category"] == "product" for x in findings), "historical exception must not hide current documentation"
        assert {x["category"] for x in findings if x["path"] == "mixed.txt" and x["line"] == 1} == {"exception", "product"}, "a real source URL must not hide adjacent display branding"
        for prefix in (".local/", ".taste-skill/", "node_modules/", "web/dist/", ".worktrees/", "other-checkout/", "ChatGPT 图像", "源铭振跃_UI品牌替换实现方案.txt"):
            assert not any(x["path"].startswith(prefix) for x in findings), "non-product input leaked: " + prefix
        assert any(x["path"] == "binary.png" and x["reason"] == "non-text; visual/binary review required" for x in findings)
        assert any(x["entry"] == "logo.png" and x["reason"] == "non-text; visual/binary review required" for x in findings)
        assert {x["path"] for x in findings if x["reason"].startswith("archive inspected:")} == {"sample.yingce-plugin", "clean.yingce-plugin"}, "all packages need coverage records"
        assert all(set(("path", "entry", "line", "text", "category", "reason")) <= x.keys() for x in findings)
        assert audit_root(root) == findings, "audit must be repeatable"
        command = [sys.executable, str(pathlib.Path(__file__).resolve()), "--root", str(root), "--json"]
        completed = subprocess.run(command, stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        assert completed.returncode == 1 and json.loads(completed.stdout) == findings, "CLI must emit the same array and fail on product findings"
        (root / "broken.yingce-plugin").write_bytes(b"not a ZIP")
        assert any(x["path"] == "broken.yingce-plugin" and x["category"] == "product" and x["reason"] == "audit error; coverage incomplete" for x in audit_root(root)), "invalid archives must not silently pass"
    with tempfile.TemporaryDirectory() as directory:
        subprocess.run(["git", "init", "-q", directory], check=True)
        completed = subprocess.run([sys.executable, str(pathlib.Path(__file__).resolve()), "--root", directory, "--json"], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        assert completed.returncode == 0 and json.loads(completed.stdout) == [], "clean audit must pass"
        root = pathlib.Path(directory).resolve()
        (root / ".gitignore").write_text("docs/\n", encoding="utf-8")
        (root / "docs").mkdir()
        (root / "docs" / "hidden.md").write_text("影策", encoding="utf-8")
        # Inject an OS denial: portable even when the test account can bypass ACLs.
        completed = subprocess.run([sys.executable, "-c", "\n".join((
            "import pathlib, runpy, sys",
            "from unittest.mock import patch",
            "script, root = sys.argv[1:]",
            "sys.argv = [script, '--root', root, '--json']",
            "with patch('os.scandir', side_effect=PermissionError(13, 'fixture: docs enumeration denied', str(pathlib.Path(root) / 'docs'))):",
            "    runpy.run_path(script, run_name='__main__')",
        )), str(pathlib.Path(__file__).resolve()), str(root)], stdout=subprocess.PIPE, stderr=subprocess.PIPE)
        assert completed.returncode == 1 and any(x["path"] == "docs" and x["category"] == "product" and x["reason"] == "audit error; coverage incomplete" for x in json.loads(completed.stdout)), "ignored docs enumeration error must fail CLI with a coverage finding"
    print("self-test: PASS (source, names, ZIP, exact exceptions, docs, exclusions, non-text, repeatability)")


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description=__doc__)
    parser.add_argument("--root", type=pathlib.Path, default=pathlib.Path("."))
    parser.add_argument("--json", action="store_true")
    parser.add_argument("--self-test", action="store_true")
    args = parser.parse_args()
    if args.self_test:
        self_test()
    else:
        result = audit_root(args.root)
        print(json.dumps(result, ensure_ascii=True, indent=2))
        raise SystemExit(int(any(item["category"] == "product" for item in result)))
