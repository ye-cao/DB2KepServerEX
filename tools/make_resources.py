#!/usr/bin/env python3
# -*- coding: utf-8 -*-
"""
make_resources.py —— 生成 app.syso（Go 链接器会自动链进 exe 的资源目标文件）

里面放三样东西：
  1. RT_MANIFEST   —— 声明 Common-Controls v6 依赖 + DPI 感知
                      （没有它，窗口只能用 Windows 经典 Win95 风格控件外观）
  2. RT_ICON       —— 程序图标（多尺寸 PNG）
  3. RT_GROUP_ICON —— 图标组，把上面几个尺寸绑成一个图标

为什么需要它：
  Go 的 Windows 链接器不会自己写 manifest，也不支持直接挂 .rc / .ico，
  但它会读取包目录下 .rsrc 段的目标文件（.syso）。
  所以这里用纯 Python 手搓一个 x86-64 COFF 目标文件。

用法：
  python tools/make_resources.py           # 在包根目录（上一级）生成 app.syso
  go build -trimpath -ldflags "-s -w -H windowsgui" -o DB2KepServerEX.exe .

注意：生成的是 x86-64 目标文件（Machine = 0x8664）。
"""

import os
import struct
import sys
import zlib

# ============================================================ 图标绘制
#
# 不做字体的依赖，纯几何绘制：圆角矩形底 + 白色右箭头（表示"转换/导出"）。
# 先在 1024x1024 上画，再逐级降采样，这样边缘自带抗锯齿。

ICON_SIZES = [16, 24, 32, 48, 64, 128, 256]
SUPER = 512  # 先在 512x512 上画，再按面积平均降采样，边缘自带抗锯齿

# 底色渐变（上 -> 下）
TOP_RGB = (30, 96, 180)
BOT_RGB = (52, 140, 226)


def _inside_round_rect(x, y, size, radius):
    cx = min(max(x, radius), size - 1 - radius)
    cy = min(max(y, radius), size - 1 - radius)
    dx, dy = x - cx, y - cy
    return dx * dx + dy * dy <= radius * radius


def _in_arrow(x, y, s):
    u = s / 1024.0
    # 箭杆
    if 232 * u <= x <= 600 * u and 452 * u <= y <= 572 * u:
        return True
    # 箭头
    x0, y0 = 560 * u, 336 * u
    x1, y1 = 560 * u, 688 * u
    xt, yt = 816 * u, 512 * u
    if x < x0 or x > xt:
        return False
    f = (x - x0) / (xt - x0)
    ytop = y0 + (yt - y0) * f
    ybot = y1 + (yt - y1) * f
    return ytop <= y <= ybot


def render_master(size=SUPER):
    """返回 size*size*4 的 RGBA bytearray"""
    px = bytearray(size * size * 4)
    radius = int(size * 0.195)
    for y in range(size):
        t = y / float(size - 1)
        r = int(TOP_RGB[0] + (BOT_RGB[0] - TOP_RGB[0]) * t)
        g = int(TOP_RGB[1] + (BOT_RGB[1] - TOP_RGB[1]) * t)
        b = int(TOP_RGB[2] + (BOT_RGB[2] - TOP_RGB[2]) * t)
        row = y * size
        for x in range(size):
            if not _inside_round_rect(x, y, size, radius):
                continue
            o = (row + x) * 4
            if _in_arrow(x, y, size):
                px[o] = px[o + 1] = px[o + 2] = 255
            else:
                px[o], px[o + 1], px[o + 2] = r, g, b
            px[o + 3] = 255
    return px


