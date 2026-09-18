//go:build !cli

// ============================================================================
//  gui.go —— 窗口版入口（给同事双击使用）
//
//  编译： go build -trimpath -ldflags "-s -w -H windowsgui" -o DB2KepServerEX.exe .
//  纯 Win32 API + Go 标准库，无第三方依赖，产出单个 exe。
//  界面外观依赖同目录下的 app.syso（内嵌 Common-Controls v6 manifest），
//  该文件由 make_manifest_syso.py 生成。
// ============================================================================

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"syscall"
	"unsafe"
)

// ============================================================ Win32 DLL / API

var (
	u32  = syscall.NewLazyDLL("user32.dll")
	g32  = syscall.NewLazyDLL("gdi32.dll")
	sh32 = syscall.NewLazyDLL("shell32.dll")
	cd32 = syscall.NewLazyDLL("comdlg32.dll")

	pGetModuleHandleW  = k32.NewProc("GetModuleHandleW")
	pRegisterClassExW  = u32.NewProc("RegisterClassExW")
	pCreateWindowExW   = u32.NewProc("CreateWindowExW")
	pDefWindowProcW    = u32.NewProc("DefWindowProcW")
	pDestroyWindow     = u32.NewProc("DestroyWindow")
	pGetMessageW       = u32.NewProc("GetMessageW")
	pTranslateMessage  = u32.NewProc("TranslateMessage")
	pDispatchMessageW  = u32.NewProc("DispatchMessageW")
	pPostQuitMessage   = u32.NewProc("PostQuitMessage")
	pSendMessageW      = u32.NewProc("SendMessageW")
	pSetWindowTextW    = u32.NewProc("SetWindowTextW")
	pGetWindowTextW    = u32.NewProc("GetWindowTextW")
	pGetWindowTextLenW = u32.NewProc("GetWindowTextLengthW")
	pMessageBoxW       = u32.NewProc("MessageBoxW")
	pEnableWindow      = u32.NewProc("EnableWindow")
	pShowWindow        = u32.NewProc("ShowWindow")
	pUpdateWindow      = u32.NewProc("UpdateWindow")
	pLoadCursorW       = u32.NewProc("LoadCursorW")
	pLoadIconW         = u32.NewProc("LoadIconW")
	pAdjustWindowRect  = u32.NewProc("AdjustWindowRect")
	pGetDC             = u32.NewProc("GetDC")
	pReleaseDC         = u32.NewProc("ReleaseDC")
	pGetSysColorBrush  = u32.NewProc("GetSysColorBrush")
	pGetSystemMetrics  = u32.NewProc("GetSystemMetrics")
	pLoadImageW        = u32.NewProc("LoadImageW")

	pCreateFontW   = g32.NewProc("CreateFontW")
	pGetDeviceCaps = g32.NewProc("GetDeviceCaps")

	pGetOpenFileNameW = cd32.NewProc("GetOpenFileNameW")
	pShellExecuteW    = sh32.NewProc("ShellExecuteW")
	pDragAcceptFiles  = sh32.NewProc("DragAcceptFiles")
	pDragQueryFileW   = sh32.NewProc("DragQueryFileW")
	pDragFinish       = sh32.NewProc("DragFinish")
)

const (
	swShow     = 5
	cwUseDefault = 0x80000000

	wsChild    = 0x40000000
	wsVisible  = 0x10000000
	wsTabStop  = 0x00010000
	wsBorder   = 0x00800000
	wsVScroll  = 0x00200000
	wsDisabled = 0x08000000

	esLeft       = 0x0000
	esRight      = 0x0002
	esMultiline  = 0x0004
	esAutoVScroll = 0x0040
	esAutoHScroll = 0x0080
	esReadOnly   = 0x0800

	bsPushButton    = 0x00000000
	bsDefPushButton = 0x00000001
	bsAutoCheckBox  = 0x00000003
	bsGroupBox      = 0x00000007

	cbsDropDownList = 0x0003
	cbsHasStrings   = 0x0200

	ssLeft = 0x0000

	wmSetFont        = 0x0030
	wmCommand        = 0x0111
	wmClose          = 0x0010
	wmDestroy        = 0x0002
	wmDropFiles      = 0x0233
	wmCtlColorStatic = 0x0138

	emSetSel       = 0x00B1
	emReplaceSel   = 0x00C2
	emScrollCaret  = 0x00B7
	emSetReadOnly  = 0x00CF

	cbAddString    = 0x0143
	cbGetCurSel    = 0x0147
	cbSetCurSel    = 0x014E
	cbResetContent = 0x014B

	bmGetCheck = 0x00F0

	bnClicked   = 0
	cbnSelChange = 1

	mbOK           = 0x00000000
	mbYesNo        = 0x00000004
	mbIconError    = 0x00000010
	mbIconWarning  = 0x00000030
	mbIconInfo     = 0x00000040
	idYes          = 6

	colorBtnFace = 15
	logPixelsX   = 88
	idcArrow     = 32512
	idiApp       = 32512

	// GetSystemMetrics 索引
	smCXIcon   = 11
	smCYIcon   = 12
	smCXSmIcon = 49
	smCYSmIcon = 50

	imageIcon = 1
	lrShared  = 0x8000

	// 窗口样式：不可缩放（固定布局），带标题栏/系统菜单/最小化
	wsOverlappedFixed = 0x00CA0000

	// GetOpenFileName 标志
	ofnNoChangeDir    = 0x00000008
	ofnPathMustExist  = 0x00000800
	ofnFileMustExist  = 0x00001000
	ofnExplorer       = 0x00080000
)

