package backend

import (
	"fmt"
	"os"
	"runtime"
	"sync"
	"syscall"
	"unsafe"
)

var (
	user32   = syscall.NewLazyDLL("user32.dll")
	shell32  = syscall.NewLazyDLL("shell32.dll")
	kernel32 = syscall.NewLazyDLL("kernel32.dll")
	gdi32    = syscall.NewLazyDLL("gdi32.dll")

	procGetModuleHandleW   = kernel32.NewProc("GetModuleHandleW")
	procCreateWindowExW    = user32.NewProc("CreateWindowExW")
	procDefWindowProcW     = user32.NewProc("DefWindowProcW")
	procRegisterClassExW   = user32.NewProc("RegisterClassExW")
	procGetMessageW        = user32.NewProc("GetMessageW")
	procDispatchMessageW   = user32.NewProc("DispatchMessageW")
	procPostQuitMessage    = user32.NewProc("PostQuitMessage")
	procLoadCursorW        = user32.NewProc("LoadCursorW")
	procCreatePopupMenu    = user32.NewProc("CreatePopupMenu")
	procAppendMenuW        = user32.NewProc("AppendMenuW")
	procTrackPopupMenu     = user32.NewProc("TrackPopupMenu")
	procDestroyMenu        = user32.NewProc("DestroyMenu")
	procSetForegroundWindow = user32.NewProc("SetForegroundWindow")
	procPostMessageW       = user32.NewProc("PostMessageW")
	procLoadImageW         = user32.NewProc("LoadImageW")
	procGetCursorPos       = user32.NewProc("GetCursorPos")
	procDestroyIcon        = user32.NewProc("DestroyIcon")
	procDestroyWindow      = user32.NewProc("DestroyWindow")
	procTranslateMessage   = user32.NewProc("TranslateMessage")
	procCreateIconIndirect = user32.NewProc("CreateIconIndirect")

	procShellNotifyIconW = shell32.NewProc("Shell_NotifyIconW")

	procGetDC                = user32.NewProc("GetDC")
	procReleaseDC            = user32.NewProc("ReleaseDC")
	procCreateCompatibleDC   = gdi32.NewProc("CreateCompatibleDC")
	procCreateCompatibleBitmap = gdi32.NewProc("CreateCompatibleBitmap")
	procSelectObject         = gdi32.NewProc("SelectObject")
	procDeleteObject         = gdi32.NewProc("DeleteObject")
	procDeleteDC             = gdi32.NewProc("DeleteDC")
	procGetDIBits            = gdi32.NewProc("GetDIBits")
	procSetDIBits            = gdi32.NewProc("SetDIBits")
	procCreateBitmap         = gdi32.NewProc("CreateBitmap")
)

const (
	WM_RBUTTONUP = 0x0205
	WM_LBUTTONUP = 0x0202
	WM_COMMAND   = 0x0111
	WM_DESTROY   = 0x0002
	WM_USER      = 0x0400
	WM_APP       = 0x8000
	WM_TRAYICON  = WM_APP + 1

	NIF_MESSAGE = 0x0001
	NIF_ICON    = 0x0002
	NIF_TIP     = 0x0004

	NIM_ADD    = 0x0000
	NIM_MODIFY = 0x0001
	NIM_DELETE = 0x0002

	MF_STRING    = 0x0000
	MF_SEPARATOR = 0x0800
	MF_CHECKED   = 0x0008

	TPM_RIGHTBUTTON = 0x0002

	ID_TRAY_EXIT       = 1001
	ID_TRAY_SHOW       = 1002
	ID_TRAY_MODEL_BASE = 2000
)

// NOTIFYICONDATAW with explicit padding to match Windows SDK 64-bit layout (976 bytes).
// Field offsets verified against MSVC default packing.
type NOTIFYICONDATAW struct {
	CbSize           uint32
	_                [4]byte
	HWnd             syscall.Handle
	UID              uint32
	UFlags           uint32
	UCallbackMessage uint32
	_                [4]byte
	HIcon            syscall.Handle
	SzTip            [128]uint16
	DwState          uint32
	DwStateMask      uint32
	SzInfo           [256]uint16
	UVersion         uint32
	SzInfoTitle      [64]uint16
	DwInfoFlags      uint32
	GuidItem         [16]byte
	HBalloonIcon     syscall.Handle
}

func buildNID(hwnd, hIcon syscall.Handle, tip string, callbackMsg uint32, flags uint32, uid uint32) NOTIFYICONDATAW {
	nid := NOTIFYICONDATAW{
		CbSize:           uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:             hwnd,
		UID:              uid,
		UFlags:           flags,
		UCallbackMessage: callbackMsg,
		HIcon:            hIcon,
	}
	copy(nid.SzTip[:], syscall.StringToUTF16(tip))
	return nid
}

