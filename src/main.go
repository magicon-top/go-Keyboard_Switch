package main

import (
	"fmt"
	"os"
	"runtime"
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/registry"
)

const (
	WH_KEYBOARD_LL = 13

	WM_KEYDOWN    = 0x0100
	WM_KEYUP      = 0x0101
	WM_SYSKEYDOWN = 0x0104
	WM_SYSKEYUP   = 0x0105
	WM_SETCURSOR  = 0x0020
	WM_MOUSEMOVE  = 0x0200
	WM_MOUSELEAVE = 0x02A2

	VK_LCONTROL = 0xA2
	VK_RCONTROL = 0xA3
	VK_CONTROL  = 0x11

	VK_LMENU = 0xA4
	VK_RMENU = 0xA5
	VK_MENU  = 0x12

	VK_LSHIFT = 0xA0
	VK_RSHIFT = 0xA1
	VK_SHIFT  = 0x10

	WM_INPUTLANGCHANGEREQUEST = 0x0050
	INPUTLANGCHANGE_FORWARD   = 0x0002

	WS_POPUP         = 0x80000000
	WS_EX_TOPMOST    = 0x00000008
	WS_EX_LAYERED    = 0x00080000
	WS_EX_TOOLWINDOW = 0x00000080
	WS_EX_NOACTIVATE = 0x08000000

	CS_DBLCLKS = 0x0008

	SM_CXSCREEN        = 0
	SM_CYSCREEN        = 1
	SM_XVIRTUALSCREEN  = 76
	SM_YVIRTUALSCREEN  = 77
	SM_CXVIRTUALSCREEN = 78
	SM_CYVIRTUALSCREEN = 79

	WM_PAINT           = 0x000F
	WM_ERASEBKGND      = 0x0014
	WM_NCHITTEST       = 0x0084
	WM_LBUTTONDBLCLK   = 0x0203
	WM_RBUTTONDOWN     = 0x0204
	WM_RBUTTONUP       = 0x0205
	WM_NCLBUTTONDBLCLK = 0x00A3
	WM_NCRBUTTONDOWN   = 0x00A4
	WM_NCRBUTTONUP     = 0x00A5
	WM_APP_REDRAW      = 0x8001

	HTCLIENT = 1

	ERROR_ALREADY_EXISTS = 183

	IDC_ARROW = 32512

	LOCALE_SLANGDISPLAYNAME = 0x0000006D
	TRANSPARENT             = 1
	TME_LEAVE               = 0x00000002
	DT_LEFT                 = 0x00000000
	DT_TOP                  = 0x00000000
	DT_SINGLELINE           = 0x00000020
	DT_NOCLIP               = 0x00000100
	NONANTIALIASED_QUALITY  = 3
)

var HTTRANSPARENT = ^uintptr(0)

type KBDLLHOOKSTRUCT struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

type MSG struct {
	Hwnd    uintptr
	Message uint32
	Wparam  uintptr
	Lparam  uintptr
	Time    uint32
	Pt      POINT
}

type POINT struct {
	X, Y int32
}

type RECT struct {
	Left, Top, Right, Bottom int32
}

type PAINTSTRUCT struct {
	Hdc         uintptr
	Erase       bool
	RcPaint     RECT
	Restore     bool
	IncUpdate   bool
	RgbReserved [32]byte
}

type WNDCLASSEX struct {
	Size       uint32
	Style      uint32
	WndProc    uintptr
	ClsExtra   int32
	WndExtra   int32
	Instance   uintptr
	Icon       uintptr
	Cursor     uintptr
	Background uintptr
	MenuName   *uint16
	ClassName  *uint16
	IconSm     uintptr
}

type GUITHREADINFO struct {
	CbSize        uint32
	Flags         uint32
	HwndActive    uintptr
	HwndFocus     uintptr
	HwndCapture   uintptr
	HwndMenuOwner uintptr
	HwndMoveSize  uintptr
	HwndCaret     uintptr
	RcCaret       RECT
}

