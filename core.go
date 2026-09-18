// ============================================================================
//  core.go —— 共享核心：编码识别、类型表、DB 解析、偏移计算、CSV 生成
//
//  本文件不含任何 UI 代码，控制台版(cli.go)和窗口版(gui.go)共用。
// ============================================================================

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"syscall"
	"unicode/utf16"
	"unicode/utf8"
	"unsafe"
)

// ============================================================ 字符编码(Win32)

var (
	k32              = syscall.NewLazyDLL("kernel32.dll")
	pMultiByteToWide = k32.NewProc("MultiByteToWideChar")
	pWideCharToMulti = k32.NewProc("WideCharToMultiByte")
)

const (
	cpUTF8 = 65001 // UTF-8
	cpGBK  = 936   // GBK / ANSI(简体中文)
	cpU16L = 1200  // UTF-16 LE
	cpU16B = 1201  // UTF-16 BE
)

func mbToWide(b []byte, cp uint32) []uint16 {
	if len(b) == 0 {
		return nil
	}
	n, _, _ := pMultiByteToWide.Call(uintptr(cp), 0,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)), 0, 0)
	if n == 0 {
		return nil
	}
	buf := make([]uint16, n)
	r, _, _ := pMultiByteToWide.Call(uintptr(cp), 0,
		uintptr(unsafe.Pointer(&b[0])), uintptr(len(b)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(n))
	if r == 0 {
		return nil
	}
	return buf
}

func wideToMB(w []uint16, cp uint32) []byte {
	if len(w) == 0 {
		return nil
	}
	n, _, _ := pWideCharToMulti.Call(uintptr(cp), 0,
		uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)), 0, 0, 0, 0)
	if n == 0 {
		return nil
	}
	buf := make([]byte, n)
	r, _, _ := pWideCharToMulti.Call(uintptr(cp), 0,
		uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)),
		uintptr(unsafe.Pointer(&buf[0])), uintptr(n), 0, 0)
	if r == 0 {
		return nil
	}
	return buf
}

// detectCP 自动识别源文件编码
func detectCP(b []byte) uint32 {
	if len(b) >= 3 && b[0] == 0xEF && b[1] == 0xBB && b[2] == 0xBF {
		return cpUTF8
	}
	if len(b) >= 2 && b[0] == 0xFF && b[1] == 0xFE {
		return cpU16L
	}
	if len(b) >= 2 && b[0] == 0xFE && b[1] == 0xFF {
		return cpU16B
	}
	if utf8.Valid(b) {
		return cpUTF8
	}
	return cpGBK
}

func cpName(cp uint32) string {
	switch cp {
	case cpUTF8:
		return "UTF-8"
	case cpGBK:
		return "GBK / ANSI"
	case cpU16L:
		return "UTF-16 LE"
	case cpU16B:
		return "UTF-16 BE"
	}
	return "?"
}

func readSource(path string) (string, uint32, error) {
	b, err := os.ReadFile(path)
	if err != nil {
		return "", 0, err
	}
	cp := detectCP(b)
	w := mbToWide(b, cp)
	if w == nil {
		return strings.TrimPrefix(string(b), "\uFEFF"), cp, nil
	}
	return strings.TrimPrefix(string(utf16.Decode(w)), "\uFEFF"), cp, nil
}

// ============================================================ 类型表
//
//  kScalar  -> 能直接建 KEPServerEX 标签，写进 CSV
//  kReserve -> 结构体 / 无对应地址写法，只占位推进偏移，不产生标签
//  两者都不是 -> 长度无法确定，明确报错中止(避免偏移整体错位)

type tkind int

const (
	kScalar tkind = iota
	kReserve
)

type typeSpec struct {
	size   int
	align  int
	kind   tkind
	isBool bool
	kep    string // KEPServerEX 地址里的 S7 数据类型关键字
	csv    string // CSV 的 Data Type 列
	strLen int
}