// 控件 ID
const (
	idEditSrc = 101 + iota
	idBtnBrowse
	idLblEnc
	idComboBlock
	idEditDB
	idComboEnc
	idEditTypes
	idChkOpen
	idBtnGo
	idBtnQuit
	idLog
)

// ============================================================ Win32 结构体

type wndClassEx struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     uintptr
	hIcon         uintptr
	hCursor       uintptr
	hbrBackground uintptr
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       uintptr
}

type rect struct{ left, top, right, bottom int32 }

type point struct{ x, y int32 }

type msgT struct {
	hwnd    uintptr
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

type openFileNameW struct {
	lStructSize       uint32
	_                 [4]byte
	hwndOwner         uintptr
	hInstance         uintptr
	lpstrFilter       *uint16
	lpstrCustomFilter *uint16
	nMaxCustFilter    uint32
	nFilterIndex      uint32
	lpstrFile         *uint16
	nMaxFile          uint32
	_                 [4]byte
	lpstrFileTitle    *uint16
	nMaxFileTitle     uint32
	_                 [4]byte
	lpstrInitialDir   *uint16
	lpstrTitle        *uint16
	flags             uint32
	nFileOffset       uint16
	nFileExtension    uint16
	lpstrDefExt       *uint16
	lCustData         uintptr
	lpfnHook          uintptr
	lpTemplateName    *uint16
	pvReserved        uintptr
	dwReserved        uint32
	flagsEx           uint32
}

// ============================================================ 全局状态

var (
	hInst  uintptr
	hMain  uintptr
	hFont  uintptr
	scale  = 1.0
	busy   bool

	hEditSrc    uintptr
	hBtnBrowse  uintptr
	hLblEnc     uintptr
	hComboBlock uintptr
	hEditDB     uintptr
	hComboEnc   uintptr
	hEditTypes  uintptr
	hChkOpen    uintptr
	hBtnGo      uintptr
	hBtnQuit    uintptr
	hLog        uintptr

	curFile   string
	curBlocks []block
)

// u16 把 Go 字符串转成 UTF-16 指针（含结尾 0）
func u16(s string) *uint16 {
	p, err := syscall.UTF16PtrFromString(s)
	if err != nil {
		p, _ = syscall.UTF16PtrFromString("")
	}
	return p
}

// metric 读一个系统度量值（屏幕尺寸 / 图标尺寸等）
func metric(idx int) uintptr {
	v, _, _ := pGetSystemMetrics.Call(uintptr(idx))
	return v
}

// sc 按屏幕 DPI 缩放坐标（设计基准 96 DPI）
func sc(v int) int32 { return int32(float64(v)*scale + 0.5) }

// ============================================================ 控件辅助

func mkCtrl(class, text string, style uint32, x, y, w, h int, id int, parent uintptr) uintptr {
	hw, _, _ := pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(u16(class))),
		uintptr(unsafe.Pointer(u16(text))),
		uintptr(style),
		uintptr(sc(x)), uintptr(sc(y)), uintptr(sc(w)), uintptr(sc(h)),
		parent, uintptr(id), hInst, 0)
	if hFont != 0 {
		pSendMessageW.Call(hw, wmSetFont, hFont, 1)
	}
	return hw
}