type TRACKMOUSEEVENT struct {
	CbSize      uint32
	DwFlags     uint32
	HwndTrack   uintptr
	DwHoverTime uint32
}

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procSetProcessDPIAware         = user32.NewProc("SetProcessDPIAware")
	procSetWindowsHookEx           = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx        = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx             = user32.NewProc("CallNextHookEx")
	procGetMessage                 = user32.NewProc("GetMessageW")
	procDispatchMessage            = user32.NewProc("DispatchMessageW")
	procGetForegroundWindow        = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId   = user32.NewProc("GetWindowThreadProcessId")
	procGetKeyboardLayout          = user32.NewProc("GetKeyboardLayout")
	procPostMessage                = user32.NewProc("PostMessageW")
	procGetKeyboardLayoutList      = user32.NewProc("GetKeyboardLayoutList")
	procGetSystemMetrics           = user32.NewProc("GetSystemMetrics")
	procCreateWindowEx             = user32.NewProc("CreateWindowExW")
	procRegisterClassEx            = user32.NewProc("RegisterClassExW")
	procShowWindow                 = user32.NewProc("ShowWindow")
	procSetLayeredWindowAttributes = user32.NewProc("SetLayeredWindowAttributes")
	procBeginPaint                 = user32.NewProc("BeginPaint")
	procEndPaint                   = user32.NewProc("EndPaint")
	procInvalidateRect             = user32.NewProc("InvalidateRect")
	procPostQuitMessage            = user32.NewProc("PostQuitMessage")
	procFillRect                   = user32.NewProc("FillRect")
	procLoadCursor                 = user32.NewProc("LoadCursorW")
	procGetGUIThreadInfo           = user32.NewProc("GetGUIThreadInfo")
	procTrackMouseEvent            = user32.NewProc("TrackMouseEvent")
	procDrawText                   = user32.NewProc("DrawTextW")
	procGetCursorPos               = user32.NewProc("GetCursorPos")
	procScreenToClient             = user32.NewProc("ScreenToClient")

	procBeep          = kernel32.NewProc("Beep")
	procCreateMutex   = kernel32.NewProc("CreateMutexW")
	procGetLocaleInfo = kernel32.NewProc("GetLocaleInfoW")

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")
	procSetBkMode        = gdi32.NewProc("SetBkMode")
	procSetTextColor     = gdi32.NewProc("SetTextColor")
	procCreateFont       = gdi32.NewProc("CreateFontW")
	procSelectObject     = gdi32.NewProc("SelectObject")

	hookHandle  uintptr
	overlayHwnd uintptr

	stateMu            sync.RWMutex
	currentBorderColor uint32 = 0
	hasBorder          bool   = false
	soundEnabled       bool   = true
	lastHKL            uintptr
	isBorderHovered    bool = false
	hoverCount         int  = 0
	cursorPt           POINT

	pressedKey     uint32
	pressTime      time.Time
	isComboPressed bool
	maxTapDuration = 400 * time.Millisecond

	borderThickness = int32(4)

	typingSoundChan = make(chan uint32, 50)

	layoutsMu     sync.RWMutex
	cachedLayouts []uintptr

	keyColors = []uint32{
		0x000000FF, // Ctrl - Red
		0x00C0C0C0, // Shift - Light Gray
		0x0000FFFF, // Alt - Yellow
	}
)

const (
	mutexName     = "Global\\LangSwitcherAppMutex"
	crTransparent = 0x00FF00FF
	appName       = "LangSwitcherApp"
)

//________________________________________________________
//Checks Windows registry for current executable path and adds missing autorun key. ensureAutoStart
func ensureAutoStart() {
	exePath, err := os.Executable()
	if err != nil {
		return
	}

	key, err := registry.OpenKey(registry.CURRENT_USER, `SOFTWARE\Microsoft\Windows\CurrentVersion\Run`, registry.QUERY_VALUE|registry.SET_VALUE)
	if err != nil {
		return
	}
	defer key.Close()

	val, _, err := key.GetStringValue(appName)
	if err != nil || val != exePath {
		_ = key.SetStringValue(appName, exePath)
	}
}