def downsample(src, ssize, dsize):
    """按面积平均降采样，支持任意整数/非整数比例"""
    px = bytearray(dsize * dsize * 4)
    for y in range(dsize):
        y0 = y * ssize // dsize
        y1 = max(y0 + 1, (y + 1) * ssize // dsize)
        for x in range(dsize):
            x0 = x * ssize // dsize
            x1 = max(x0 + 1, (x + 1) * ssize // dsize)
            sr = sg = sb = sa = 0
            n = 0
            for yy in range(y0, y1):
                base = yy * ssize * 4
                for xx in range(x0, x1):
                    o = base + xx * 4
                    a = src[o + 3]
                    sr += src[o] * a
                    sg += src[o + 1] * a
                    sb += src[o + 2] * a
                    sa += a
                    n += 1
            o = (y * dsize + x) * 4
            if sa:
                px[o] = sr // sa
                px[o + 1] = sg // sa
                px[o + 2] = sb // sa
            px[o + 3] = sa // n
    return px


def png_encode(w, h, rgba):
    def chunk(tag, data):
        return (struct.pack(">I", len(data)) + tag + data +
                struct.pack(">I", zlib.crc32(tag + data) & 0xFFFFFFFF))
    out = b"\x89PNG\r\n\x1a\n"
    out += chunk(b"IHDR", struct.pack(">IIBBBBB", w, h, 8, 6, 0, 0, 0))
    rows = b"".join(b"\x00" + bytes(rgba[y * w * 4:(y + 1) * w * 4]) for y in range(h))
    out += chunk(b"IDAT", zlib.compress(rows, 9))
    out += chunk(b"IEND", b"")
    return out


def build_icon_images():
    """返回 [(边长, PNG 字节), ...]，从大到小"""
    master = render_master(SUPER)
    return [(s, png_encode(s, s, downsample(master, SUPER, s)))
            for s in sorted(ICON_SIZES, reverse=True)]


def build_ico(images):
    """打包成 .ico（Vista+ 支持 PNG 条目）"""
    n = len(images)
    header = struct.pack("<HHH", 0, 1, n)
    offset = 6 + 16 * n
    entries, blobs = b"", b""
    for s, data in images:
        entries += struct.pack("<BBBBHHII",
                               0 if s >= 256 else s,
                               0 if s >= 256 else s,
                               0, 0, 1, 32, len(data), offset)
        blobs += data
        offset += len(data)
    return header + entries + blobs


# ============================================================ manifest

MANIFEST = b"""<?xml version="1.0" encoding="UTF-8" standalone="yes"?>
<assembly xmlns="urn:schemas-microsoft-com:asm.v1" manifestVersion="1.0">
  <assemblyIdentity type="win32" name="DB2KepServerEX" version="1.0.0.0" processorArchitecture="*"/>
  <description>Siemens DB source to KEPServerEX tag CSV converter</description>
  <dependency>
    <dependentAssembly>
      <assemblyIdentity type="win32" name="Microsoft.Windows.Common-Controls"
                        version="6.0.0.0" processorArchitecture="*"
                        publicKeyToken="6595b64144ccf1df" language="*"/>
    </dependentAssembly>
  </dependency>
  <application xmlns="urn:schemas-microsoft-com:asm.v3">
    <windowsSettings>
      <dpiAware xmlns="http://schemas.microsoft.com/SMI/2005/WindowsSettings">true</dpiAware>
      <longPathAware xmlns="http://schemas.microsoft.com/SMI/2016/WindowsSettings">true</longPathAware>
    </windowsSettings>
  </application>
  <trustInfo xmlns="urn:schemas-microsoft-com:asm.v3">
    <security>
      <requestedPrivileges>
        <requestedExecutionLevel level="asInvoker" uiAccess="false"/>
      </requestedPrivileges>
    </security>
  </trustInfo>
  <compatibility xmlns="urn:schemas-microsoft-com:compatibility.v1">
    <application>
      <supportedOS Id="{e2011457-1546-43c5-a5fe-008deee3d3f0}"/>
      <supportedOS Id="{35138b9a-5d96-4fbd-8e2d-a2440225f93a}"/>
      <supportedOS Id="{4a2f28e3-53b9-4441-ba9c-d69d4a4a6e38}"/>
      <supportedOS Id="{1f676c76-80e1-4239-95bb-83d0f6d0da78}"/>
      <supportedOS Id="{8e0f7a12-bfb3-4fe8-b9a5-48fd50a15a9a}"/>
    </application>
  </compatibility>
</assembly>
"""

RT_ICON = 3
RT_GROUP_ICON = 14
RT_MANIFEST = 24
LANG_ID = 0x0409  # en-US；资源语言不匹配时系统会回退，安全


def build_group_icon(images):
    """GRPICONDIR：把多个尺寸绑成一个图标组，nID 指向对应的 RT_ICON"""
    out = struct.pack("<HHH", 0, 1, len(images))
    for idx, (s, data) in enumerate(images):
        out += struct.pack("<BBBBHHIH",
                           0 if s >= 256 else s,
                           0 if s >= 256 else s,
                           0, 0, 1, 32, len(data), idx + 1)
    return out


# ============================================================ .rsrc 资源段

HIGH_BIT = 0x80000000


def build_rsrc(resources):
    """resources: [(类型ID, 资源ID, 语言ID, 数据bytes), ...] -> .rsrc 段内容 + 重定位偏移表"""
    types = {}
    for tid, rid, lang, data in resources:
        types.setdefault(tid, {}).setdefault(rid, {})[lang] = data

    type_ids = sorted(types)
    plan = []          # (类型ID, 资源ID, 语言ID)
    for tid in type_ids:
        for rid in sorted(types[tid]):
            for lang in sorted(types[tid][rid]):
                plan.append((tid, rid, lang))

    # ---- 第一遍：算各块偏移 ----
    off = 0
    lvl1_off = off
    off += 16 + 8 * len(type_ids)

    lvl2_off = {}
    for tid in type_ids:
        lvl2_off[tid] = off
        off += 16 + 8 * len(types[tid])

    lvl3_off = {}
    for tid in type_ids:
        for rid in sorted(types[tid]):
            lvl3_off[(tid, rid)] = off
            off += 16 + 8 * len(types[tid][rid])

    dataent_off = {}
    for tid, rid, lang in plan:
        dataent_off[(tid, rid, lang)] = off
        off += 16

    # ---- 第二遍：写内容 ----
    buf = bytearray()

    def dir_header(named, ids):
        return struct.pack("<IIHHHH", 0, 0, 0, 0, named, ids)

    # 一级目录（按类型）
    buf += dir_header(0, len(type_ids))
    for tid in type_ids:
        buf += struct.pack("<II", tid, HIGH_BIT | lvl2_off[tid])

    # 二级目录（按资源 ID）
    for tid in type_ids:
        rids = sorted(types[tid])
        buf += dir_header(0, len(rids))
        for rid in rids:
            buf += struct.pack("<II", rid, HIGH_BIT | lvl3_off[(tid, rid)])

    # 三级目录（按语言）
    for tid in type_ids:
        for rid in sorted(types[tid]):
            langs = sorted(types[tid][rid])
            buf += dir_header(0, len(langs))
            for lang in langs:
                buf += struct.pack("<II", lang, dataent_off[(tid, rid, lang)])

    # 数据项：OffsetToData 先占位，后面靠重定位改成 RVA
    reloc_offsets = []
    for tid, rid, lang in plan:
        data = types[tid][rid][lang]
        reloc_offsets.append(len(buf))          # 这个 4 字节字段要重定位
        buf += struct.pack("<IIII", 0, len(data), 0, 0)

    # 数据本体（4 字节对齐）
    blob_off = {}
    for tid, rid, lang in plan:
        while len(buf) % 4:
            buf += b"\x00"
        blob_off[(tid, rid, lang)] = len(buf)
        buf += types[tid][rid][lang]

    # 把每个数据项的加数写进去（链接器最终会写成 段RVA + 加数）
    for idx, key in enumerate(plan):
        struct.pack_into("<I", buf, reloc_offsets[idx], blob_off[key])

    return bytes(buf), reloc_offsets


# ============================================================ COFF 目标文件

IMAGE_FILE_MACHINE_AMD64 = 0x8664
IMAGE_SCN_CNT_INITIALIZED_DATA = 0x00000040
IMAGE_SCN_MEM_READ = 0x40000000
IMAGE_REL_AMD64_ADDR32 = 0x0002
IMAGE_SYM_CLASS_STATIC = 3

FILE_HEADER_SIZE = 20
SECTION_HEADER_SIZE = 40
RELOC_SIZE = 10
SYMBOL_SIZE = 18


def build_syso(rsrc, reloc_offsets):
    data_off = FILE_HEADER_SIZE + SECTION_HEADER_SIZE
    nrel = len(reloc_offsets)
    reloc_off = data_off + len(rsrc)
    symtab_off = reloc_off + RELOC_SIZE * nrel
    strtab_off = symtab_off + SYMBOL_SIZE

    header = struct.pack("<HHIIIHH",
                         IMAGE_FILE_MACHINE_AMD64,
                         1,            # NumberOfSections
                         0,            # TimeDateStamp
                         symtab_off,   # PointerToSymbolTable
                         1,            # NumberOfSymbols
                         0,            # SizeOfOptionalHeader
                         0)            # Characteristics

    sect = struct.pack("<8sIIIIIIHHI",
                       b".rsrc\x00\x00\x00",
                       len(rsrc),                       # VirtualSize
                       0,                               # VirtualAddress
                       len(rsrc),                       # SizeOfRawData
                       data_off,                        # PointerToRawData
                       reloc_off,                       # PointerToRelocations
                       0,                               # PointerToLinenumbers
                       nrel,                            # NumberOfRelocations
                       0,                               # NumberOfLinenumbers
                       IMAGE_SCN_CNT_INITIALIZED_DATA | IMAGE_SCN_MEM_READ)

    # 重定位必须按 VirtualAddress 升序（Go 的 loadpe 会排序，这里先排好）
    relocs = b"".join(struct.pack("<IIH", o, 0, IMAGE_REL_AMD64_ADDR32)
                      for o in sorted(reloc_offsets))

    # 段符号：Name 必须以 '.' 开头，Go 靠 issect() 认它是段符号
    symbol = struct.pack("<8sIhHBB", b".rsrc\x00\x00\x00", 0, 1, 0,
                         IMAGE_SYM_CLASS_STATIC, 0)

    strtab = struct.pack("<I", 4)  # 空字符串表，只有 4 字节长度头

    out = header + sect + rsrc + relocs + symbol + strtab
    assert len(out) == strtab_off + 4
    return out


def main():
    here = os.path.dirname(os.path.abspath(__file__))
    # 本脚本位于 tools/ 下，而 app.syso 必须落在包根目录（和 .go 文件同级）
    # 才会被 go build 自动识别，所以往上一级输出。
    root = os.path.dirname(here)
    out_path = os.path.join(root, "app.syso")

    images = build_icon_images()

    resources = []
    for idx, (s, data) in enumerate(images):
        resources.append((RT_ICON, idx + 1, LANG_ID, data))
    resources.append((RT_GROUP_ICON, 1, LANG_ID, build_group_icon(images)))
    resources.append((RT_MANIFEST, 1, LANG_ID, MANIFEST))

    rsrc, relocs = build_rsrc(resources)
    blob = build_syso(rsrc, relocs)
    with open(out_path, "wb") as f:
        f.write(blob)

    print("已生成 %s（%d 字节）" % (out_path, len(blob)))
    print("  图标尺寸：%s" % ", ".join(str(s) for s, _ in images))
    print("  资源条目：%d，重定位：%d" % (len(resources), len(relocs)))
    return 0


if __name__ == "__main__":
    sys.exit(main())