func getText(h uintptr) string {
	n, _, _ := pGetWindowTextLenW.Call(h)
	buf := make([]uint16, n+1)
	pGetWindowTextW.Call(h, uintptr(unsafe.Pointer(&buf[0])), uintptr(n+1))
	return syscall.UTF16ToString(buf)
}

func setText(h uintptr, s string) {
	pSetWindowTextW.Call(h, uintptr(unsafe.Pointer(u16(s))))
}

func msgBox(text, caption string, flags uint32) int {
	r, _, _ := pMessageBoxW.Call(hMain,
		uintptr(unsafe.Pointer(u16(text))),
		uintptr(unsafe.Pointer(u16(caption))),
		uintptr(flags))
	return int(r)
}

func comboAdd(h uintptr, s string) {
	pSendMessageW.Call(h, cbAddString, 0, uintptr(unsafe.Pointer(u16(s))))
}

func comboSel(h uintptr) int {
	r, _, _ := pSendMessageW.Call(h, cbGetCurSel, 0, 0)
	return int(int32(uint32(r)))
}

func comboSet(h uintptr, i int) {
	pSendMessageW.Call(h, cbSetCurSel, uintptr(i), 0)
}

// appendLog 往日志框追加一行（只读状态下也能写：临时放开只读）
func appendLog(s string) {
	if hLog == 0 {
		return
	}
	pSendMessageW.Call(hLog, emSetReadOnly, 0, 0)
	pSendMessageW.Call(hLog, emSetSel, ^uintptr(0), ^uintptr(0))
	pSendMessageW.Call(hLog, emReplaceSel, 0, uintptr(unsafe.Pointer(u16(s))))
	pSendMessageW.Call(hLog, emSetReadOnly, 1, 0)
	pSendMessageW.Call(hLog, emScrollCaret, 0, 0)
}

func logln(s string) { appendLog(s + "\r\n") }

// ============================================================ 业务动作

func loadFile(path string) {
	sf, err := loadSource(path)
	if err != nil {
		msgBox("读取失败：\r\n"+err.Error(), "错误", mbOK|mbIconError)
		curFile = ""
		curBlocks = nil
		setText(hEditSrc, "")
		setText(hLblEnc, "")
		pSendMessageW.Call(hComboBlock, cbResetContent, 0, 0)
		return
	}
	curFile = path
	curBlocks = sf.Blocks

	setText(hEditSrc, path)

	names := ""
	for i, b := range sf.Blocks {
		if i > 0 {
			names += "、"
		}
		if b.name == "" {
			names += "(未命名)"
		} else {
			names += b.name
		}
	}
	setText(hLblEnc, fmt.Sprintf("源文件编码：%s      数据块：%d 个（%s）", cpName(sf.CP), len(sf.Blocks), names))

	pSendMessageW.Call(hComboBlock, cbResetContent, 0, 0)
	for _, b := range sf.Blocks {
		nm := b.name
		if nm == "" {
			nm = "(未命名)"
		}
		cnt := blockCount(b)
		desc := fmt.Sprintf("%d 个变量", cnt)
		if cnt < 0 {
			desc = "含无法识别类型"
		}
		comboAdd(hComboBlock, fmt.Sprintf("%s    —  %s", nm, desc))
	}
	comboSet(hComboBlock, 0)
	pEnableWindow.Call(hBtnGo, 1)

	logln("────────────────────────────────────────────────")
	logln("已载入：" + filepath.Base(path))
	logln(fmt.Sprintf("    源文件编码 %s，共 %d 个数据块", cpName(sf.CP), len(sf.Blocks)))
	for i, b := range sf.Blocks {
		nm := b.name
		if nm == "" {
			nm = "(未命名)"
		}
		cnt := blockCount(b)
		if cnt < 0 {
			logln(fmt.Sprintf("    %d) %s    类型无法识别，需在【自定义类型】里补长度", i+1, nm))
		} else {
			logln(fmt.Sprintf("    %d) %s    %d 个变量", i+1, nm, cnt))
		}
	}
}

