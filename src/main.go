package main

import (
	"fmt"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"golang.org/x/sys/windows"
)

const (
	WH_KEYBOARD_LL = 13

	WM_KEYDOWN    = 0x0100
	WM_KEYUP      = 0x0101
	WM_SYSKEYDOWN = 0x0104
	WM_SYSKEYUP   = 0x0105
	WM_SETCURSOR  = 0x0020

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
	Pt      struct{ X, Y int32 }
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

var (
	user32   = windows.NewLazySystemDLL("user32.dll")
	kernel32 = windows.NewLazySystemDLL("kernel32.dll")
	gdi32    = windows.NewLazySystemDLL("gdi32.dll")

	procSetProcessDPIAware          = user32.NewProc("SetProcessDPIAware")
	procSetWindowsHookEx            = user32.NewProc("SetWindowsHookExW")
	procUnhookWindowsHookEx         = user32.NewProc("UnhookWindowsHookEx")
	procCallNextHookEx              = user32.NewProc("CallNextHookEx")
	procGetMessage                  = user32.NewProc("GetMessageW")
	procDispatchMessage             = user32.NewProc("DispatchMessageW")
	procGetForegroundWindow         = user32.NewProc("GetForegroundWindow")
	procGetWindowThreadProcessId    = user32.NewProc("GetWindowThreadProcessId")
	procGetKeyboardLayout           = user32.NewProc("GetKeyboardLayout")
	procPostMessage                 = user32.NewProc("PostMessageW")
	procGetKeyboardLayoutList       = user32.NewProc("GetKeyboardLayoutList")
	procGetSystemMetrics            = user32.NewProc("GetSystemMetrics")
	procCreateWindowEx              = user32.NewProc("CreateWindowExW")
	procRegisterClassEx             = user32.NewProc("RegisterClassExW")
	procShowWindow                  = user32.NewProc("ShowWindow")
	procSetLayeredWindowAttributes  = user32.NewProc("SetLayeredWindowAttributes")
	procBeginPaint                  = user32.NewProc("BeginPaint")
	procEndPaint                    = user32.NewProc("EndPaint")
	procInvalidateRect              = user32.NewProc("InvalidateRect")
	procPostQuitMessage             = user32.NewProc("PostQuitMessage")
	procFillRect                    = user32.NewProc("FillRect")
	procLoadCursor                  = user32.NewProc("LoadCursorW")
	procGetGUIThreadInfo            = user32.NewProc("GetGUIThreadInfo")

	procBeep        = kernel32.NewProc("Beep")
	procCreateMutex = kernel32.NewProc("CreateMutexW")

	procCreateSolidBrush = gdi32.NewProc("CreateSolidBrush")
	procDeleteObject     = gdi32.NewProc("DeleteObject")

	hookHandle  uintptr
	overlayHwnd uintptr

	stateMu            sync.RWMutex
	currentBorderColor uint32 = 0
	hasBorder          bool   = false
	soundEnabled       bool   = true
	lastHKL            uintptr

	pressedKey     uint32
	pressTime      time.Time
	isComboPressed bool
	maxTapDuration = 400 * time.Millisecond

	borderThickness = int32(4)

	typingSoundChan = make(chan uint32, 50)

	layoutsMu     sync.RWMutex
	cachedLayouts []uintptr
)

const (
	mutexName     = "Global\\LangSwitcherAppMutex"
	crTransparent = 0x00FF00FF
)

//________________
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

//________________
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

//________________
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

//________________
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
	refreshKeyboardLayouts()
	initSoundWorker()
	fmt.Println("Started successfully")
	createOverlayWindow()
	go trackLanguageChanges()
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

//________________
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

//________________
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
		currentBorderColor = 0x000000FF
	case 1:
		hasBorder = false
	case 2:
		hasBorder = true
		currentBorderColor = 0x0000FFFF
	default:
		hasBorder = false
	}
	stateMu.Unlock()
	if overlayHwnd != 0 {
		procPostMessage.Call(overlayHwnd, WM_APP_REDRAW, 0, 0)
	}
}

//________________
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
						go applyLanguage(0, 400, 0x000000FF)
					case isShift(vk):
						go applyLanguage(1, 800, 0)
					case isAlt(vk):
						go applyLanguage(2, 1200, 0x0000FFFF)
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

//________________
//Evaluates if key belongs to active target modifiers. isTargetKey
func isTargetKey(vk uint32) bool {
	return isCtrl(vk) || isShift(vk) || isAlt(vk)
}

//________________
//Checks for Control virtual key codes. isCtrl
func isCtrl(vk uint32) bool {
	return vk == VK_LCONTROL || vk == VK_RCONTROL || vk == VK_CONTROL
}

//________________
//Checks for Shift virtual key codes. isShift
func isShift(vk uint32) bool {
	return vk == VK_LSHIFT || vk == VK_RSHIFT || vk == VK_SHIFT
}

//________________
//Checks for Alt virtual key codes. isAlt
func isAlt(vk uint32) bool {
	return vk == VK_LMENU || vk == VK_RMENU || vk == VK_MENU
}

//________________
//Verifies if keyup corresponds to initial keydown event. isBaseKeyMatch
func isBaseKeyMatch(k1, k2 uint32) bool {
	return (isCtrl(k1) && isCtrl(k2)) || (isShift(k1) && isShift(k2)) || (isAlt(k1) && isAlt(k2))
}

//________________
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

//________________
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

//________________
//Processes window messages for overlay rendering and clicks. windowProc
func windowProc(hwnd uintptr, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_ERASEBKGND:
		return 1
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
		procEndPaint.Call(hwnd, uintptr(unsafe.Pointer(&ps)))
		return 0
	}
	defProc := user32.NewProc("DefWindowProcW")
	ret, _, _ := defProc.Call(hwnd, uintptr(msg), wParam, lParam)
	return ret
}

//________________
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