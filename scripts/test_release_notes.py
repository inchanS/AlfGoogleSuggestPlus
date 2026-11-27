import unittest
from unittest.mock import patch

import release_notes as notes


class ReleaseNoteTest(unittest.TestCase):
    def entry(self, message):
        with patch.object(notes, "related_prs", return_value=["[PR #3](repo/pull/3)"]):
            return notes.commit_entry("abcdef012345", "개발자", message, "repo")

    def test_categories_and_details(self):
        self.assertEqual(len(notes.SECTIONS), 11)
        for kind, section in notes.SECTIONS.items():
            with self.subTest(kind=kind):
                group, entry, breaking = self.entry(
                    f"{kind}(search): change search\n\n- 검색 동작 변경\n- 한글 입력 지원"
                )
                self.assertEqual(group, section)
                self.assertIn("**search** · 검색 동작 변경", entry)
                self.assertIn("  - 한글 입력 지원", entry)
                self.assertIn("작성자 개발자", entry)
                self.assertIn("PR #3", entry)
                self.assertFalse(breaking)

    def test_breaking_and_translation(self):
        messages = {"change settings": "설정 변경", "Rename key": "키 이름 변경"}
        with patch.dict("sys.modules", {"argostranslate": unittest.mock.Mock()}) as modules:
            translate = modules["argostranslate"].translate.translate
            translate.side_effect = lambda text, source, target: messages[text]
            group, entry, breaking = self.entry(
                "feat(config)!: change settings\n\nBREAKING CHANGE: Rename key"
            )
        self.assertEqual(group, "새 기능")
        self.assertIn("설정 변경", entry)
        self.assertIn("키 이름 변경", breaking)
        self.assertEqual(translate.call_count, 2)
        notes.korean.cache_clear()

    def test_unknown_type(self):
        self.assertEqual(self.entry("검색 동작 변경")[0], "기타 변경")
