//go:build windows

package display

import (
	"fmt"
	"sort"
	"strings"
	"syscall"
	"unsafe"
)

const (
	qdcOnlyActivePaths = 0x00000002

	sdcApply                    = 0x00000080
	sdcUseSuppliedDisplayConfig = 0x00000020
	sdcSaveToDatabase           = 0x00000200
	sdcAllowChanges             = 0x00000400

	displayConfigDeviceInfoGetTargetName = 2
	displayConfigModeInfoTypeSource      = 1
	displayConfigPathModeIdxInvalid      = 0xFFFFFFFF

	errorSuccess            = 0
	errorInsufficientBuffer = 122
)

var (
	user32                           = syscall.NewLazyDLL("user32.dll")
	procGetDisplayConfigBufferSizes  = user32.NewProc("GetDisplayConfigBufferSizes")
	procQueryDisplayConfig           = user32.NewProc("QueryDisplayConfig")
	procSetDisplayConfig             = user32.NewProc("SetDisplayConfig")
	procDisplayConfigGetDeviceInfo   = user32.NewProc("DisplayConfigGetDeviceInfo")
)

type luid struct {
	LowPart  uint32
	HighPart int32
}

type displayConfigRational struct {
	Numerator   uint32
	Denominator uint32
}

type displayConfigPathSourceInfo struct {
	AdapterID   luid
	ID          uint32
	ModeInfoIdx uint32
	StatusFlags uint32
}

type displayConfigPathTargetInfo struct {
	AdapterID        luid
	ID               uint32
	ModeInfoIdx      uint32
	OutputTechnology uint32
	Rotation         uint32
	Scaling          uint32
	RefreshRate      displayConfigRational
	ScanLineOrdering uint32
	TargetAvailable  int32
	StatusFlags      uint32
}

type displayConfigPathInfo struct {
	SourceInfo displayConfigPathSourceInfo
	TargetInfo displayConfigPathTargetInfo
	Flags      uint32
}

type displayConfigModeInfo struct {
	InfoType  uint32
	ID        uint32
	AdapterID luid
	Union     [6]uint64
}

type displayConfigSourceMode struct {
	Width       uint32
	Height      uint32
	PixelFormat uint32
	PosX        int32
	PosY        int32
}

type displayConfigDeviceInfoHeader struct {
	InfoType  uint32
	Size      uint32
	AdapterID luid
	ID        uint32
}

type displayConfigTargetDeviceName struct {
	Header                    displayConfigDeviceInfoHeader
	Flags                     uint32
	OutputTechnology          uint32
	EdidManufactureID         uint16
	EdidProductCodeID         uint16
	ConnectorInstance         uint32
	MonitorFriendlyDeviceName [64]uint16
	MonitorDevicePath         [128]uint16
}

func init() {
	if unsafe.Sizeof(displayConfigPathInfo{}) != 72 {
		panic(fmt.Sprintf("unexpected DISPLAYCONFIG_PATH_INFO size: %d", unsafe.Sizeof(displayConfigPathInfo{})))
	}
	if unsafe.Sizeof(displayConfigModeInfo{}) != 64 {
		panic(fmt.Sprintf("unexpected DISPLAYCONFIG_MODE_INFO size: %d", unsafe.Sizeof(displayConfigModeInfo{})))
	}
	if unsafe.Sizeof(displayConfigTargetDeviceName{}) != 420 {
		panic(fmt.Sprintf("unexpected DISPLAYCONFIG_TARGET_DEVICE_NAME size: %d", unsafe.Sizeof(displayConfigTargetDeviceName{})))
	}
}

// DisplayInfo describes a single active monitor + connector target in the current topology.
type DisplayInfo struct {
	ID           string `json:"id"`
	FriendlyName string `json:"friendlyName,omitempty"`
	DevicePath   string `json:"devicePath,omitempty"`
	TargetID     uint32 `json:"targetId"`
	X            int32  `json:"x"`
	Y            int32  `json:"y"`
	Width        uint32 `json:"width"`
	Height       uint32 `json:"height"`
}

// Position holds target desktop coordinates for a display ID.
type Position struct {
	X int32 `json:"x"`
	Y int32 `json:"y"`
}

// Snapshot captures the live Windows CCD topology and decoded display metadata.
type Snapshot struct {
	Displays  []DisplayInfo
	Signature string
	paths     []displayConfigPathInfo
	modes     []displayConfigModeInfo
	modeIndex map[string]uint32
}

// QueryActive queries Windows for all active display paths, modes, and target names.
func QueryActive() (*Snapshot, error) {
	paths, modes, err := queryRawActive()
	if err != nil {
		return nil, err
	}

	displays := make([]DisplayInfo, 0, len(paths))
	modeIndex := make(map[string]uint32, len(paths))

	for _, path := range paths {
		srcIdx := path.SourceInfo.ModeInfoIdx
		if srcIdx == displayConfigPathModeIdxInvalid || int(srcIdx) >= len(modes) {
			continue
		}
		if modes[srcIdx].InfoType != displayConfigModeInfoTypeSource {
			continue
		}
		srcMode := (*displayConfigSourceMode)(unsafe.Pointer(&modes[srcIdx].Union[0]))

		friendly, devPath, mfgID, prodID := getTargetDeviceName(path.TargetInfo.AdapterID, path.TargetInfo.ID)
		dispID := buildDisplayID(devPath, mfgID, prodID, path.TargetInfo.ID)

		displays = append(displays, DisplayInfo{
			ID:           dispID,
			FriendlyName: friendly,
			DevicePath:   devPath,
			TargetID:     path.TargetInfo.ID,
			X:            srcMode.PosX,
			Y:            srcMode.PosY,
			Width:        srcMode.Width,
			Height:       srcMode.Height,
		})
		modeIndex[dispID] = srcIdx
	}

	sort.Slice(displays, func(i, j int) bool {
		return displays[i].ID < displays[j].ID
	})

	return &Snapshot{
		Displays:  displays,
		Signature: BuildSignature(displays),
		paths:     paths,
		modes:     modes,
		modeIndex: modeIndex,
	}, nil
}