// handleDrop 处理拖进窗口的文件
func handleDrop(hDrop uintptr) {
	// 第一个参数传 -1 时返回拖入的文件个数
	n, _, _ := pDragQueryFileW.Call(hDrop, ^uintptr(0), 0, 0)
	if n == 0 {
		pDragFinish.Call(hDrop)
		return
	}
	buf := make([]uint16, 1024)
	pDragQueryFileW.Call(hDrop, 0, uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	pDragFinish.Call(hDrop)

	p := syscall.UTF16ToString(buf)
	if p == "" {
		return
	}
	loadFile(p)
	if n > 1 {
		logln(fmt.Sprintf("（拖入了 %d 个文件，只处理了第一个）", n))
	}
}

func onBrowse() {
	buf := make([]uint16, 2048)
	filter := u16("DB 源文件 (*.awl;*.db;*.scl;*.txt)\x00*.awl;*.db;*.scl;*.txt\x00所有文件 (*.*)\x00*.*\x00\x00")
	title := u16("选择西门子 DB 源文件（.awl / .db）")
	defExt := u16("awl")

	ofn := openFileNameW{
		lStructSize:     uint32(unsafe.Sizeof(openFileNameW{})),
		hwndOwner:       hMain,
		lpstrFilter:     filter,
		nFilterIndex:    1,
		lpstrFile:       &buf[0],
		nMaxFile:        uint32(len(buf)),
		lpstrTitle:      title,
		flags:           ofnExplorer | ofnFileMustExist | ofnPathMustExist | ofnNoChangeDir,
		lpstrDefExt:     defExt,
	}
	r, _, _ := pGetOpenFileNameW.Call(uintptr(unsafe.Pointer(&ofn)))
	if r == 0 {
		return
	}
	loadFile(syscall.UTF16ToString(buf))
}

// encFromCombo 把下拉框序号翻译成输出编码参数
func encFromCombo() string {
	switch comboSel(hComboEnc) {
	case 1:
		return "utf8"
	case 2:
		return "ansi"
	}
	return "utf8bom"
}

func onConvert() {
	if busy {
		return
	}

	// 允许直接把路径粘进"源文件"框里，不必走浏览对话框
	typed := strings.Trim(strings.TrimSpace(getText(hEditSrc)), "\"")
	if typed != "" && typed != curFile {
		loadFile(typed)
	}
	if curFile == "" {
		msgBox("请先点【浏览...】选择 .awl / .db 源文件，\r\n或直接把文件路径粘贴到【源文件】框里。", "提示", mbOK|mbIconInfo)
		return
	}

	dbNum, err := strconv.Atoi(strings.TrimSpace(getText(hEditDB)))
	if err != nil || dbNum < 1 || dbNum > 65535 {
		msgBox("DB 号必须是 1 - 65535 之间的整数。", "提示", mbOK|mbIconWarning)
		return
	}

	idx := comboSel(hComboBlock)
	if idx < 0 || idx >= len(curBlocks) {
		msgBox("请先选择要转换的数据块。", "提示", mbOK|mbIconWarning)
		return
	}
	blk := curBlocks[idx]

	// 自定义类型：每次重新解析，避免上一次的残留定义干扰
	userTypes = map[string]typeSpec{}
	if bad := parseUserTypes(getText(hEditTypes)); len(bad) > 0 {
		msgBox("以下自定义类型定义看不懂，已忽略：\r\n"+strings.Join(bad, "\r\n")+
			"\r\n\r\n正确写法：类型名=字节数   例：MyUDT=24", "提示", mbOK|mbIconWarning)
	}

	// 优化块警告
	if blk.optimized {
		if msgBox("该数据块声明了 S7_Optimized_Access := 'TRUE'（优化块）。\r\n\r\n"+
			"优化块在 PLC 里没有固定绝对偏移，按经典块算出来的地址是无效的。\r\n"+
			"优化块请改用符号寻址（KEPServerEX 的 S7 Plus 驱动）。\r\n\r\n仍要生成吗？",
			"警告", mbYesNo|mbIconWarning) != idYes {
			logln("已取消（优化块）。")
			return
		}
	}

	enc := encFromCombo()
	busy = true
	pEnableWindow.Call(hBtnGo, 0)

	res, cerr := convertBlock(&srcFile{Path: curFile, Blocks: curBlocks}, blk, dbNum, enc, "")
	if cerr != nil {
		busy = false
		pEnableWindow.Call(hBtnGo, 1)
		txt := cerr.Error()
		if _, ok := cerr.(*resolveErr); ok {
			txt += "\r\n\r\n这个类型要占多少字节无法确定，后面所有变量的偏移都会错位，所以已经中止。\r\n" +
				"如果是自定义 UDT，请在【自定义类型】里填上它的字节数再试。"
		}
		logln("× 转换失败：" + cerr.Error())
		msgBox(txt, "转换失败", mbOK|mbIconError)
		return
	}

	nm := res.BlockName
	if nm == "" {
		nm = "(未命名)"
	}
	logln("────────────────────────────────────────────────")
	logln(fmt.Sprintf("√ 数据块 %s  →  DB%d", nm, dbNum))
	logln(fmt.Sprintf("    变量 %d 个（Bool %d 个），推算块长度 %d 字节", res.NScalar, res.NBool, res.Total))
	if len(res.Reserved) > 0 {
		logln(fmt.Sprintf("    另有 %d 个结构体类型只占位、不生成标签：", len(res.Reserved)))
		for _, it := range res.Reserved {
			logln(fmt.Sprintf("        %s : %s（%d 字节，偏移 %d）", it.name, it.rawType, it.spec.size, it.offset))
		}
	}
	if len(res.BadNames) > 0 {
		logln(fmt.Sprintf("    ！有 %d 个标签名不符合 KEPServerEX 命名规则，导入可能被拒：", len(res.BadNames)))
		for _, n := range res.BadNames {
			logln("        " + n)
		}
		logln("        （不允许：英文句点 . 、双引号、开头的下划线 _ 、首尾空格）")
	}
	shown := 0
	for _, it := range res.Items {
		if it.spec.kind != kScalar || shown >= 3 {
			continue
		}
		logln(fmt.Sprintf("        %s  ->  %s", it.name, addressOf(it, dbNum)))
		shown++
	}
	if res.NScalar > shown {
		logln("        ...")
	}
	logln(fmt.Sprintf("    输出文件：%s", res.OutPath))
	logln(fmt.Sprintf("    共 %d 条标签，编码 %s", res.NScalar, strings.ToLower(enc)))

	busy = false
	pEnableWindow.Call(hBtnGo, 1)

	chk, _, _ := pSendMessageW.Call(hChkOpen, bmGetCheck, 0, 0)
	if chk&0xFFFF != 0 {
		dir := filepath.Dir(res.OutPath)
		pShellExecuteW.Call(0,
			uintptr(unsafe.Pointer(u16("open"))),
			uintptr(unsafe.Pointer(u16(dir))),
			0, 0, swShow)
	}
}

// ============================================================ 窗口过程

func wndProc(hwnd, msg, wParam, lParam uintptr) (ret uintptr) {
	// 兜底：任何意外都不让窗口无声消失
	defer func() {
		if r := recover(); r != nil {
			ret = 0
			msgBox(fmt.Sprintf("程序内部出错：%v\n\n请把上面的操作步骤反馈给开发者。", r),
				"错误", mbOK|mbIconError)
		}
	}()

	switch msg {
	case wmCommand:
		id := int(wParam & 0xFFFF)
		code := int((wParam >> 16) & 0xFFFF)
		switch id {
		case idBtnBrowse:
			if code == bnClicked {
				onBrowse()
			}
		case idBtnGo:
			if code == bnClicked {
				onConvert()
			}
		case idBtnQuit:
			if code == bnClicked {
				pDestroyWindow.Call(hwnd)
			}
		}
		return 0

	case wmCtlColorStatic:
		br, _, _ := pGetSysColorBrush.Call(colorBtnFace)
		return br

	case wmDropFiles:
		handleDrop(wParam)
		return 0

	case wmClose:
		pDestroyWindow.Call(hwnd)
		return 0

	case wmDestroy:
		pPostQuitMessage.Call(0)
		return 0
	}
	r, _, _ := pDefWindowProcW.Call(hwnd, msg, wParam, lParam)
	return r
}

var wndProcCB = syscall.NewCallback(wndProc)

// ============================================================ 界面搭建

func buildUI() {
	// ---- 第 1 组：源文件 ----
	mkCtrl("BUTTON", " 1. 源文件 ", wsChild|wsVisible|bsGroupBox, 12, 10, 696, 78, 0, hMain)
	mkCtrl("STATIC", "源文件：", wsChild|wsVisible|ssLeft, 26, 37, 58, 18, 0, hMain)
	hEditSrc = mkCtrl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll,
		88, 33, 500, 24, idEditSrc, hMain)
	hBtnBrowse = mkCtrl("BUTTON", "浏览...", wsChild|wsVisible|wsTabStop|bsPushButton,
		598, 32, 98, 26, idBtnBrowse, hMain)
	hLblEnc = mkCtrl("STATIC", "", wsChild|wsVisible|ssLeft, 88, 61, 600, 18, idLblEnc, hMain)

	// ---- 第 2 组：数据块 ----
	mkCtrl("BUTTON", " 2. 数据块与 DB 号 ", wsChild|wsVisible|bsGroupBox, 12, 96, 696, 78, 0, hMain)
	mkCtrl("STATIC", "数据块：", wsChild|wsVisible|ssLeft, 26, 123, 58, 18, 0, hMain)
	hComboBlock = mkCtrl("COMBOBOX", "", wsChild|wsVisible|wsTabStop|wsVScroll|cbsDropDownList|cbsHasStrings,
		88, 120, 420, 220, idComboBlock, hMain)
	mkCtrl("STATIC", "DB 号：", wsChild|wsVisible|ssLeft, 524, 123, 56, 18, 0, hMain)
	hEditDB = mkCtrl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll|esRight,
		586, 120, 110, 24, idEditDB, hMain)
	setText(hEditDB, "10")

	// ---- 第 3 组：输出选项 ----
	mkCtrl("BUTTON", " 3. 输出选项 ", wsChild|wsVisible|bsGroupBox, 12, 182, 696, 88, 0, hMain)
	mkCtrl("STATIC", "输出编码：", wsChild|wsVisible|ssLeft, 26, 209, 66, 18, 0, hMain)
	hComboEnc = mkCtrl("COMBOBOX", "", wsChild|wsVisible|wsTabStop|wsVScroll|cbsDropDownList|cbsHasStrings,
		94, 206, 150, 160, idComboEnc, hMain)
	comboAdd(hComboEnc, "UTF-8 带 BOM（推荐）")
	comboAdd(hComboEnc, "UTF-8 不带 BOM")
	comboAdd(hComboEnc, "ANSI / GBK（导入乱码时用）")
	comboSet(hComboEnc, 0)

	mkCtrl("STATIC", "自定义类型：", wsChild|wsVisible|ssLeft, 258, 209, 82, 18, 0, hMain)
	hEditTypes = mkCtrl("EDIT", "", wsChild|wsVisible|wsBorder|wsTabStop|esAutoHScroll,
		344, 206, 352, 24, idEditTypes, hMain)
	mkCtrl("STATIC", "例：MyUDT=24（类型名=字节数，多个用逗号隔开）", wsChild|wsVisible|ssLeft,
		344, 234, 356, 16, 0, hMain)
	hChkOpen = mkCtrl("BUTTON", "转换后打开输出目录", wsChild|wsVisible|wsTabStop|bsAutoCheckBox,
		94, 231, 210, 22, idChkOpen, hMain)

	// ---- 操作按钮 ----
	hBtnGo = mkCtrl("BUTTON", "开始转换", wsChild|wsVisible|wsTabStop|bsDefPushButton,
		490, 282, 110, 32, idBtnGo, hMain)
	hBtnQuit = mkCtrl("BUTTON", "退出", wsChild|wsVisible|wsTabStop|bsPushButton,
		608, 282, 88, 32, idBtnQuit, hMain)

	// ---- 日志框 ----
	hLog = mkCtrl("EDIT", "", wsChild|wsVisible|wsBorder|wsVScroll|esMultiline|esAutoVScroll|esReadOnly,
		12, 326, 696, 302, idLog, hMain)

	logln("西门子 DB 源文件（.awl / .db）  →  KEPServerEX 点表")
	logln("按 S7-300/400 经典（非优化）DB 布局自动推算偏移。")
	logln("")
	logln("使用步骤：")
	logln("    1. 选源文件 —— 点【浏览...】，或把 .awl / .db 文件直接拖进这个窗口，")
	logln("       或把文件路径粘贴到上面的【源文件】框里")
	logln("    2. 选数据块，填好这个块的 DB 号")
	logln("    3. 点【开始转换】")
	logln("")
	logln("CSV 会生成在源文件所在目录，导入方法：")
	logln("    KEPServerEX 里选中目标 Device  ->  File  ->  Import CSV")
	logln("")
	logln("提示：TIA 里导出源文件时请选 .awl / .db（源文件），不要用 .scl。")
	logln("      中文注释靠源文件本身携带，源文件缺注释则点表 Description 为空。")
	logln("")
}