var scalarTable = map[string]typeSpec{
	"BOOL": {isBool: true, kep: "X", csv: "Boolean"},

	"BYTE":  {size: 1, align: 1, kep: "B", csv: "Byte"},
	"USINT": {size: 1, align: 1, kep: "B", csv: "Byte"},
	"SINT":  {size: 1, align: 1, kep: "C", csv: "Char"},
	"SBYTE": {size: 1, align: 1, kep: "C", csv: "Char"},
	"CHAR":  {size: 1, align: 1, kep: "C", csv: "Char"},

	"WORD":    {size: 2, align: 2, kep: "W", csv: "Word"},
	"UINT":    {size: 2, align: 2, kep: "W", csv: "Word"},
	"INT":     {size: 2, align: 2, kep: "I", csv: "Short"},
	"S5TIME":  {size: 2, align: 2, kep: "W", csv: "Word"},
	"TIMER":   {size: 2, align: 2, kep: "W", csv: "Word"},
	"COUNTER": {size: 2, align: 2, kep: "W", csv: "Word"},
	"DATE":    {size: 2, align: 2, kep: "DATE", csv: "String"},

	"DWORD":       {size: 4, align: 2, kep: "D", csv: "DWord"},
	"UDINT":       {size: 4, align: 2, kep: "D", csv: "DWord"},
	"DINT":        {size: 4, align: 2, kep: "DI", csv: "Long"},
	"REAL":        {size: 4, align: 2, kep: "REAL", csv: "Float"},
	"TIME":        {size: 4, align: 2, kep: "TIME", csv: "String"},
	"TOD":         {size: 4, align: 2, kep: "TOD", csv: "String"},
	"TIME_OF_DAY": {size: 4, align: 2, kep: "TOD", csv: "String"},

	"DT":            {size: 8, align: 2, kep: "DT", csv: "String"},
	"DATE_AND_TIME": {size: 8, align: 2, kep: "DT", csv: "String"},
	"LREAL":         {size: 8, align: 2, kep: "LREAL", csv: "Double"},
	"LINT":          {size: 8, align: 2, kep: "LINT", csv: "LLong"},
	"ULINT":         {size: 8, align: 2, kep: "LINT", csv: "LLong"},
	"LWORD":         {size: 8, align: 2, kep: "LWORD", csv: "QWord"},
}

// 结构体 / 无对应地址写法的类型：按西门子 SDT 官方长度占位，不产生标签
var reserveTable = map[string]int{
	"DTL":           12,
	"LTIME":         8,
	"LDT":           8,
	"IEC_TIMER":     16,
	"TON":           16,
	"TOF":           16,
	"TP":            16,
	"TONR":          16,
	"IEC_SCOUNTER":  3,
	"IEC_USCOUNTER": 3,
	"IEC_COUNTER":   6,
	"IEC_UCOUNTER":  6,
	"CTU":           6,
	"CTD":           6,
	"CTUD":          6,
	"IEC_DCOUNTER":  12,
	"IEC_UDCOUNTER": 12,
	"ERROR_STRUCT":  28,
	"CREF":          8,
	"NREF":          8,
	"VREF":          12,
	"CONDITIONS":    52,
	"TADDR_PARAM":   8,
	"TCON_PARAM":    64,
	"VOID":          0,
	"POINTER":       6,
	"ANY":           10,
}

var userTypes = map[string]typeSpec{}

type resolveErr struct{ name string }

func (e *resolveErr) Error() string { return "无法确定长度的数据类型: " + e.name }

// resolveType 解析类型表达式，返回规格与数组元素个数
func resolveType(expr string) (typeSpec, int, error) {
	t := strings.ToUpper(strings.Join(strings.Fields(expr), ""))

	if strings.HasPrefix(t, "ARRAY[") {
		e := strings.Index(t, "]")
		if e < 0 {
			return typeSpec{}, 0, &resolveErr{expr}
		}
		rng := t[6:e]
		rest := strings.TrimPrefix(t[e+1:], "OF")
		p := strings.Index(rng, "..")
		if p < 0 {
			return typeSpec{}, 0, &resolveErr{expr}
		}
		lo, err1 := strconv.Atoi(strings.TrimSpace(rng[:p]))
		hi, err2 := strconv.Atoi(strings.TrimSpace(rng[p+2:]))
		if err1 != nil || err2 != nil || hi < lo {
			return typeSpec{}, 0, &resolveErr{expr}
		}
		sp, _, err := resolveType(rest)
		if err != nil {
			return typeSpec{}, 0, err
		}
		return sp, hi - lo + 1, nil
	}

	if strings.HasPrefix(t, "STRING") {
		n := 254
		if s := strings.Index(t, "["); s >= 0 {
			if e := strings.Index(t, "]"); e > s {
				if v, err := strconv.Atoi(t[s+1 : e]); err == nil {
					n = v
				}
			}
		}
		return typeSpec{size: n + 2, align: 1, kind: kScalar,
			kep: "STRING", csv: "String", strLen: n}, 1, nil
	}

	if strings.HasPrefix(t, "WSTRING") {
		n := 254
		if s := strings.Index(t, "["); s >= 0 {
			if e := strings.Index(t, "]"); e > s {
				if v, err := strconv.Atoi(t[s+1 : e]); err == nil {
					n = v
				}
			}
		}
		return typeSpec{size: 2*n + 4, align: 2, kind: kReserve}, 1, nil
	}

	if sp, ok := scalarTable[t]; ok {
		return sp, 1, nil
	}
	if sz, ok := reserveTable[t]; ok {
		return typeSpec{size: sz, align: 2, kind: kReserve}, 1, nil
	}
	if sp, ok := userTypes[t]; ok {
		return sp, 1, nil
	}
	return typeSpec{}, 0, &resolveErr{expr}
}

