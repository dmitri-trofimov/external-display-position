//go:build windows

package watcher

import (
	"fmt"
	"io"
	"log"
	"os"
	"os/signal"
	"runtime"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"external-display-position/internal/config"
	"external-display-position/internal/display"
)

const (
	wmDestroy        = 0x0002
	wmClose          = 0x0010
	wmDisplayChange  = 0x007E
	wmTimer          = 0x0113
	wmPowerBroadcast = 0x0218

	pbtApmResumeSuspend   = 0x0007
	pbtApmResumeAutomatic = 0x0012

	wsOverlapped = 0x00000000

	retryTimerID = 1
	retryDelayMs = 300
	maxRetries   = 3

	loopGuardWindow = 3 * time.Second
	maxLoopApplies  = 3
)

var (
	kernel32             = syscall.NewLazyDLL("kernel32.dll")
	procGetModuleHandleW = kernel32.NewProc("GetModuleHandleW")
	procCreateMutexW     = kernel32.NewProc("CreateMutexW")
	procOpenMutexW       = kernel32.NewProc("OpenMutexW")

	user32              = syscall.NewLazyDLL("user32.dll")
	procRegisterClassEx = user32.NewProc("RegisterClassExW")
	procCreateWindowEx  = user32.NewProc("CreateWindowExW")
	procDefWindowProc   = user32.NewProc("DefWindowProcW")
	procGetMessage      = user32.NewProc("GetMessageW")
	procTranslateMsg    = user32.NewProc("TranslateMessage")
	procDispatchMsg     = user32.NewProc("DispatchMessageW")
	procPostMessage     = user32.NewProc("PostMessageW")
	procPostQuitMessage = user32.NewProc("PostQuitMessage")
	procDestroyWindow   = user32.NewProc("DestroyWindow")
	procSetTimer        = user32.NewProc("SetTimer")
	procKillTimer       = user32.NewProc("KillTimer")
)

type wndClassExW struct {
	cbSize        uint32
	style         uint32
	lpfnWndProc   uintptr
	cbClsExtra    int32
	cbWndExtra    int32
	hInstance     syscall.Handle
	hIcon         syscall.Handle
	hCursor       syscall.Handle
	hbrBackground syscall.Handle
	lpszMenuName  *uint16
	lpszClassName *uint16
	hIconSm       syscall.Handle
}

type point struct {
	x int32
	y int32
}

type winMsg struct {
	hwnd    syscall.Handle
	message uint32
	wParam  uintptr
	lParam  uintptr
	time    uint32
	pt      point
}

func init() {
	if unsafe.Sizeof(wndClassExW{}) != 80 {
		panic(fmt.Sprintf("unexpected WNDCLASSEXW size: %d", unsafe.Sizeof(wndClassExW{})))
	}
	if unsafe.Sizeof(winMsg{}) != 48 {
		panic(fmt.Sprintf("unexpected MSG size: %d", unsafe.Sizeof(winMsg{})))
	}
}

// AcquireSingleInstance creates a named Win32 mutex so only one watcher runs per user session.
// Returns a release func or an error if another watcher is already running.
func AcquireSingleInstance() (func(), error) {
	name, err := syscall.UTF16PtrFromString("Local\\ExternalDisplayPositionWatcher")
	if err != nil {
		return nil, err
	}
	handle, _, lastErr := procCreateMutexW.Call(0, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return nil, fmt.Errorf("CreateMutexW failed: %w", lastErr)
	}
	const errorAlreadyExists = 183
	if errno, ok := lastErr.(syscall.Errno); ok && errno == errorAlreadyExists {
		_ = syscall.CloseHandle(syscall.Handle(handle))
		return nil, fmt.Errorf("another watcher instance is already running")
	}
	return func() {
		_ = syscall.CloseHandle(syscall.Handle(handle))
	}, nil
}

// IsRunning reports whether a watcher instance currently holds the session mutex.
func IsRunning() bool {
	name, err := syscall.UTF16PtrFromString("Local\\ExternalDisplayPositionWatcher")
	if err != nil {
		return false
	}
	const synchronize = 0x00100000
	handle, _, _ := procOpenMutexW.Call(synchronize, 0, uintptr(unsafe.Pointer(name)))
	if handle == 0 {
		return false
	}
	_ = syscall.CloseHandle(syscall.Handle(handle))
	return true
}

type state struct {
	configPath     string
	logger         *log.Logger
	cachedCfg      *config.Config
	cachedModTime  time.Time
	hasCached      bool
	lastAppliedSig string
	lastApplyAt    time.Time
	applyStreak    int
	hwnd           syscall.Handle
	retryCount     int
}

var (
	activeMu    sync.Mutex
	activeState *state
)

