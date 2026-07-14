import unittest
from pathlib import Path

from interview_bank_import import chunk_text, collect_files, load_json_atoms, normalize_import_package


class InterviewBankImportScriptTests(unittest.TestCase):
    def test_normalizes_single_atom_payload(self) -> None:
        package = normalize_import_package(
            [
                {
                    "subject": "MySQL 索引下推",
                    "title": "MySQL 索引下推",
                    "category": "database",
                    "difficulty": "l3",
                    "question_role": "opening",
                    "source_ref": "notes/mysql.json",
                    "content": {
                        "principles": ["解释机制", "命中条件"],
                        "pitfalls": "把 ICP 和回表混为一谈\n忽略联合索引顺序",
                        "follow_up_paths": ["为什么能减少回表", "失效场景有哪些"],
                    },
                }
            ],
            {
                "source_ref": "defaults.json",
                "domain": "database",
                "category": "database",
                "difficulty": "L3",
                "question_role": "opening",
                "status": "published",
            },
            batch_id="qb-database-test",
        )
        self.assertEqual(package["batchId"], "qb-database-test")
        self.assertEqual(len(package["items"]), 1)
        item = package["items"][0]
        self.assertEqual(item["domain"], "database")
        self.assertEqual(item["difficulty"], "L3")
        self.assertEqual(item["questionRole"], "opening")
        self.assertEqual(item["status"], "published")
        self.assertEqual(item["pitfalls"], ["把 ICP 和回表混为一谈", "忽略联合索引顺序"])
        self.assertEqual(package["validationReport"]["errors"], [])

    def test_reports_enum_and_required_errors(self) -> None:
        package = normalize_import_package(
            [
                {
                    "id": "broken-1",
                    "title": "",
                    "subject": "无效数据",
                    "domain": "unknown",
                    "category": "unknown",
                    "difficulty": "mid",
                    "questionRole": "oops",
                    "sourceRef": "",
                    "principles": ["只有一条"],
                    "pitfalls": ["只有一条"],
                    "followUpPaths": ["只有一条"],
                    "status": "active",
                }
            ],
            {
                "source_ref": "",
                "domain": "",
                "category": "",
                "difficulty": "L3",
                "question_role": "opening",
                "status": "published",
            },
        )
        errors = "\n".join(package["validationReport"]["errors"])
        self.assertIn("sourceRef is required", errors)
        self.assertIn("category must be one of", errors)
        self.assertIn("questionRole must be one of", errors)
        self.assertIn("status must be one of", errors)
        self.assertEqual(package["items"][0]["title"], "无效数据")

    def test_accepts_items_or_atoms_package_shape(self) -> None:
        atoms = load_json_atoms(self._write_temp_json({"items": [{"id": "a1"}]}))
        self.assertEqual(len(atoms), 1)
        self.assertEqual(atoms[0]["id"], "a1")
        atoms = load_json_atoms(self._write_temp_json({"atoms": [{"id": "a2"}]}))
        self.assertEqual(len(atoms), 1)
        self.assertEqual(atoms[0]["id"], "a2")

    def test_collect_files_supports_directory_scan(self) -> None:
        import tempfile

        with tempfile.TemporaryDirectory() as tmpdir:
            root = Path(tmpdir)
            (root / "notes.txt").write_text("hello", encoding="utf-8")
            (root / "nested").mkdir()
            (root / "nested" / "outline.md").write_text("# title", encoding="utf-8")
            (root / "payload.json").write_text("{}", encoding="utf-8")
            (root / "ignored.csv").write_text("a,b", encoding="utf-8")
            files = collect_files([root])
            names = sorted(path.name for path in files)
            self.assertEqual(names, ["notes.txt", "outline.md", "payload.json"])

    def test_chunk_text_preserves_overlap(self) -> None:
        chunks = chunk_text("abcdefghij", chunk_size=4, overlap=1)
        self.assertEqual(chunks, ["abcd", "defg", "ghij", "j"])

    def _write_temp_json(self, payload):
        import json
        import tempfile

        handle = tempfile.NamedTemporaryFile("w", suffix=".json", delete=False, encoding="utf-8")
        handle.write(json.dumps(payload, ensure_ascii=False))
        handle.close()
        self.addCleanup(lambda: Path(handle.name).unlink(missing_ok=True))
        return Path(handle.name)


if __name__ == "__main__":
    unittest.main()