// parseUserTypes 解析 "名称=字节数[:对齐],..." 形式的自定义类型定义，
// 返回无法解析的条目
func parseUserTypes(def string) []string {
	var bad []string
	for _, part := range strings.Split(def, ",") {
		part = strings.TrimSpace(part)
		if part == "" {
			continue
		}
		eq := strings.Index(part, "=")
		if eq <= 0 {
			bad = append(bad, part)
			continue
		}
		name := strings.ToUpper(strings.TrimSpace(part[:eq]))
		val := strings.TrimSpace(part[eq+1:])
		align := 2
		if c := strings.Index(val, ":"); c > 0 {
			if v, err := strconv.Atoi(strings.TrimSpace(val[c+1:])); err == nil {
				align = v
			}
			val = strings.TrimSpace(val[:c])
		}
		sz, err := strconv.Atoi(val)
		if err != nil || sz < 0 {
			bad = append(bad, part)
			continue
		}
		userTypes[name] = typeSpec{size: sz, align: align, kind: kReserve}
	}
	return bad
}

// ============================================================ 成员解析

type item struct {
	name    string
	rawType string
	comment string
	spec    typeSpec
	offset  int
	bit     int
}

func stripBraces(s string) string {
	for {
		i := strings.Index(s, "{")
		if i < 0 {
			return s
		}
		j := strings.Index(s[i:], "}")
		if j < 0 {
			return s[:i]
		}
		s = s[:i] + " " + s[i+j+1:]
	}
}

func splitComment(s string) (string, string) {
	if i := strings.Index(s, "//"); i >= 0 {
		return s[:i], strings.TrimSpace(s[i+2:])
	}
	return s, ""
}

// parseMembers 解析 STRUCT 内的成员，支持嵌套 STRUCT(用下划线拼接名字)
func parseMembers(lines []string) ([]item, error) {
	var out []item
	var stack []string
	started := false

	for _, raw := range lines {
		s := strings.TrimSpace(raw)
		if s == "" || strings.HasPrefix(s, "//") {
			continue
		}
		up := strings.ToUpper(s)

		if !started {
			if strings.HasPrefix(up, "STRUCT") && !strings.Contains(up, ":") {
				started = true
			}
			continue
		}
		if strings.HasPrefix(up, "END_STRUCT") {
			if len(stack) > 0 {
				stack = stack[:len(stack)-1]
				continue
			}
			break
		}

		body, comment := splitComment(stripBraces(s))
		body = strings.TrimSpace(strings.TrimSuffix(strings.TrimSpace(body), ";"))
		if body == "" {
			continue
		}
		ci := strings.Index(body, ":")
		if ci < 0 {
			continue
		}
		name := strings.TrimSpace(body[:ci])
		typ := strings.TrimSpace(body[ci+1:])
		if name == "" || typ == "" {
			continue
		}

		if strings.EqualFold(typ, "STRUCT") {
			stack = append(stack, name)
			continue
		}

		prefix := ""
		for _, f := range stack {
			prefix += f + "_"
		}

		sp, count, err := resolveType(typ)
		if err != nil {
			return nil, err
		}

		if sp.kind == kReserve {
			out = append(out, item{name: prefix + name, rawType: typ,
				spec: typeSpec{size: sp.size * count, align: sp.align, kind: kReserve}})
			continue
		}
		if count > 1 {
			for i := 0; i < count; i++ {
				out = append(out, item{
					name:    fmt.Sprintf("%s%s[%d]", prefix, name, i),
					rawType: typ, comment: comment, spec: sp,
				})
			}
		} else {
			out = append(out, item{name: prefix + name, rawType: typ, comment: comment, spec: sp})
		}
	}
	return out, nil
}

