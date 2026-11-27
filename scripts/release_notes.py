"""Print Markdown release notes for a tagged commit."""
import re
import subprocess
import sys


def git(*args):
    return subprocess.check_output(["git", *args], encoding="utf-8")


def version(tag):
    if re.fullmatch(r"v[0-9]+\.[0-9]+\.[0-9]+", tag):
        return tuple(map(int, tag[1:].split(".")))
    return ()


def release_notes(tag, repo_url):
    current = version(tag)
    if not current:
        raise ValueError(f"Invalid release tag: {tag}")
    tags = git("tag", "--merged", tag).splitlines()
    previous = max((t for t in tags if () < version(t) < current),
                   key=version, default=None)
    revisions = f"{previous}..{tag}" if previous else tag
    records = git("log", "--reverse", "--no-merges", "--format=%H%x00%B", "-z", revisions)
    fields = records.split("\0")
    print("## 주요 변경 사항\n")
    for commit, message in zip(fields[::2], fields[1::2]):
        lines = message.strip().splitlines()
        title, *body = lines or ["제목 없는 커밋"]
        print(f"- {title} ([{commit[:7]}]({repo_url}/commit/{commit}))")
        for line in body:
            if line.lstrip().startswith(("- ", "* ")):
                print(f"  - {line.lstrip()[2:]}")
    if not records:
        print("새 변경 커밋이 없습니다.")
    if previous:
        print(f"\n[전체 변경 내역]({repo_url}/compare/{previous}...{tag})")


if __name__ == "__main__":
    release_notes(sys.argv[1], sys.argv[2])