func main() {
	// 按屏幕 DPI 缩放，保证 125% / 150% 缩放下界面不变形
	if hdc, _, _ := pGetDC.Call(0); hdc != 0 {
		dpi, _, _ := pGetDeviceCaps.Call(hdc, logPixelsX)
		pReleaseDC.Call(0, hdc)
		if dpi >= 96 && dpi <= 480 {
			scale = float64(dpi) / 96.0
		}
	}

	hInst, _, _ = pGetModuleHandleW.Call(0)

	// 字体：优先微软雅黑 UI，退回 Segoe UI
	face := "Microsoft YaHei UI"
	hFont, _, _ = pCreateFontW.Call(
		uintptr(int32(-sc(13))), 0, 0, 0, 400 /*FW_NORMAL*/, 0, 0, 0,
		1 /*DEFAULT_CHARSET*/, 0, 0, 5 /*CLEARTYPE_QUALITY*/, 0,
		uintptr(unsafe.Pointer(u16(face))))
	if hFont == 0 {
		face = "Segoe UI"
		hFont, _, _ = pCreateFontW.Call(
			uintptr(int32(-sc(13))), 0, 0, 0, 400, 0, 0, 0,
			1, 0, 0, 5, 0, uintptr(unsafe.Pointer(u16(face))))
	}

	cursor, _, _ := pLoadCursorW.Call(0, idcArrow)

	// 图标资源由 app.syso 提供（图标组 ID = 1）；取大/小两档保证清晰
	icon, _, _ := pLoadImageW.Call(hInst, 1, imageIcon,
		metric(smCXIcon), metric(smCYIcon), lrShared)
	iconSm, _, _ := pLoadImageW.Call(hInst, 1, imageIcon,
		metric(smCXSmIcon), metric(smCYSmIcon), lrShared)
	if icon == 0 {
		icon, _, _ = pLoadIconW.Call(0, idiApp)
	}
	if iconSm == 0 {
		iconSm = icon
	}

	className := u16("DB2KepServerEXWndClass")
	wc := wndClassEx{
		cbSize:        uint32(unsafe.Sizeof(wndClassEx{})),
		lpfnWndProc:   wndProcCB,
		hInstance:     hInst,
		hIcon:         icon,
		hCursor:       cursor,
		hbrBackground: colorBtnFace + 1,
		lpszClassName: className,
		hIconSm:       iconSm,
	}
	if r, _, _ := pRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); r == 0 {
		msgBox("窗口类注册失败。", "错误", mbOK|mbIconError)
		os.Exit(1)
	}

	// 客户区 720 x 640
	r := rect{0, 0, sc(720), sc(640)}
	pAdjustWindowRect.Call(uintptr(unsafe.Pointer(&r)), wsOverlappedFixed, 0)
	winW := int(r.right - r.left)
	winH := int(r.bottom - r.top)

	title := "西门子 DB 源文件 → KEPServerEX 点表 转换器"
	hMain, _, _ = pCreateWindowExW.Call(0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(u16(title))),
		wsOverlappedFixed,
		uintptr(cwUseDefault), uintptr(cwUseDefault),
		uintptr(winW), uintptr(winH),
		0, 0, hInst, 0)
	if hMain == 0 {
		msgBox("窗口创建失败。", "错误", mbOK|mbIconError)
		os.Exit(1)
	}

	buildUI()

	// 允许把 .awl / .db 文件直接拖进窗口
	pDragAcceptFiles.Call(hMain, 1)

	// 支持把 .awl / .db 文件直接拖到 exe 图标上启动：
	//   第一个参数当源文件，第二个参数当 DB 号
	if len(os.Args) > 1 {
		p := strings.Trim(strings.TrimSpace(os.Args[1]), "\"")
		if st, err := os.Stat(p); err == nil && !st.IsDir() {
			loadFile(p)
		}
		if len(os.Args) > 2 {
			if n, err := strconv.Atoi(strings.TrimSpace(os.Args[2])); err == nil && n >= 1 && n <= 65535 {
				setText(hEditDB, strconv.Itoa(n))
			}
		}
	}

	pShowWindow.Call(hMain, swShow)
	pUpdateWindow.Call(hMain)

	var m msgT
	for {
		gr, _, _ := pGetMessageW.Call(uintptr(unsafe.Pointer(&m)), 0, 0, 0)
		if int32(gr) <= 0 {
			break
		}
		pTranslateMessage.Call(uintptr(unsafe.Pointer(&m)))
		pDispatchMessageW.Call(uintptr(unsafe.Pointer(&m)))
	}
}