// layout 按 S7-300/400 经典(非优化) DB 布局规则分配偏移：
// BOOL 按位紧凑；WORD/INT/DWORD/REAL 等需落在偶数字节(字对齐)。
func layout(items []item) int {
	off, bit := 0, 0
	for i := range items {
		if items[i].spec.isBool {
			items[i].offset, items[i].bit = off, bit
			bit++
			if bit == 8 {
				bit, off = 0, off+1
			}
			continue
		}
		if bit > 0 {
			bit, off = 0, off+1
		}
		if items[i].spec.align == 2 && off%2 != 0 {
			off++
		}
		items[i].offset = off
		off += items[i].spec.size
	}
	if bit > 0 {
		off++
	}
	return off
}

func addressOf(it item, db int) string {
	if it.spec.isBool {
		return fmt.Sprintf("DB%d,X%d.%d", db, it.offset, it.bit)
	}
	if it.spec.strLen > 0 {
		return fmt.Sprintf("DB%d,STRING%d.%d", db, it.offset, it.spec.strLen)
	}
	return fmt.Sprintf("DB%d,%s%d", db, it.spec.kep, it.offset)
}

// ============================================================ 标签名检查

// badTagName 检查生成的标签名是否踩了 KEPServerEX 的命名限制。
// 依据 PTC 官方帮助 "Properly Name a Channel, Device, Tag, and Tag Group"：
// 保留/受限字符为 —— 英文句点、双引号、开头的下划线、首尾空格。
// 返回空串表示没问题，否则返回原因。
func badTagName(n string) string {
	switch {
	case n == "":
		return "名称为空"
	case strings.HasPrefix(n, "_"):
		return "以 _ 开头"
	case strings.Contains(n, "."):
		return "含英文句点"
	case strings.Contains(n, "\""):
		return "含双引号"
	case strings.TrimSpace(n) != n:
		return "首尾有空格"
	}
	return ""
}

// ============================================================ CSV 输出

var csvHeader = []string{
	"Tag Name", "Address", "Data Type", "Respect Data Type", "Client Access",
	"Scan Rate", "Scaling", "Raw Low", "Raw High", "Scaled Low", "Scaled High",
	"Scaled Data Type", "Clamp Low", "Clamp High", "Eng. Units", "Description",
	"Negate Value",
}

func csvField(s string) string {
	if strings.ContainsAny(s, ",\"\r\n") {
		return "\"" + strings.ReplaceAll(s, "\"", "\"\"") + "\""
	}
	return s
}

func buildCSV(items []item, db int) string {
	var b strings.Builder
	hdr := make([]string, len(csvHeader))
	for i, h := range csvHeader {
		hdr[i] = csvField(h)
	}
	b.WriteString(strings.Join(hdr, ",") + "\r\n")

	for _, it := range items {
		if it.spec.kind != kScalar {
			continue
		}
		row := []string{
			csvField(it.name),
			csvField(addressOf(it, db)),
			it.spec.csv,
			"1",          // Respect Data Type
			"Read/Write", // Client Access
			"1000",       // Scan Rate
			"None",       // Scaling
			"0", "0", "0", "0",
			it.spec.csv, // Scaled Data Type
			"0", "0",    // Clamp Low / High
			"",          // Eng. Units
			csvField(it.comment),
			"0", // Negate Value
		}
		b.WriteString(strings.Join(row, ",") + "\r\n")
	}
	return b.String()
}

func writeOut(path, content, enc string) error {
	switch strings.ToLower(strings.TrimSpace(enc)) {
	case "ansi", "gbk", "936", "gb2312":
		b := wideToMB(utf16.Encode([]rune(content)), cpGBK)
		if b == nil {
			return fmt.Errorf("按 GBK 编码失败")
		}
		return os.WriteFile(path, b, 0644)
	case "utf8", "utf-8":
		return os.WriteFile(path, []byte(content), 0644)
	default: // utf8bom
		out := make([]byte, 0, len(content)+3)
		out = append(out, 0xEF, 0xBB, 0xBF)
		out = append(out, []byte(content)...)
		return os.WriteFile(path, out, 0644)
	}
}

// ============================================================ 数据块

type block struct {
	name      string
	lines     []string
	optimized bool
}

func quotedName(s string) string {
	a := strings.Index(s, "\"")
	if a < 0 {
		return ""
	}
	b := strings.Index(s[a+1:], "\"")
	if b < 0 {
		return ""
	}
	return s[a+1 : a+1+b]
}

