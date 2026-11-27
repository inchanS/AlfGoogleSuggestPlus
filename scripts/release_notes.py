"""Generate Korean Markdown release notes for a tagged commit."""
import os
import re
import subprocess
import sys
from functools import lru_cache

SECTIONS = {
    "feat": "새 기능", "fix": "버그 수정", "docs": "문서", "style": "코드 스타일",
    "refactor": "코드 리팩터링", "perf": "성능 개선", "test": "테스트", "build": "빌드",
    "ci": "지속적 통합", "chore": "유지보수", "revert": "변경 되돌리기",
}
HEADER = re.compile(r"([\w-]+)(?:\(([^)]+)\))?(!)?:\s+(.+)")
BREAKING = re.compile(r"^BREAKING[ -]CHANGES?:\s*(.*)")
HANGUL = re.compile(r"[가-힣ㄱ-ㅎㅏ-ㅣᄀ-ᇿ]")


def git(*args):
    return subprocess.check_output(["git", *args], encoding="utf-8")


def version(tag):
    if re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        return tuple(map(int, tag[1:].split(".")))
    return ()


class TranslationRequired(Exception):
    """The release runner needs the English-to-Korean model."""


@lru_cache(maxsize=None)
def korean(text):
    if HANGUL.search(text) or not re.search(r"[a-zA-Z]", text):
        return text
    try:
        from argostranslate import translate
    except ModuleNotFoundError as exc:
        raise TranslationRequired from exc
    return translate.translate(text, "en", "ko")


def related_prs(commit, repo_url):
    if not os.environ.get("GH_TOKEN"):
        return []
    repo = repo_url.rstrip("/").rsplit("/", 2)[-2:]
    numbers = subprocess.check_output([
        "gh", "api", "--paginate", f"repos/{'/'.join(repo)}/commits/{commit}/pulls",
        "--jq", ".[].number",
    ], text=True).splitlines()
    return [f"[PR #{n}]({repo_url}/pull/{n})" for n in numbers]


def commit_entry(commit, author, message, repo_url):
    lines = message.strip().splitlines()
    title, *body = lines or ["제목 없는 커밋"]
    header = HEADER.fullmatch(title)
    kind, scope, bang, subject = header.groups() if header else ("", "", "", title)
    details = []
    changes = []
    for line in body:
        line = line.strip()
        marker = BREAKING.match(line)
        if marker:
            changes.append(marker[1])
        elif line.startswith(("- ", "* ")):
            details.append(line[2:].strip())
        elif line and not re.match(r"^[A-Za-z][A-Za-z-]*-by:\s", line, re.I):
            details.append(line)

    # Prefer the author's Korean summary to a translation of the English subject.
    if not HANGUL.search(subject):
        for i, detail in enumerate(details):
            if HANGUL.search(detail):
                subject = details.pop(i)
                break
    label = f"**{scope}** · " if scope else ""
    links = [f"[커밋 {commit[:7]}]({repo_url}/commit/{commit})", *related_prs(commit, repo_url)]
    entry = f"- {label}{korean(subject)} (작성자 {author} · {' · '.join(links)})"
    breaking_entry = entry if bang or changes else ""
    for change in filter(None, changes):
        breaking_entry += f"\n  - {korean(change)}"
    for detail in filter(None, details):
        entry += f"\n  - {korean(detail)}"
    return SECTIONS.get(kind, "기타 변경"), entry, breaking_entry


def release_notes(tag, repo_url):
    current = version(tag)
    if not current:
        raise ValueError(f"잘못된 릴리스 태그: {tag}")
    tags = git("tag", "--merged", tag).splitlines()
    previous = max((t for t in tags if () < version(t) < current),
                   key=version, default=None)
    revisions = f"{previous}..{tag}" if previous else tag
    records = git("log", "--reverse", "--no-merges", "--format=%H%x00%an%x00%B", "-z", revisions)
    fields = records.split("\0")
    sections = {name: [] for name in ("호환성 변경", *SECTIONS.values(), "기타 변경")}
    for commit, author, message in zip(fields[::3], fields[1::3], fields[2::3]):
        section, entry, breaking = commit_entry(commit, author, message, repo_url)
        sections[section].append(entry)
        if breaking:
            sections["호환성 변경"].append(breaking)
    for section, entries in sections.items():
        if entries:
            print(f"## {section}\n")
            print("\n".join(entries), end="\n\n")
    if not records:
        print("새 변경 커밋이 없습니다.")
    if previous:
        print(f"[전체 변경 내역]({repo_url}/compare/{previous}...{tag})")


if __name__ == "__main__":
    try:
        release_notes(sys.argv[1], sys.argv[2])
    except TranslationRequired:
        sys.exit(42)