//________________________________________________________
//Initializes a goroutine for playing sound effects. initSoundWorker
func initSoundWorker() {
	go func() {
		for freq := range typingSoundChan {
			if freq > 0 {
				procBeep.Call(uintptr(freq), 20)
			}
		}
	}()
}

//________________________________________________________
//Retrieves active system keyboard layouts from cache. getKeyboardLayouts
func getKeyboardLayouts() []uintptr {
	layoutsMu.RLock()
	if len(cachedLayouts) > 0 {
		res := cachedLayouts
		layoutsMu.RUnlock()
		return res
	}
	layoutsMu.RUnlock()
	return refreshKeyboardLayouts()
}

//________________________________________________________
//Refreshes cached list of system keyboard layouts. refreshKeyboardLayouts
func refreshKeyboardLayouts() []uintptr {
	count, _, _ := procGetKeyboardLayoutList.Call(0, 0)
	if count == 0 {
		return nil
	}
	layouts := make([]uintptr, count)
	procGetKeyboardLayoutList.Call(count, uintptr(unsafe.Pointer(&layouts[0])))
	layoutsMu.Lock()
	cachedLayouts = layouts
	layoutsMu.Unlock()
	return layouts
}

//________________________________________________________
//Retrieves display name of language in uppercase for given layout handle. getLayoutName
func getLayoutName(hkl uintptr) string {
	lcid := uint32(uint16(hkl))
	if lcid == 0 {
		return "UNKNOWN"
	}
	buf := make([]uint16, 64)
	ret, _, _ := procGetLocaleInfo.Call(uintptr(lcid), uintptr(LOCALE_SLANGDISPLAYNAME), uintptr(unsafe.Pointer(&buf[0])), uintptr(len(buf)))
	if ret == 0 {
		return "UNKNOWN"
	}
	return strings.ToUpper(syscall.UTF16ToString(buf))
}

//________________________________________________________
//Main application loop initializing window and hooks. main
func main() {
	runtime.LockOSThread()
	procSetProcessDPIAware.Call()
	mName, _ := syscall.UTF16PtrFromString(mutexName)
	hMutex, _, errMutex := procCreateMutex.Call(0, 1, uintptr(unsafe.Pointer(mName)))
	if hMutex == 0 || (errMutex != nil && errMutex == windows.ERROR_ALREADY_EXISTS) {
		fmt.Println("Application already running")
		return
	}
	ensureAutoStart()
	refreshKeyboardLayouts()
	initSoundWorker()
	fmt.Println("Started successfully")
	createOverlayWindow()
	go trackLanguageChanges()
	go trackMouseHoverLoop()
	hook, _, _ := procSetWindowsHookEx.Call(uintptr(WH_KEYBOARD_LL), syscall.NewCallback(keyboardProc), 0, 0)
	if hook == 0 {
		fmt.Println("Failed to install keyboard hook")
		return
	}
	hookHandle = hook
	defer procUnhookWindowsHookEx.Call(hookHandle)
	var msg MSG
	for {
		ret, _, _ := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 || int32(ret) == -1 {
			break
		}
		procDispatchMessage.Call(uintptr(unsafe.Pointer(&msg)))
	}
}