// splitBlocks 把一个源文件里的多个 DATA_BLOCK 拆开。
// 没有任何 DATA_BLOCK 时，整个文件当做一个匿名块。
func splitBlocks(lines []string) []block {
	var out []block
	var cur *block
	for _, ln := range lines {
		s := strings.TrimSpace(ln)
		up := strings.ToUpper(s)
		if strings.HasPrefix(up, "DATA_BLOCK") {
			if cur != nil {
				out = append(out, *cur)
			}
			cur = &block{name: quotedName(s)}
		}
		if cur == nil {
			continue
		}
		cur.lines = append(cur.lines, ln)

		// 优化块检测。
		// 注意：TIA 导出的属性是写在花括号里的 —— { S7_Optimized_Access := 'TRUE' }，
		// 所以不能只看行首。这里直接在原始行里找关键字，只把行尾 // 注释剥掉
		// （不能用 stripBraces，它是连花括号内容一起删的）。
		attr := strings.ToUpper(s)
		if i := strings.Index(attr, "//"); i >= 0 {
			attr = attr[:i]
		}
		if strings.Contains(attr, "S7_OPTIMIZED_ACCESS") && strings.Contains(attr, "TRUE") {
			cur.optimized = true
		}
		if strings.HasPrefix(up, "END_DATA_BLOCK") {
			out = append(out, *cur)
			cur = nil
		}
	}
	if cur != nil {
		out = append(out, *cur)
	}
	if len(out) == 0 {
		out = append(out, block{lines: lines})
	}
	return out
}

// ============================================================ 转换入口(共用)

type srcFile struct {
	Path   string
	CP     uint32
	Blocks []block
}

func loadSource(path string) (*srcFile, error) {
	src, cp, err := readSource(path)
	if err != nil {
		return nil, err
	}
	lines := strings.Split(strings.ReplaceAll(src, "\r\n", "\n"), "\n")
	return &srcFile{Path: path, CP: cp, Blocks: splitBlocks(lines)}, nil
}

// blockCount 返回该块可生成的标签数；解析失败返回 -1
func blockCount(b block) int {
	its, err := parseMembers(b.lines)
	if err != nil {
		return -1
	}
	n := 0
	for _, it := range its {
		if it.spec.kind == kScalar {
			n++
		}
	}
	return n
}

type convertResult struct {
	BlockName string
	Items     []item
	Total     int
	NScalar   int
	NBool     int
	Reserved  []item
	BadNames  []string // 不符合 KEPServerEX 命名规则的标签名
	OutPath   string
}

func defaultOutPath(srcPath, blockName string, multi bool, dbNum int) string {
	base := strings.TrimSuffix(filepath.Base(srcPath), filepath.Ext(srcPath))
	if blockName != "" && multi {
		base = base + "_" + blockName
	}
	return filepath.Join(filepath.Dir(srcPath),
		fmt.Sprintf("%s_DB%d_KEPServerEX.csv", base, dbNum))
}

// convertBlock 解析指定块 -> 计算偏移 -> 写出 CSV
func convertBlock(sf *srcFile, blk block, dbNum int, enc, outPath string) (*convertResult, error) {
	items, err := parseMembers(blk.lines)
	if err != nil {
		return nil, err
	}
	res := &convertResult{BlockName: blk.name, Items: items}
	seen := make(map[string]int, len(items))
	for _, it := range items {
		if it.spec.kind == kReserve {
			res.Reserved = append(res.Reserved, it)
			continue
		}
		res.NScalar++
		if it.spec.isBool {
			res.NBool++
		}
		seen[it.name]++
		if why := badTagName(it.name); why != "" {
			res.BadNames = append(res.BadNames, it.name+"（"+why+"）")
		}
	}

	// 重名检查：嵌套 STRUCT 展平时用下划线拼接，父子结构名字组合后可能撞名。
	// KEPServerEX 要求标签名唯一，撞名会导致导入时后者覆盖前者。
	var dups []string
	for n, c := range seen {
		if c > 1 {
			dups = append(dups, n)
		}
	}
	sort.Strings(dups)
	for _, n := range dups {
		res.BadNames = append(res.BadNames, fmt.Sprintf("%s（重名 %d 次）", n, seen[n]))
	}
	if res.NScalar == 0 {
		return nil, fmt.Errorf("没解析到任何变量，请确认文件是 DATA_BLOCK 源文件且含 STRUCT...END_STRUCT")
	}
	res.Total = layout(items)
	res.OutPath = outPath
	if res.OutPath == "" {
		res.OutPath = defaultOutPath(sf.Path, blk.name, len(sf.Blocks) > 1, dbNum)
	}
	if err := writeOut(res.OutPath, buildCSV(items, dbNum), enc); err != nil {
		return nil, err
	}
	return res, nil
}
