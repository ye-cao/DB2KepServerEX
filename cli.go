//go:build cli

// ============================================================================
//  cli.go —— 控制台版入口（给脚本/自动化用）
//
//  编译： go build -tags cli -trimpath -ldflags "-s -w" -o DB2KepServerEX_命令行版.exe .
//  不写 -tags cli 时编译出的是窗口版(gui.go)。
// ============================================================================

package main

import (
	"bufio"
	"fmt"
	"os"
	"path/filepath"
	"sort"
	"strconv"
	"strings"
	"unicode/utf16"
	"unsafe"
)

var (
	pGetStdHandle   = k32.NewProc("GetStdHandle")
	pWriteConsoleW  = k32.NewProc("WriteConsoleW")
	pGetConsoleMode = k32.NewProc("GetConsoleMode")
	pGetConsoleCP   = k32.NewProc("GetConsoleCP")
)

const (
	stdInputHandle  = ^uintptr(9)  // -10
	stdOutputHandle = ^uintptr(10) // -11
	invalidHandle   = ^uintptr(0)
)

// printOut 走 WriteConsoleW，控制台代码页是什么都不影响中文显示；
// 输出被重定向到文件/管道时自动回退为 UTF-8 字节流。
func printOut(s string) {
	h, _, _ := pGetStdHandle.Call(stdOutputHandle)
	if h != 0 && h != invalidHandle {
		var mode uint32
		if r, _, _ := pGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode))); r != 0 {
			w := utf16.Encode([]rune(s))
			if len(w) > 0 {
				var written uint32
				pWriteConsoleW.Call(h, uintptr(unsafe.Pointer(&w[0])), uintptr(len(w)),
					uintptr(unsafe.Pointer(&written)), 0)
			}
			return
		}
	}
	os.Stdout.WriteString(s)
}

func stdinIsConsole() bool {
	h, _, _ := pGetStdHandle.Call(stdInputHandle)
	if h == 0 || h == invalidHandle {
		return false
	}
	var mode uint32
	r, _, _ := pGetConsoleMode.Call(h, uintptr(unsafe.Pointer(&mode)))
	return r != 0
}

var stdinReader = bufio.NewReader(os.Stdin)

// readLine 读一行输入；控制台下按控制台输入代码页解码，保证中文路径不乱码。
func readLine(prompt string) string {
	printOut(prompt)
	line, err := stdinReader.ReadString('\n')
	if err != nil && line == "" {
		return ""
	}
	line = strings.TrimRight(line, "\r\n")
	if stdinIsConsole() {
		if r, _, _ := pGetConsoleCP.Call(); r != 0 && uint32(r) != cpUTF8 {
			if w := mbToWide([]byte(line), uint32(r)); w != nil {
				line = string(utf16.Decode(w))
			}
		}
	}
	return strings.TrimSpace(line)
}

func dispWidth(s string) int {
	w := 0
	for _, r := range s {
		if r >= 0x1100 && (r <= 0x115F || r == 0x2329 || r == 0x232A ||
			(r >= 0x2E80 && r <= 0xA4CF) || (r >= 0xAC00 && r <= 0xD7A3) ||
			(r >= 0xF900 && r <= 0xFAFF) || (r >= 0xFE30 && r <= 0xFE6F) ||
			(r >= 0xFF00 && r <= 0xFF60) || (r >= 0xFFE0 && r <= 0xFFE6)) {
			w += 2
		} else {
			w++
		}
	}
	return w
}

func padRight(s string, width int) string {
	if d := width - dispWidth(s); d > 0 {
		return s + strings.Repeat(" ", d)
	}
	return s
}

const banner = `
========================================================
  西门子 DB 源文件(.awl / .db) -> KEPServerEX 点表 转换器
  按 S7-300/400 经典(非优化)DB 布局计算偏移
========================================================
`

func usage() {
	printOut(banner)
	printOut(`
用法：
  1) 直接双击本 exe，按提示输入文件路径和 DB 号
  2) 把 .awl / .db 文件拖到本 exe 上，再输入 DB 号
  3) 命令行：DB2KepServerEX_命令行版.exe <源文件> [DB号] [选项]

选项：
  -db N        数据块的 DB 号(1-65535)，等同于位置参数
  -block 名称  文件里有多个 DATA_BLOCK 时，指定要转换的块(名称或序号)
  -enc 编码    输出编码：utf8bom(默认) | utf8 | ansi(GBK)
  -o 路径      指定输出 CSV 路径，默认与源文件同目录
  -types 定义  自定义类型长度，逗号分隔，用于 UDT 等无法自动判断长度的类型
               格式：名称=字节数[:对齐]    例：-types "MyUDT=24,Other=8:1"
  -listtypes   打印支持的数据类型表后退出
`)
}