//________________________________________________________
//Polls cursor position and triggers redrawing strictly when position or hover state changes. trackMouseHoverLoop
func trackMouseHoverLoop() {
	ticker := time.NewTicker(30 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		if overlayHwnd == 0 {
			continue
		}

		var pt POINT
		procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))
		procScreenToClient.Call(overlayHwnd, uintptr(unsafe.Pointer(&pt)))

		vw, _, _ := procGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
		vh, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
		if vw == 0 || vh == 0 {
			w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
			h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
			vw = w
			vh = h
		}
		width := int32(vw)
		height := int32(vh)

		isBorder := pt.X >= 0 && pt.Y >= 0 && pt.X < width && pt.Y < height &&
			(pt.X < borderThickness || pt.X >= width-borderThickness || pt.Y < borderThickness || pt.Y >= height-borderThickness)

		stateMu.Lock()
		hoverChanged := (isBorderHovered != isBorder)
		if hoverChanged {
			isBorderHovered = isBorder
			if isBorder {
				hoverCount++
				cursorPt = pt
			}
		}
		stateMu.Unlock()

		if hoverChanged {
			procPostMessage.Call(overlayHwnd, WM_APP_REDRAW, 0, 0)
		}
	}
}

//________________________________________________________
//Monitors active window layout changes periodically. trackLanguageChanges
func trackLanguageChanges() {
	ticker := time.NewTicker(100 * time.Millisecond)
	defer ticker.Stop()
	for range ticker.C {
		hwnd, _, _ := procGetForegroundWindow.Call()
		if hwnd == 0 {
			continue
		}
		threadID, _, _ := procGetWindowThreadProcessId.Call(hwnd, 0)
		if threadID == 0 {
			continue
		}
		hkl, _, _ := procGetKeyboardLayout.Call(threadID)
		if hkl == 0 {
			continue
		}
		stateMu.RLock()
		prevHKL := lastHKL
		stateMu.RUnlock()
		if hkl != prevHKL {
			stateMu.Lock()
			lastHKL = hkl
			stateMu.Unlock()
			updateBorderForHKL(hkl)
		}
	}
}

//________________________________________________________
//Updates overlay color based on selected layout index. updateBorderForHKL
func updateBorderForHKL(hkl uintptr) {
	layouts := getKeyboardLayouts()
	if len(layouts) == 0 {
		return
	}
	index := -1
	for i, l := range layouts {
		if l == hkl {
			index = i
			break
		}
	}
	if index == -1 {
		layouts = refreshKeyboardLayouts()
		for i, l := range layouts {
			if l == hkl {
				index = i
				break
			}
		}
		if index == -1 {
			return
		}
	}
	stateMu.Lock()
	switch index {
	case 0:
		hasBorder = true
		currentBorderColor = keyColors[0]
	case 1:
		hasBorder = false
	case 2:
		hasBorder = true
		currentBorderColor = keyColors[2]
	default:
		hasBorder = false
	}
	stateMu.Unlock()
	if overlayHwnd != 0 {
		procPostMessage.Call(overlayHwnd, WM_APP_REDRAW, 0, 0)
	}
}

//________________________________________________________
//Processes keyboard low level hooks for shortcut events. keyboardProc
func keyboardProc(nCode int, wParam uintptr, lParam uintptr) uintptr {
	if nCode >= 0 {
		kbd := (*KBDLLHOOKSTRUCT)(unsafe.Pointer(lParam))
		vk := kbd.VkCode
		shouldPlaySound := false
		stateMu.Lock()
		if wParam == WM_KEYDOWN || wParam == WM_SYSKEYDOWN {
			if isTargetKey(vk) {
				if pressedKey != 0 && pressedKey != vk {
					isComboPressed = true
				} else if pressedKey == 0 {
					pressedKey = vk
					pressTime = time.Now()
					isComboPressed = false
				}
			} else {
				if pressedKey != 0 {
					isComboPressed = true
				}
				shouldPlaySound = true
			}
		} else if wParam == WM_KEYUP || wParam == WM_SYSKEYUP {
			if isTargetKey(vk) && isBaseKeyMatch(pressedKey, vk) {
				if !isComboPressed && time.Since(pressTime) <= maxTapDuration {
					switch {
					case isCtrl(vk):
						go applyLanguage(0, 400, keyColors[0])
					case isShift(vk):
						go applyLanguage(1, 800, 0)
					case isAlt(vk):
						go applyLanguage(2, 1200, keyColors[2])
					}
				}
				pressedKey = 0
				isComboPressed = false
			}
		}
		stateMu.Unlock()
		if shouldPlaySound {
			playTypingSound()
		}
	}
	ret, _, _ := procCallNextHookEx.Call(hookHandle, uintptr(nCode), wParam, lParam)
	return ret
}