func buildNIDMinimal(hwnd syscall.Handle, uid uint32) NOTIFYICONDATAW {
	return NOTIFYICONDATAW{
		CbSize: uint32(unsafe.Sizeof(NOTIFYICONDATAW{})),
		HWnd:   hwnd,
		UID:    uid,
	}
}

type MSG struct {
	Hwnd    syscall.Handle
	Message uint32
	WParam  uintptr
	LParam  uintptr
	Time    uint32
	PtX     int32
	PtY     int32
}

type WNDCLASSEXW struct {
	Size          uint32
	Style         uint32
	WndProc       uintptr
	ClsExtra      int32
	WndExtra      int32
	HInstance     syscall.Handle
	HIcon         syscall.Handle
	HCursor       syscall.Handle
	HbrBackground syscall.Handle
	MenuName      *uint16
	ClassName     *uint16
	HIconSm       syscall.Handle
}

type POINT struct {
	X, Y int32
}

type TrayCallbacks struct {
	OnModelSelect func(alias string)
	OnShowWindow  func()
	OnExit        func()
	GetModels     func() []RouteConfig
	GetCurrent    func() string
}

type trayAction struct {
	cmd   int    // ID_TRAY_EXIT, ID_TRAY_SHOW, or model index (>= 0)
	alias string // model alias (only for model select)
}

type SystemTray struct {
	mu         sync.Mutex
	hwnd       syscall.Handle
	hIcon      syscall.Handle
	running    bool
	callbacks  TrayCallbacks
	done       chan struct{}  // signals when message loop exits
	initDone   chan error     // signals window+icon creation result
	actionChan chan trayAction // dispatches menu actions to a dedicated goroutine
}

func NewSystemTray() *SystemTray {
	return &SystemTray{
		done:       make(chan struct{}),
		initDone:   make(chan error, 1),
		actionChan: make(chan trayAction, 16),
	}
}

func (st *SystemTray) SetCallbacks(cb TrayCallbacks) {
	st.mu.Lock()
	st.callbacks = cb
	st.mu.Unlock()
}

// Start launches a dedicated OS-threaded goroutine that creates the tray window
// and runs its message loop. This is REQUIRED because Windows delivers messages
// to the thread that created the window.
func (st *SystemTray) Start() error {
	st.mu.Lock()
	if st.running {
		st.mu.Unlock()
		return nil
	}
	st.mu.Unlock()

	go st.runThread()
	return <-st.initDone
}

// runThread locks to an OS thread, creates all tray resources, then pumps messages.
func (st *SystemTray) runThread() {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	if err := st.setup(); err != nil {
		st.initDone <- err
		close(st.done)
		return
	}

	st.mu.Lock()
	st.running = true
	st.mu.Unlock()
	st.initDone <- nil

	// Start callback dispatcher goroutine (not locked to OS thread)
	// so Wails runtime calls are safe.
	go st.dispatchLoop()

	// Message pump — stays on this OS thread forever until Quit posted.
	for {
		var msg MSG
		ret, _, _ := procGetMessageW.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		if ret == 0 { // WM_QUIT
			break
		}
		if ret == ^uintptr(0) { // -1 error
			break
		}
		procTranslateMessage.Call(uintptr(unsafe.Pointer(&msg)))
		procDispatchMessageW.Call(uintptr(unsafe.Pointer(&msg)))
	}

	st.mu.Lock()
	st.running = false
	st.hwnd = 0
	st.hIcon = 0
	st.mu.Unlock()
	close(st.done)
}

// dispatchLoop processes tray menu actions on a regular goroutine (not OS-locked)
// so that Wails runtime calls from callbacks are safe.
func (st *SystemTray) dispatchLoop() {
	defer func() {
		if r := recover(); r != nil {
			fmt.Fprintf(os.Stderr, "tray dispatch panic: %v\n", r)
		}
	}()
	for action := range st.actionChan {
		st.mu.Lock()
		cb := st.callbacks
		st.mu.Unlock()

		switch action.cmd {
		case ID_TRAY_EXIT:
			if cb.OnExit != nil {
				cb.OnExit()
			}
		case ID_TRAY_SHOW:
			if cb.OnShowWindow != nil {
				cb.OnShowWindow()
			}
		default: // model select
			if cb.OnModelSelect != nil && cb.GetModels != nil {
				models := cb.GetModels()
				if action.cmd >= 0 && action.cmd < len(models) {
					cb.OnModelSelect(models[action.cmd].Alias)
				}
			}
		}
	}
}