func listTypes() {
	printOut("\n【标量类型】直接生成 KEPServerEX 标签\n")
	names := make([]string, 0, len(scalarTable))
	for k := range scalarTable {
		names = append(names, k)
	}
	sort.Strings(names)
	printOut(fmt.Sprintf("  %-16s %-8s %-12s %s\n", "西门子类型", "字节", "地址关键字", "CSV Data Type"))
	for _, n := range names {
		s := scalarTable[n]
		sz := "1 位"
		if !s.isBool {
			sz = strconv.Itoa(s.size)
		}
		printOut(fmt.Sprintf("  %-16s %-8s %-12s %s\n", n, sz, s.kep, s.csv))
	}
	printOut("  STRING[n] 也支持，占 n+2 字节，地址写作 STRING<偏移>.<n>\n")

	printOut("\n【结构体类型】只占位推进偏移，不生成标签\n")
	rnames := make([]string, 0, len(reserveTable))
	for k := range reserveTable {
		rnames = append(rnames, k)
	}
	sort.Strings(rnames)
	line := "  "
	for i, n := range rnames {
		line += fmt.Sprintf("%s(%d)  ", n, reserveTable[n])
		if (i+1)%5 == 0 {
			printOut(line + "\n")
			line = "  "
		}
	}
	if strings.TrimSpace(line) != "" {
		printOut(line + "\n")
	}
	printOut("\n  其它类型请用 -types \"名称=字节数\" 指定长度。\n\n")
}

// parseArgs 手写解析，允许开关出现在文件名前后任意位置
// （Go 标准 flag 包遇到第一个位置参数就停止解析，不适合拖拽场景）
func parseArgs(argv []string) (pos []string, opts map[string]string) {
	opts = map[string]string{"enc": "utf8bom", "o": "", "block": "", "db": "", "types": ""}
	aliases := map[string]string{
		"-enc": "enc", "--enc": "enc",
		"-o": "o", "--o": "o", "-out": "o", "--out": "o",
		"-block": "block", "--block": "block",
		"-db": "db", "--db": "db",
		"-types": "types", "--types": "types",
	}
	for i := 0; i < len(argv); i++ {
		a := argv[i]
		low := strings.ToLower(a)
		if low == "-h" || low == "--help" || low == "-?" || low == "/?" {
			usage()
			os.Exit(0)
		}
		if low == "-listtypes" || low == "--listtypes" {
			listTypes()
			os.Exit(0)
		}
		key, inline := "", false
		if v, ok := aliases[low]; ok {
			key = v
		} else if eq := strings.Index(a, "="); eq > 0 {
			if v, ok := aliases[strings.ToLower(a[:eq])]; ok {
				key, inline = v, true
				opts[key] = a[eq+1:]
			}
		}
		if key == "" {
			if strings.HasPrefix(a, "-") && len(a) > 1 {
				printOut("  ！忽略未知参数：" + a + "\n")
				continue
			}
			pos = append(pos, strings.Trim(a, "\""))
			continue
		}
		if !inline && i+1 < len(argv) {
			opts[key] = argv[i+1]
			i++
		}
	}
	return
}

func reportResult(res *convertResult, dbNum int, enc string) {
	printOut(fmt.Sprintf("  变量总数：%d 个（其中 Bool %d 个）\n", res.NScalar, res.NBool))
	printOut(fmt.Sprintf("  推算块长度：%d 字节\n", res.Total))
	if len(res.Reserved) > 0 {
		printOut(fmt.Sprintf("\n  以下 %d 个结构体类型只占位、不生成标签：\n", len(res.Reserved)))
		for _, it := range res.Reserved {
			printOut(fmt.Sprintf("    %s : %s（%d 字节，偏移 %d）\n",
				it.name, it.rawType, it.spec.size, it.offset))
		}
		printOut("    如需读取其内部成员，请对照 TIA 里的实际布局手动添加。\n")
	}
	if len(res.BadNames) > 0 {
		printOut(fmt.Sprintf("\n  ！！有 %d 个标签名不符合 KEPServerEX 的命名规则，导入可能被拒：\n", len(res.BadNames)))
		for _, n := range res.BadNames {
			printOut("      " + n + "\n")
		}
		printOut("     KEPServerEX 不允许：英文句点 .、双引号 \"、开头的下划线 _、名称首尾的空格。\n")
		printOut("     请在 TIA 里改掉这些变量名，或导入后手动重命名。\n")
	}
	printOut("\n  地址格式示例：\n")
	shown := 0
	for _, it := range res.Items {
		if it.spec.kind != kScalar {
			continue
		}
		if shown >= 3 {
			break
		}
		printOut("    " + padRight(it.name, 30) + " " + addressOf(it, dbNum) + "\n")
		shown++
	}
	printOut("    ...\n")
	printOut("\n  √ 已生成：" + res.OutPath + "\n")
	printOut(fmt.Sprintf("    共 %d 条标签，编码 %s\n", res.NScalar, strings.ToLower(enc)))
	printOut("\n  导入方法：KEPServerEX 里选中目标 Device -> File -> Import CSV\n")
	printOut("  若导入后中文乱码，用 -enc ansi 重新生成一次。\n")
}