//________________________________________________________
//Evaluates if key belongs to active target modifiers. isTargetKey
func isTargetKey(vk uint32) bool {
	return isCtrl(vk) || isShift(vk) || isAlt(vk)
}

//________________________________________________________
//Checks for Control virtual key codes. isCtrl
func isCtrl(vk uint32) bool {
	return vk == VK_LCONTROL || vk == VK_RCONTROL || vk == VK_CONTROL
}

//________________________________________________________
//Checks for Shift virtual key codes. isShift
func isShift(vk uint32) bool {
	return vk == VK_LSHIFT || vk == VK_RSHIFT || vk == VK_SHIFT
}

//________________________________________________________
//Checks for Alt virtual key codes. isAlt
func isAlt(vk uint32) bool {
	return vk == VK_LMENU || vk == VK_RMENU || vk == VK_MENU
}

//________________________________________________________
//Verifies if keyup corresponds to initial keydown event. isBaseKeyMatch
func isBaseKeyMatch(k1, k2 uint32) bool {
	return (isCtrl(k1) && isCtrl(k2)) || (isShift(k1) && isShift(k2)) || (isAlt(k1) && isAlt(k2))
}

//________________________________________________________
//Dispatches typing sound frequency to worker channel. playTypingSound
func playTypingSound() {
	stateMu.RLock()
	enabled := soundEnabled
	currentHKL := lastHKL
	stateMu.RUnlock()
	if !enabled || currentHKL == 0 {
		return
	}
	layouts := getKeyboardLayouts()
	if len(layouts) == 0 {
		return
	}
	currentIndex := 0
	for i, l := range layouts {
		if l == currentHKL {
			currentIndex = i
			break
		}
	}
	var freq uint32
	switch currentIndex {
	case 0:
		freq = 400
	case 1:
		freq = 0
	default:
		freq = 1200
	}
	if freq > 0 {
		select {
		case typingSoundChan <- freq:
		default:
		}
	}
}

//________________________________________________________
//Sends layout switch message to active window and focused control. applyLanguage
func applyLanguage(index int, freq uint32, colorRGB uint32) {
	layouts := getKeyboardLayouts()
	if len(layouts) == 0 {
		return
	}
	if index >= len(layouts) {
		index = len(layouts) - 1
	}
	targetHKL := layouts[index]
	hwnd, _, _ := procGetForegroundWindow.Call()
	if hwnd != 0 {
		procPostMessage.Call(hwnd, WM_INPUTLANGCHANGEREQUEST, INPUTLANGCHANGE_FORWARD, targetHKL)
		threadID, _, _ := procGetWindowThreadProcessId.Call(hwnd, 0)
		var gti GUITHREADINFO
		gti.CbSize = uint32(unsafe.Sizeof(gti))
		ret, _, _ := procGetGUIThreadInfo.Call(threadID, uintptr(unsafe.Pointer(&gti)))
		if ret != 0 && gti.HwndFocus != 0 && gti.HwndFocus != hwnd {
			procPostMessage.Call(gti.HwndFocus, WM_INPUTLANGCHANGEREQUEST, INPUTLANGCHANGE_FORWARD, targetHKL)
		}
	}
	if freq > 0 {
		procBeep.Call(uintptr(freq), 100)
	}
	stateMu.Lock()
	lastHKL = targetHKL
	if colorRGB == 0 {
		hasBorder = false
	} else {
		hasBorder = true
		currentBorderColor = colorRGB
	}
	stateMu.Unlock()
	if overlayHwnd != 0 {
		procPostMessage.Call(overlayHwnd, WM_APP_REDRAW, 0, 0)
	}
}