// Run starts the Win32 message loop and blocks until interrupted or closed.
func Run(configPath string, logOut io.Writer) error {
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()

	logger := log.New(logOut, "", log.LstdFlags)

	st := &state{
		configPath: configPath,
		logger:     logger,
	}

	activeMu.Lock()
	activeState = st
	activeMu.Unlock()
	defer func() {
		activeMu.Lock()
		if activeState == st {
			activeState = nil
		}
		activeMu.Unlock()
	}()

	hInstance, _, _ := procGetModuleHandleW.Call(0)
	className, err := syscall.UTF16PtrFromString("ExternalDisplayPositionWatcherWindow")
	if err != nil {
		return err
	}
	wndTitle, err := syscall.UTF16PtrFromString("ExternalDisplayPositionWatcher")
	if err != nil {
		return err
	}

	wc := wndClassExW{
		cbSize:        uint32(unsafe.Sizeof(wndClassExW{})),
		lpfnWndProc:   syscall.NewCallback(wndProc),
		hInstance:     syscall.Handle(hInstance),
		lpszClassName: className,
	}
	atom, _, lastErr := procRegisterClassEx.Call(uintptr(unsafe.Pointer(&wc)))
	if atom == 0 {
		return fmt.Errorf("RegisterClassExW failed: %w", lastErr)
	}

	// Invisible top-level window (not HWND_MESSAGE) so Windows delivers WM_DISPLAYCHANGE broadcasts.
	hwnd, _, lastErr := procCreateWindowEx.Call(
		0,
		uintptr(unsafe.Pointer(className)),
		uintptr(unsafe.Pointer(wndTitle)),
		wsOverlapped,
		0, 0, 0, 0,
		0,
		0,
		hInstance,
		0,
	)
	if hwnd == 0 {
		return fmt.Errorf("CreateWindowExW failed: %w", lastErr)
	}
	st.hwnd = syscall.Handle(hwnd)

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, os.Interrupt)
	defer signal.Stop(sigCh)
	go func(target syscall.Handle) {
		for range sigCh {
			procPostMessage.Call(uintptr(target), wmClose, 0, 0)
		}
	}(st.hwnd)

	logger.Printf("watcher started (config=%s)", configPath)
	st.applyCurrent("startup")

	var msg winMsg
	for {
		ret, _, err := procGetMessage.Call(uintptr(unsafe.Pointer(&msg)), 0, 0, 0)
		switch int32(ret) {
		case -1:
			return fmt.Errorf("GetMessageW failed: %w", err)
		case 0:
			logger.Printf("watcher stopped")
			return nil
		default:
			procTranslateMsg.Call(uintptr(unsafe.Pointer(&msg)))
			procDispatchMsg.Call(uintptr(unsafe.Pointer(&msg)))
		}
	}
}

func wndProc(hwnd syscall.Handle, msg uint32, wParam, lParam uintptr) uintptr {
	activeMu.Lock()
	st := activeState
	activeMu.Unlock()

	switch msg {
	case wmDisplayChange:
		if st != nil {
			st.retryCount = 0
			procKillTimer.Call(uintptr(hwnd), retryTimerID)
			st.applyCurrent("WM_DISPLAYCHANGE")
		}
		return 0
	case wmPowerBroadcast:
		if st != nil && (wParam == pbtApmResumeSuspend || wParam == pbtApmResumeAutomatic) {
			st.retryCount = 0
			procKillTimer.Call(uintptr(hwnd), retryTimerID)
			st.applyCurrent("WM_POWERBROADCAST")
		}
		return 1
	case wmTimer:
		if st != nil && wParam == retryTimerID {
			procKillTimer.Call(uintptr(hwnd), retryTimerID)
			st.applyCurrent("retry")
		}
		return 0
	case wmClose:
		procDestroyWindow.Call(uintptr(hwnd))
		return 0
	case wmDestroy:
		procPostQuitMessage.Call(0)
		return 0
	}
	ret, _, _ := procDefWindowProc.Call(uintptr(hwnd), uintptr(msg), wParam, lParam)
	return ret
}

func (s *state) loadConfigIfChanged() (*config.Config, error) {
	info, err := os.Stat(s.configPath)
	if err != nil {
		if os.IsNotExist(err) {
			s.cachedCfg = &config.Config{Profiles: make(map[string]config.Profile)}
			s.hasCached = false
			return s.cachedCfg, nil
		}
		return nil, err
	}
	if s.hasCached && info.ModTime().Equal(s.cachedModTime) {
		return s.cachedCfg, nil
	}
	cfg, err := config.Load(s.configPath)
	if err != nil {
		return nil, err
	}
	s.cachedCfg = cfg
	s.cachedModTime = info.ModTime()
	s.hasCached = true
	return cfg, nil
}

func (s *state) applyCurrent(reason string) {
	cfg, err := s.loadConfigIfChanged()
	if err != nil {
		s.logger.Printf("[%s] config error: %v", reason, err)
		return
	}
	if len(cfg.Profiles) == 0 {
		return
	}

	snap, err := display.QueryActive()
	if err != nil {
		s.logger.Printf("[%s] query displays failed: %v", reason, err)
		s.scheduleRetry()
		return
	}
	if snap.Signature == "" {
		return
	}

	prof, ok := cfg.Profiles[snap.Signature]
	if !ok {
		return
	}

	now := time.Now()
	if snap.Signature == s.lastAppliedSig && now.Sub(s.lastApplyAt) < loopGuardWindow && s.applyStreak >= maxLoopApplies {
		return
	}

	changed, err := snap.ApplyPositions(prof.Positions)
	if err != nil {
		s.logger.Printf("[%s] apply failed for %s: %v", reason, snap.Signature, err)
		s.scheduleRetry()
		return
	}

	s.retryCount = 0
	if !changed {
		s.applyStreak = 0
		return
	}

	if snap.Signature == s.lastAppliedSig && now.Sub(s.lastApplyAt) < loopGuardWindow {
		s.applyStreak++
	} else {
		s.lastAppliedSig = snap.Signature
		s.applyStreak = 1
	}
	s.lastApplyAt = now

	label := snap.Signature
	if prof.Name != "" {
		label = fmt.Sprintf("%s (%s)", prof.Name, snap.Signature)
	}
	s.logger.Printf("[%s] applied layout %s", reason, label)
}

func (s *state) scheduleRetry() {
	if s.hwnd == 0 || s.retryCount >= maxRetries {
		return
	}
	s.retryCount++
	procSetTimer.Call(uintptr(s.hwnd), retryTimerID, retryDelayMs, 0)
}