func main() {
	args, opts := parseArgs(os.Args[1:])
	enc := opts["enc"]
	outPath := opts["o"]
	if bad := parseUserTypes(opts["types"]); len(bad) > 0 {
		printOut("  ！以下 -types 定义无法解析，已忽略：" + strings.Join(bad, ", ") + "\n")
	}

	printOut(banner)

	// ---- 源文件 ----
	srcPath := ""
	if len(args) > 0 {
		srcPath = args[0]
	}
	for {
		if srcPath == "" {
			srcPath = strings.Trim(readLine("请输入源文件路径 (.awl / .db): "), "\"")
		}
		if srcPath == "" {
			continue
		}
		if st, err := os.Stat(srcPath); err != nil || st.IsDir() {
			printOut("  × 文件不存在：" + srcPath + "\n")
			srcPath = ""
			continue
		}
		break
	}

	printOut("\n正在解析 " + filepath.Base(srcPath) + " ...\n")
	sf, err := loadSource(srcPath)
	if err != nil {
		printOut("  × 读取失败：" + err.Error() + "\n")
		os.Exit(1)
	}
	printOut("  源文件编码：" + cpName(sf.CP) + "\n")

	// ---- 选块 ----
	pick := 0
	if len(sf.Blocks) > 1 {
		printOut(fmt.Sprintf("  文件里发现 %d 个 DATA_BLOCK：\n", len(sf.Blocks)))
		for i, b := range sf.Blocks {
			nm := b.name
			if nm == "" {
				nm = "(未命名)"
			}
			cnt := fmt.Sprintf("%d 个变量", blockCount(b))
			if blockCount(b) < 0 {
				cnt = "解析异常"
			}
			printOut(fmt.Sprintf("    %d) %-28s %s\n", i+1, nm, cnt))
		}
		sel := strings.TrimSpace(opts["block"])
		switch {
		case sel != "":
			if n, e := strconv.Atoi(sel); e == nil && n >= 1 && n <= len(sf.Blocks) {
				pick = n - 1
			} else {
				for i, b := range sf.Blocks {
					if strings.EqualFold(b.name, sel) {
						pick = i
						break
					}
				}
			}
		case stdinIsConsole():
			for {
				s := readLine(fmt.Sprintf("  请输入要转换的块 (1-%d 或块名): ", len(sf.Blocks)))
				if n, e := strconv.Atoi(s); e == nil && n >= 1 && n <= len(sf.Blocks) {
					pick = n - 1
					break
				}
				found := false
				for i, b := range sf.Blocks {
					if strings.EqualFold(b.name, s) {
						pick, found = i, true
						break
					}
				}
				if found {
					break
				}
				printOut("  × 选择无效，请重新输入\n")
			}
		default:
			printOut("  × 非交互模式下请用 -block 指定要转换的块。\n")
			os.Exit(1)
		}
	}
	blk := sf.Blocks[pick]
	if blk.name != "" {
		printOut("  数据块名称：" + blk.name + "\n")
	}

	// ---- DB 号 ----
	dbNum := 0
	dbArg := opts["db"]
	if dbArg == "" && len(args) > 1 {
		dbArg = args[1]
	}
	if n, e := strconv.Atoi(strings.TrimSpace(dbArg)); e == nil {
		dbNum = n
	}
	for dbNum < 1 || dbNum > 65535 {
		s := readLine("请输入该数据块的 DB 号 (1-65535): ")
		n, e := strconv.Atoi(s)
		if e != nil || n < 1 || n > 65535 {
			printOut("  × 输入无效，请输入 1-65535 之间的整数\n")
			continue
		}
		dbNum = n
	}

	// ---- 优化块告警 ----
	if blk.optimized {
		printOut("\n  ！！警告：该块声明了 S7_Optimized_Access := 'TRUE'（优化块）\n")
		printOut("     优化块没有固定绝对偏移，下面算出的地址在 PLC 里是无效的。\n")
		printOut("     优化块请改用符号寻址（KEPServerEX 的 S7 Plus 驱动）。\n")
		if stdinIsConsole() {
			a := strings.ToLower(readLine("     仍要生成吗？(y/N): "))
			if a != "y" && a != "yes" {
				printOut("  已取消。\n")
				os.Exit(2)
			}
		} else {
			printOut("     （非交互模式，继续生成，请自行确认。）\n")
		}
	}

	// ---- 转换 ----
	res, err := convertBlock(sf, blk, dbNum, enc, outPath)
	if err != nil {
		printOut("\n  × " + err.Error() + "\n")
		if _, ok := err.(*resolveErr); ok {
			printOut("    该类型会占用多少字节无法确定，后面的变量偏移会全部错位，因此已中止。\n")
			printOut("    若是自定义 UDT，请用 -types \"类型名=字节数\" 指定长度后重试。\n")
			printOut("    用 -listtypes 可以查看已支持的类型表。\n")
		}
		os.Exit(1)
	}
	reportResult(res, dbNum, enc)

	if stdinIsConsole() {
		printOut("\n  按回车键退出...")
		stdinReader.ReadString('\n')
	}
}