//________________________________________________________
//Processes window messages for overlay rendering and clicks. windowProc
func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_ERASEBKGND:
		return 1
	case WM_MOUSEMOVE:
		return 0
	case WM_MOUSELEAVE:
		stateMu.Lock()
		isBorderHovered = false
		stateMu.Unlock()
		procPostMessage.Call(hwnd, WM_APP_REDRAW, 0, 0)
	case WM_NCHITTEST:
		stateMu.RLock()
		activeBorder := hasBorder
		stateMu.RUnlock()
		if !activeBorder {
			return HTTRANSPARENT
		}
		x := int32(int16(lParam & 0xFFFF))
		y := int32(int16((lParam >> 16) & 0xFFFF))
		vx, _, _ := procGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
		vy, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
		vw, _, _ := procGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
		vh, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
		if vw == 0 || vh == 0 {
			w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
			h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
			vw = w
			vh = h
			vx = 0
			vy = 0
		}
		vLeft := int32(vx)
		vTop := int32(vy)
		vRight := vLeft + int32(vw)
		vBottom := vTop + int32(vh)
		if x < vLeft+borderThickness || x >= vRight-borderThickness || y < vTop+borderThickness || y >= vBottom-borderThickness {
			return HTCLIENT
		}
		return HTTRANSPARENT
	case WM_RBUTTONDOWN, WM_NCRBUTTONDOWN:
		return 0
	case WM_RBUTTONUP, WM_NCRBUTTONUP:
		stateMu.Lock()
		soundEnabled = !soundEnabled
		enabled := soundEnabled
		stateMu.Unlock()
		if enabled {
			procBeep.Call(1000, 100)
		} else {
			procBeep.Call(300, 100)
		}
		return 0
	case WM_LBUTTONDBLCLK, WM_NCLBUTTONDBLCLK:
		procBeep.Call(300, 150)
		procPostQuitMessage.Call(0)
		return 0
	case WM_APP_REDRAW:
		procInvalidateRect.Call(hwnd, 0, 0)
		return 0
	case WM_PAINT:
		var ps PAINTSTRUCT
		hdc, _, _ := procBeginPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		vw, _, _ := procGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
		vh, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
		if vw == 0 || vh == 0 {
			w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
			h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
			vw = w
			vh = h
		}
		width := int32(vw)
		height := int32(vh)

		bgBrush, _, _ := procCreateSolidBrush.Call(crTransparent)
		fullRect := RECT{0, 0, width, height}
		procFillRect.Call(hdc, uintptr(unsafe.Pointer(&fullRect)), bgBrush)
		procDeleteObject.Call(bgBrush)

		stateMu.RLock()
		activeBorder := hasBorder
		borderColor := currentBorderColor
		hovered := isBorderHovered
		currentHoverCount := hoverCount
		curPos := cursorPt
		stateMu.RUnlock()

		if activeBorder {
			borderBrush, _, _ := procCreateSolidBrush.Call(uintptr(borderColor))
			top := RECT{0, 0, width, borderThickness}
			bottom := RECT{0, height - borderThickness, width, height}
			left := RECT{0, borderThickness, borderThickness, height - borderThickness}
			right := RECT{width - borderThickness, borderThickness, width, height - borderThickness}
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&top)), borderBrush)
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&bottom)), borderBrush)
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&left)), borderBrush)
			procFillRect.Call(hdc, uintptr(unsafe.Pointer(&right)), borderBrush)
			procDeleteObject.Call(borderBrush)
		}

		if hovered && currentHoverCount <= 3 {
			layouts := getKeyboardLayouts()
			labels := []string{"Ctrl - ", "Shift - ", "Alt - "}
			fontName, _ := syscall.UTF16PtrFromString("Segoe UI")

			hFont, _, _ := procCreateFont.Call(14, 0, 0, 0, 400, 0, 0, 0, 0, 0, 0, NONANTIALIASED_QUALITY, 0, uintptr(unsafe.Pointer(fontName)))
			oldFont, _, _ := procSelectObject.Call(hdc, hFont)
			procSetBkMode.Call(hdc, TRANSPARENT)

			startX := curPos.X + 5
			startY := curPos.Y + 5
			if startX+55 > width {
				startX = curPos.X - 55
			}
			if startY+80 > height {
				startY = curPos.Y - 45
			}

			yOffset := startY
			for i := 0; i < len(labels) && i < len(layouts); i++ {
				langName := getLayoutName(layouts[i])

				utf16Label, _ := syscall.UTF16FromString(labels[i])
				utf16Lang, _ := syscall.UTF16FromString(langName)

				rowColor := keyColors[i]

				var labelWidth RECT
				procDrawText.Call(hdc, uintptr(unsafe.Pointer(&utf16Label[0])), uintptr(int32(len(utf16Label)-1)), uintptr(unsafe.Pointer(&labelWidth)), DT_LEFT|DT_TOP|DT_SINGLELINE|0x00000400)
				textW := labelWidth.Right - labelWidth.Left

				labelRect := RECT{Left: startX, Top: yOffset, Right: startX + textW + 10, Bottom: yOffset + 20}
				procSetTextColor.Call(hdc, uintptr(rowColor))
				procDrawText.Call(hdc, uintptr(unsafe.Pointer(&utf16Label[0])), uintptr(int32(len(utf16Label)-1)), uintptr(unsafe.Pointer(&labelRect)), DT_LEFT|DT_TOP|DT_SINGLELINE|DT_NOCLIP)

				langX := startX + textW
				langRect := RECT{Left: langX, Top: yOffset, Right: langX + 200, Bottom: yOffset + 20}
				procSetTextColor.Call(hdc, uintptr(rowColor))
				procDrawText.Call(hdc, uintptr(unsafe.Pointer(&utf16Lang[0])), uintptr(int32(len(utf16Lang)-1)), uintptr(unsafe.Pointer(&langRect)), DT_LEFT|DT_TOP|DT_SINGLELINE|DT_NOCLIP)

				yOffset += 12
			}

			procSelectObject.Call(hdc, oldFont)
			procDeleteObject.Call(hFont)
		}

		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	defProc := user32.NewProc("DefWindowProcW")
	ret, _, _ := defProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