// BuildSignature returns a stable, order-independent signature for a set of displays.
func BuildSignature(displays []DisplayInfo) string {
	ids := make([]string, len(displays))
	for i, d := range displays {
		ids[i] = d.ID
	}
	sort.Strings(ids)
	return strings.Join(ids, "|")
}

// ApplyPositions updates source mode positions for the current snapshot if any differ.
// Returns changed=true when SetDisplayConfig was called and succeeded.
func (s *Snapshot) ApplyPositions(target map[string]Position) (bool, error) {
	if len(s.paths) == 0 || len(s.modes) == 0 {
		return false, nil
	}

	changed := false
	for dispID, want := range target {
		idx, ok := s.modeIndex[dispID]
		if !ok {
			return false, fmt.Errorf("display %q missing from active topology", dispID)
		}
		srcMode := (*displayConfigSourceMode)(unsafe.Pointer(&s.modes[idx].Union[0]))
		if srcMode.PosX != want.X || srcMode.PosY != want.Y {
			srcMode.PosX = want.X
			srcMode.PosY = want.Y
			changed = true
		}
	}

	if !changed {
		return false, nil
	}

	flags := uint32(sdcApply | sdcUseSuppliedDisplayConfig | sdcAllowChanges | sdcSaveToDatabase)
	ret, _, _ := procSetDisplayConfig.Call(
		uintptr(len(s.paths)),
		uintptr(unsafe.Pointer(&s.paths[0])),
		uintptr(len(s.modes)),
		uintptr(unsafe.Pointer(&s.modes[0])),
		uintptr(flags),
	)
	if ret != errorSuccess {
		return false, fmt.Errorf("SetDisplayConfig failed with Win32 error %d", ret)
	}
	return true, nil
}

func queryRawActive() ([]displayConfigPathInfo, []displayConfigModeInfo, error) {
	for attempt := 0; attempt < 5; attempt++ {
		var numPaths, numModes uint32
		ret, _, _ := procGetDisplayConfigBufferSizes.Call(
			uintptr(qdcOnlyActivePaths),
			uintptr(unsafe.Pointer(&numPaths)),
			uintptr(unsafe.Pointer(&numModes)),
		)
		if ret != errorSuccess {
			return nil, nil, fmt.Errorf("GetDisplayConfigBufferSizes failed with Win32 error %d", ret)
		}
		if numPaths == 0 || numModes == 0 {
			return nil, nil, nil
		}

		paths := make([]displayConfigPathInfo, numPaths)
		modes := make([]displayConfigModeInfo, numModes)

		ret, _, _ = procQueryDisplayConfig.Call(
			uintptr(qdcOnlyActivePaths),
			uintptr(unsafe.Pointer(&numPaths)),
			uintptr(unsafe.Pointer(&paths[0])),
			uintptr(unsafe.Pointer(&numModes)),
			uintptr(unsafe.Pointer(&modes[0])),
			0,
		)
		if ret == errorInsufficientBuffer {
			continue
		}
		if ret != errorSuccess {
			return nil, nil, fmt.Errorf("QueryDisplayConfig failed with Win32 error %d", ret)
		}
		return paths[:numPaths], modes[:numModes], nil
	}
	return nil, nil, fmt.Errorf("QueryDisplayConfig failed after retries due to buffer size changes")
}

func getTargetDeviceName(adapter luid, targetID uint32) (friendlyName, devicePath string, mfgID, prodID uint16) {
	var info displayConfigTargetDeviceName
	info.Header.InfoType = displayConfigDeviceInfoGetTargetName
	info.Header.Size = uint32(unsafe.Sizeof(info))
	info.Header.AdapterID = adapter
	info.Header.ID = targetID

	ret, _, _ := procDisplayConfigGetDeviceInfo.Call(uintptr(unsafe.Pointer(&info)))
	if ret != errorSuccess {
		return "", "", 0, 0
	}
	return syscall.UTF16ToString(info.MonitorFriendlyDeviceName[:]),
		syscall.UTF16ToString(info.MonitorDevicePath[:]),
		info.EdidManufactureID,
		info.EdidProductCodeID
}

// buildDisplayID extracts a reboot-stable MonitorHWID:TargetUID identifier.
// Example devicePath: \\?\DISPLAY#BOE1600#5&614b76b&0&UID4356#{e6f07b5f-...}
func buildDisplayID(devicePath string, mfgID, prodID uint16, targetID uint32) string {
	parts := strings.Split(devicePath, "#")
	if len(parts) >= 2 && parts[1] != "" {
		return fmt.Sprintf("%s:%d", strings.ToUpper(parts[1]), targetID)
	}
	return fmt.Sprintf("%04X%04X:%d", mfgID, prodID, targetID)
}