// setup creates window class, message window, icon, and registers tray icon.
func (st *SystemTray) setup() error {
	hInst, _, _ := procGetModuleHandleW.Call(0)
	if hInst == 0 {
		return fmt.Errorf("get module handle failed")
	}

	className, _ := syscall.UTF16PtrFromString("MoonBridgeTrayWindow")
	cursor, _, _ := procLoadCursorW.Call(0, 32512)

	wc := WNDCLASSEXW{
		Size:      uint32(unsafe.Sizeof(WNDCLASSEXW{})),
		WndProc:   syscall.NewCallback(st.wndProc),
		HInstance: syscall.Handle(hInst),
		HCursor:   syscall.Handle(cursor),
		ClassName: className,
	}
	if ret, _, _ := procRegisterClassExW.Call(uintptr(unsafe.Pointer(&wc))); ret == 0 {
		return fmt.Errorf("register window class failed")
	}

	hwnd, _, _ := procCreateWindowExW.Call(
		0, uintptr(unsafe.Pointer(className)), 0, 0,
		0, 0, 0, 0,
		^uintptr(2), // HWND_MESSAGE
		0, hInst, 0,
	)
	if hwnd == 0 {
		return fmt.Errorf("create window failed")
	}
	st.hwnd = syscall.Handle(hwnd)

	st.hIcon = createTrayIcon()
	if st.hIcon == 0 {
		procDestroyWindow.Call(uintptr(hwnd))
		return fmt.Errorf("create tray icon failed")
	}

	nid := buildNID(st.hwnd, st.hIcon, "Moon Bridge Manager", WM_TRAYICON, NIF_MESSAGE|NIF_ICON|NIF_TIP, 1)

	if ret, _, errResult := procShellNotifyIconW.Call(NIM_ADD, uintptr(unsafe.Pointer(&nid))); ret == 0 {
		_ = errResult
		procDestroyWindow.Call(uintptr(hwnd))
		procDestroyIcon.Call(uintptr(st.hIcon))
		return fmt.Errorf("shell notify icon add failed (err=%d)", errResult)
	}

	return nil
}

func (st *SystemTray) Stop() {
	st.mu.Lock()
	hwnd := st.hwnd
	hIcon := st.hIcon
	st.mu.Unlock()

	if hwnd != 0 {
		nid := buildNIDMinimal(hwnd, 1)
		procShellNotifyIconW.Call(NIM_DELETE, uintptr(unsafe.Pointer(&nid)))
		procPostMessageW.Call(uintptr(hwnd), WM_DESTROY, 0, 0)
	}
	if hIcon != 0 {
		procDestroyIcon.Call(uintptr(hIcon))
	}
	close(st.actionChan)
}

func (st *SystemTray) UpdateMenu() {
	st.mu.Lock()
	hwnd := st.hwnd
	st.mu.Unlock()
	if hwnd != 0 {
		procPostMessageW.Call(uintptr(hwnd), WM_USER+1, 0, 0)
	}
}

func (st *SystemTray) wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	switch msg {
	case WM_TRAYICON:
		if lParam == WM_RBUTTONUP || lParam == WM_LBUTTONUP {
			st.showContextMenu()
		}
		return 0

	case WM_COMMAND:
		cmd := wParam & 0xFFFF
		switch {
		case cmd == ID_TRAY_EXIT:
			st.actionChan <- trayAction{cmd: ID_TRAY_EXIT}
		case cmd == ID_TRAY_SHOW:
			st.actionChan <- trayAction{cmd: ID_TRAY_SHOW}
		case cmd >= ID_TRAY_MODEL_BASE:
			idx := int(cmd - ID_TRAY_MODEL_BASE)
			st.actionChan <- trayAction{cmd: idx}
		}
		return 0

	case WM_DESTROY:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProcW.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (st *SystemTray) showContextMenu() {
	procSetForegroundWindow.Call(uintptr(st.hwnd))

	hMenu, _, _ := procCreatePopupMenu.Call()
	if hMenu == 0 {
		return
	}

	var current string
	var models []RouteConfig

	st.mu.Lock()
	cb := st.callbacks
	st.mu.Unlock()

	if cb.GetCurrent != nil {
		current = cb.GetCurrent()
	}
	if cb.GetModels != nil {
		models = cb.GetModels()
	}

	if len(models) == 0 {
		noLabel, _ := syscall.UTF16PtrFromString("(无可用模型)")
		procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_MODEL_BASE, uintptr(unsafe.Pointer(noLabel)))
	} else {
		for i, m := range models {
			label, _ := syscall.UTF16PtrFromString(m.Alias)
			flags := uintptr(MF_STRING)
			if m.Alias == current {
				flags |= MF_CHECKED
			}
			procAppendMenuW.Call(hMenu, flags, ID_TRAY_MODEL_BASE+uintptr(i), uintptr(unsafe.Pointer(label)))
		}
	}

	procAppendMenuW.Call(hMenu, MF_SEPARATOR, 0, 0)

	showLabel, _ := syscall.UTF16PtrFromString("显示窗口")
	procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_SHOW, uintptr(unsafe.Pointer(showLabel)))

	exitLabel, _ := syscall.UTF16PtrFromString("退出")
	procAppendMenuW.Call(hMenu, MF_STRING, ID_TRAY_EXIT, uintptr(unsafe.Pointer(exitLabel)))

	var pt POINT
	procGetCursorPos.Call(uintptr(unsafe.Pointer(&pt)))

	procTrackPopupMenu.Call(hMenu, TPM_RIGHTBUTTON, uintptr(pt.X), uintptr(pt.Y), 0, uintptr(st.hwnd), 0)
	procDestroyMenu.Call(hMenu)
}

