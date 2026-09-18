#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""把使用说明的 Markdown 转成带打印样式的 HTML（供 Edge/Chrome 无头模式出 PDF）。
只实现本项目用到的语法：标题、表格、围栏代码块、列表、引用、粗体、行内代码。"""

import html
import re
import sys
from urllib.parse import quote

CSS = r"""
@page { size: A4; margin: 15mm 14mm 14mm 14mm; }
* { box-sizing: border-box; }
body {
  font-family: "Microsoft YaHei", "微软雅黑", "Segoe UI", sans-serif;
  font-size: 10.5pt;
  line-height: 1.62;
  color: #1b1b1f;
  margin: 0;
}
h1 {
  font-size: 19pt; margin: 0 0 6pt; padding-bottom: 6pt;
  border-bottom: 2.5px solid #1e60b4; color: #10325f;
  letter-spacing: .3px;
}
h2 {
  font-size: 13.5pt; margin: 17pt 0 7pt; padding-left: 8pt;
  border-left: 4px solid #2e7de0; color: #10325f;
  page-break-after: avoid;
}
h3 {
  font-size: 11.5pt; margin: 12pt 0 5pt; color: #23558f;
  page-break-after: avoid;
}
p { margin: 5pt 0; }
a { color: #1e60b4; }
strong { color: #10325f; }
hr { border: 0; border-top: 1px solid #d6dae0; margin: 13pt 0; }
ul, ol { margin: 5pt 0; padding-left: 20pt; }
li { margin: 2.5pt 0; }
li > ul, li > ol { margin: 2pt 0; }

code {
  font-family: Consolas, "Courier New", "Microsoft YaHei", monospace;
  font-size: .92em;
  background: #f0f3f7; border: 1px solid #e0e5ec;
  border-radius: 3px; padding: .5pt 3pt;
  color: #b3341f;
}
pre {
  font-family: Consolas, "MS Gothic", "Microsoft YaHei", monospace;
  font-size: 8.6pt; line-height: 1.42;
  background: #f7f9fc; border: 1px solid #dde3ea; border-left: 3px solid #2e7de0;
  border-radius: 3px; padding: 7pt 9pt; margin: 7pt 0;
  white-space: pre; overflow-x: hidden;
  page-break-inside: avoid;
}
pre code { background: none; border: 0; padding: 0; color: #1b1b1f; }

table {
  border-collapse: collapse; width: 100%; margin: 8pt 0;
  font-size: 9.6pt;
}
th, td {
  border: 1px solid #cfd6de; padding: 4pt 7pt; text-align: left;
  vertical-align: top;
}
th { background: #e8eff8; color: #10325f; font-weight: 600; }
tr:nth-child(even) td { background: #fafbfd; }
tr { page-break-inside: avoid; }

blockquote {
  margin: 7pt 0; padding: 6pt 10pt;
  background: #fdf7e6; border-left: 3px solid #e0a93a;
  color: #4a3c1c; font-size: 9.8pt;
}
blockquote p { margin: 3pt 0; }

img {
  max-width: 100%; display: block; margin: 8pt 0;
  border: 1px solid #cfd6de; border-radius: 4px;
  page-break-inside: avoid;
}

.doc-meta {
  font-size: 9pt; color: #6b7280; margin: 0 0 12pt;
}
"""


def inline(text: str) -> str:
    """行内标记：先转义，再处理 code / bold。"""
    out = html.escape(text, quote=False)

    # 图片：路径做 URL 编码，中文文件名也能被 file:// 加载
    out = re.sub(
        r"!\[([^\]]*)\]\(([^)]+)\)",
        lambda m: '<img src="%s" alt="%s">'
        % (quote(m.group(2)), html.escape(m.group(1), quote=True)),
        out,
    )

    codes = []

    def stash(m):
        codes.append(m.group(1))
        return "\x00%d\x00" % (len(codes) - 1)

    out = re.sub(r"`([^`]+)`", stash, out)
    out = re.sub(r"\*\*([^*]+)\*\*", r"<strong>\1</strong>", out)
    out = re.sub(r"(?<!\*)\*([^*\n]+)\*(?!\*)", r"<em>\1</em>", out)
    for i, c in enumerate(codes):
        out = out.replace("\x00%d\x00" % i, "<code>%s</code>" % html.escape(c, quote=False))
    return out


def split_row(line: str):
    line = line.strip()
    if line.startswith("|"):
        line = line[1:]
    if line.endswith("|"):
        line = line[:-1]
    return [c.strip() for c in line.split("|")]


def is_sep(line: str) -> bool:
    cells = split_row(line)
    return bool(cells) and all(re.fullmatch(r":?-{2,}:?", c) for c in cells if c != "")


def convert(md: str) -> str:
    lines = md.replace("\r\n", "\n").split("\n")
    out, i, n = [], 0, len(lines)

    while i < n:
        line = lines[i]
        stripped = line.strip()

        # 围栏代码块
        if stripped.startswith("```"):
            i += 1
            buf = []
            while i < n and not lines[i].strip().startswith("```"):
                buf.append(lines[i])
                i += 1
            i += 1
            out.append("<pre><code>%s</code></pre>" % html.escape("\n".join(buf), quote=False))
            continue

        # 表格
        if stripped.startswith("|") and i + 1 < n and is_sep(lines[i + 1]):
            head = split_row(lines[i])
            i += 2
            body = []
            while i < n and lines[i].strip().startswith("|"):
                body.append(split_row(lines[i]))
                i += 1
            rows = ["<table>", "<thead><tr>"]
            rows += ["<th>%s</th>" % inline(c) for c in head]
            rows.append("</tr></thead><tbody>")
            for r in body:
                rows.append("<tr>" + "".join("<td>%s</td>" % inline(c) for c in r) + "</tr>")
            rows.append("</tbody></table>")
            out.append("".join(rows))
            continue

        # 标题
        m = re.match(r"^(#{1,6})\s+(.*)$", stripped)
        if m:
            lvl = len(m.group(1))
            out.append("<h%d>%s</h%d>" % (lvl, inline(m.group(2)), lvl))
            i += 1
            continue

        # 分隔线
        if re.fullmatch(r"-{3,}|\*{3,}", stripped):
            out.append("<hr>")
            i += 1
            continue

        # 引用
        if stripped.startswith(">"):
            buf = []
            while i < n and lines[i].strip().startswith(">"):
                buf.append(lines[i].strip()[1:].strip())
                i += 1
            out.append("<blockquote>%s</blockquote>" % "".join("<p>%s</p>" % inline(b) for b in buf if b))
            continue

        # 列表
        if re.match(r"^\s*([-*+]|\d+\.)\s+", line):
            ordered = bool(re.match(r"^\s*\d+\.\s+", line))
            tag = "ol" if ordered else "ul"
            items = []
            while i < n and re.match(r"^\s*([-*+]|\d+\.)\s+", lines[i]):
                content = re.sub(r"^\s*([-*+]|\d+\.)\s+", "", lines[i])
                items.append("<li>%s</li>" % inline(content))
                i += 1
            out.append("<%s>%s</%s>" % (tag, "".join(items), tag))
            continue

        # 空行
        if stripped == "":
            i += 1
            continue

        # 段落（连续非空行合并）
        buf = [stripped]
        i += 1
        while i < n and lines[i].strip() != "" and not re.match(
            r"^\s*([-*+]|\d+\.)\s+", lines[i]
        ) and not lines[i].strip().startswith(("|", ">", "#", "```")):
            buf.append(lines[i].strip())
            i += 1
        out.append("<p>%s</p>" % inline(" ".join(buf)))

    return "\n".join(out)


def main():
    src, dst = sys.argv[1], sys.argv[2]
    with open(src, "r", encoding="utf-8") as f:
        md = f.read()
    body = convert(md)
    page = (
        '<!DOCTYPE html>\n<html lang="zh-CN">\n<head>\n<meta charset="utf-8">\n'
        '<title>DB2KepServerEX 使用说明</title>\n<style>%s</style>\n</head>\n'
        "<body>\n%s\n</body>\n</html>\n" % (CSS, body)
    )
    with open(dst, "w", encoding="utf-8") as f:
        f.write(page)
    print("已生成 %s（%d 字节）" % (dst, len(page.encode("utf-8"))))


if __name__ == "__main__":
    main()
