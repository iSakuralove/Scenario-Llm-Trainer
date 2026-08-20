#!/usr/bin/env python3
"""thin_interview_bank_package 最小自检。"""

from thin_interview_bank_package import knowledge_key, pick_one, thin_items


def test_thin_prefers_opening():
    items = [
        {"id": "java-jvm-2-aaaaaa", "questionRole": "mixed", "subject": "b"},
        {"id": "java-jvm-1-bbbbbb", "questionRole": "opening", "subject": "a"},
        {"id": "java-jvm-3-cccccc", "questionRole": "followup", "subject": "c"},
        {"id": "java-gc-1-dddddd", "questionRole": "opening", "subject": "d"},
        {"id": "java-gc-2-eeeeee", "questionRole": "mixed", "subject": "e"},
    ]
    selected, stats = thin_items(items)
    assert stats["group_count"] == 2
    assert stats["selected_count"] == 2
    roles = {item["id"]: item["questionRole"] for item in selected}
    assert roles["java-jvm-1-bbbbbb"] == "opening"
    assert roles["java-gc-1-dddddd"] == "opening"
    assert knowledge_key(items[0]) == "java-jvm"
    assert pick_one(items[:3])["questionRole"] == "opening"
    print("ok")


if __name__ == "__main__":
    test_thin_prefers_opening()