func createTrayIcon() syscall.Handle {
	// The Wails binary does not embed icon resources (no .rsrc section),
	// so LoadImageW from resources always fails.
	// Create a simple colored icon programmatically via CreateIconIndirect.
	return createFallbackIcon()
}

func createFallbackIcon() syscall.Handle {
	// Use gdi32 and user32 package-level procs to create a 32x32 DIB section,
	// fill with color, then convert to HICON via CreateIconIndirect.
	const DIB_RGB_COLORS = 0

	// Screen DC for compatibility
	hDC, _, _ := procGetDC.Call(0)
	if hDC == 0 {
		return 0
	}

	// 32-bit DIB section
	hMemDC, _, _ := procCreateCompatibleDC.Call(hDC)
	if hMemDC == 0 {
		procReleaseDC.Call(0, hDC)
		return 0
	}

	hBitmap, _, _ := procCreateCompatibleBitmap.Call(hDC, 32, 32)
	if hBitmap == 0 {
		procDeleteDC.Call(hMemDC)
		procReleaseDC.Call(0, hDC)
		return 0
	}

	oldBmp, _, _ := procSelectObject.Call(hMemDC, hBitmap)

	// Read pixels as BGRA
	type BITMAPINFOHEADER struct {
		Size          uint32
		Width         int32
		Height        int32
		Planes        uint16
		BitCount      uint16
		Compression   uint32
		SizeImage     uint32
		XPelsPerMeter int32
		YPelsPerMeter int32
		ClrUsed       uint32
		ClrImportant  uint32
	}

	bmi := make([]byte, 40) // BITMAPINFOHEADER = 40 bytes
	b := (*BITMAPINFOHEADER)(unsafe.Pointer(&bmi[0]))
	b.Size = 40
	b.Width = 32
	b.Height = 32
	b.Planes = 1
	b.BitCount = 32
	b.Compression = 0

	bits := make([]byte, 32*32*4)
	procGetDIBits.Call(hMemDC, hBitmap, 0, 32, uintptr(unsafe.Pointer(&bits[0])), uintptr(unsafe.Pointer(&bmi[0])), DIB_RGB_COLORS)

	// Fill with a solid blue (#4F46E5, full alpha)
	for i := 0; i < len(bits); i += 4 {
		bits[i] = 0xE5   // B
		bits[i+1] = 0x46 // G
		bits[i+2] = 0x4F // R
		bits[i+3] = 0xFF // A
	}

	// Write back
	b.Height = -32 // top-down DIB
	procSetDIBits.Call(hMemDC, hBitmap, 0, 32, uintptr(unsafe.Pointer(&bits[0])), uintptr(unsafe.Pointer(&bmi[0])), DIB_RGB_COLORS)

	// Create AND mask (all zeros = fully opaque)
	maskBits := make([]byte, 32*32) // 1 bit per pixel, padded to 4 bytes per row = 128 bytes
	for i := range maskBits {
		maskBits[i] = 0x00
	}

	type ICONINFO struct {
		FIcon    int32
		XHotspot uint32
		YHotspot uint32
		HbmMask  syscall.Handle
		HbmColor syscall.Handle
	}

	hMaskBmp, _, _ := procCreateBitmap.Call(32, 32, 1, 1, uintptr(unsafe.Pointer(&maskBits[0])))
	if hMaskBmp == 0 {
		procSelectObject.Call(hMemDC, oldBmp)
		procDeleteObject.Call(hBitmap)
		procDeleteDC.Call(hMemDC)
		procReleaseDC.Call(0, hDC)
		return 0
	}

	ico := ICONINFO{
		FIcon:    1,
		HbmMask:  syscall.Handle(hMaskBmp),
		HbmColor: syscall.Handle(hBitmap),
	}
	hIcon, _, _ := procCreateIconIndirect.Call(uintptr(unsafe.Pointer(&ico)))

	// Cleanup DIB resources
	procSelectObject.Call(hMemDC, oldBmp)
	procDeleteObject.Call(hBitmap)
	procDeleteObject.Call(hMaskBmp)
	procDeleteDC.Call(hMemDC)
	procReleaseDC.Call(0, hDC)

	if hIcon == 0 {
		return 0
	}
	return syscall.Handle(hIcon)
}