//________________________________________________________
//Creates transparent top-level overlay window. createOverlayWindow
func createOverlayWindow() {
	className, _ := syscall.UTF16PtrFromString("OverlayClass")
	var wc WNDCLASSEX
	wc.Size = uint32(unsafe.Sizeof(wc))
	wc.Style = CS_DBLCLKS
	wc.WndProc = syscall.NewCallback(windowProc)
	wc.ClassName = className
	wc.Background = 0
	cursor, _, _ := procLoadCursor.Call(0, uintptr(IDC_ARROW))
	wc.Cursor = cursor
	procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	vx, _, _ := procGetSystemMetrics.Call(SM_XVIRTUALSCREEN)
	vy, _, _ := procGetSystemMetrics.Call(SM_YVIRTUALSCREEN)
	vw, _, _ := procGetSystemMetrics.Call(SM_CXVIRTUALSCREEN)
	vh, _, _ := procGetSystemMetrics.Call(SM_CYVIRTUALSCREEN)
	if vw == 0 || vh == 0 {
		w, _, _ := procGetSystemMetrics.Call(SM_CXSCREEN)
		h, _, _ := procGetSystemMetrics.Call(SM_CYSCREEN)
		vw = w
		vh = h
		vx = 0
		vy = 0
	}
	hwnd, _, _ := procCreateWindowEx.Call(WS_EX_TOPMOST|WS_EX_LAYERED|WS_EX_TOOLWINDOW|WS_EX_NOACTIVATE, uintptr(unsafe.Pointer(className)), 0, WS_POPUP, vx, vy, vw, vh, 0, 0, 0, 0)
	overlayHwnd = hwnd
	procSetLayeredWindowAttributes.Call(hwnd, crTransparent, 0, 1)
	procShowWindow.Call(hwnd, 5)
}