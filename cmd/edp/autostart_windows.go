//go:build windows

package main

import (
	"fmt"
	"os"
	"path/filepath"
	"syscall"
	"unsafe"
)

const (
	hkeyCurrentUser = uintptr(0x80000001)
	keyRead         = 0x20019
	keySetValue     = 0x0002
	regSZ           = 1
	runKeyPath      = `Software\Microsoft\Windows\CurrentVersion\Run`
	runValueName    = "ExternalDisplayPosition"
)

var (
	advapi32           = syscall.NewLazyDLL("advapi32.dll")
	procRegOpenKeyExW  = advapi32.NewProc("RegOpenKeyExW")
	procRegQueryValueW = advapi32.NewProc("RegQueryValueExW")
	procRegSetValueW   = advapi32.NewProc("RegSetValueExW")
	procRegDeleteValW  = advapi32.NewProc("RegDeleteValueW")
	procRegCloseKey    = advapi32.NewProc("RegCloseKey")
)

func handleAutostart(args []string, configPath string, isCustomConfig bool) error {
	action := "status"
	if len(args) > 0 {
		action = args[0]
	}

	switch action {
	case "status":
		val, ok, err := getAutostartValue()
		if err != nil {
			return err
		}
		if !ok {
			fmt.Println("Autostart: disabled")
			return nil
		}
		fmt.Printf("Autostart: enabled (%s)\n", val)
		return nil

	case "enable":
		exePath, err := os.Executable()
		if err != nil {
			return err
		}
		edpwPath := filepath.Join(filepath.Dir(exePath), "edpw.exe")
		if _, err := os.Stat(edpwPath); err != nil {
			return fmt.Errorf("expected %q next to edp.exe (build edpw.exe first): %w", edpwPath, err)
		}
		cmd := fmt.Sprintf(`"%s"`, edpwPath)
		if isCustomConfig {
			absCfg, err := filepath.Abs(configPath)
			if err != nil {
				return err
			}
			cmd = fmt.Sprintf(`"%s" --config "%s"`, edpwPath, absCfg)
		}
		if err := setAutostartValue(cmd); err != nil {
			return err
		}
		fmt.Printf("Autostart enabled: %s\n", cmd)
		return nil

	case "disable":
		if err := deleteAutostartValue(); err != nil {
			return err
		}
		fmt.Println("Autostart disabled.")
		return nil

	default:
		return fmt.Errorf("unknown autostart action %q (use: enable | disable | status)", action)
	}
}

func getAutostartValue() (string, bool, error) {
	subKey, _ := syscall.UTF16PtrFromString(runKeyPath)
	valName, _ := syscall.UTF16PtrFromString(runValueName)

	var hKey uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(subKey)), 0, keyRead, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return "", false, fmt.Errorf("RegOpenKeyExW failed: Win32 error %d", ret)
	}
	defer procRegCloseKey.Call(hKey)

	var valType, bufSize uint32
	ret, _, _ = procRegQueryValueW.Call(hKey, uintptr(unsafe.Pointer(valName)), 0, uintptr(unsafe.Pointer(&valType)), 0, uintptr(unsafe.Pointer(&bufSize)))
	if ret == 2 { // ERROR_FILE_NOT_FOUND
		return "", false, nil
	}
	if ret != 0 {
		return "", false, fmt.Errorf("RegQueryValueExW failed: Win32 error %d", ret)
	}

	buf := make([]uint16, (bufSize+1)/2)
	ret, _, _ = procRegQueryValueW.Call(hKey, uintptr(unsafe.Pointer(valName)), 0, uintptr(unsafe.Pointer(&valType)), uintptr(unsafe.Pointer(&buf[0])), uintptr(unsafe.Pointer(&bufSize)))
	if ret != 0 {
		return "", false, fmt.Errorf("RegQueryValueExW read failed: Win32 error %d", ret)
	}
	return syscall.UTF16ToString(buf), true, nil
}

func setAutostartValue(command string) error {
	subKey, _ := syscall.UTF16PtrFromString(runKeyPath)
	valName, _ := syscall.UTF16PtrFromString(runValueName)
	data, err := syscall.UTF16FromString(command)
	if err != nil {
		return err
	}

	var hKey uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(subKey)), 0, keySetValue, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW failed: Win32 error %d", ret)
	}
	defer procRegCloseKey.Call(hKey)

	byteLen := uint32(len(data) * 2)
	ret, _, _ = procRegSetValueW.Call(hKey, uintptr(unsafe.Pointer(valName)), 0, regSZ, uintptr(unsafe.Pointer(&data[0])), uintptr(byteLen))
	if ret != 0 {
		return fmt.Errorf("RegSetValueExW failed: Win32 error %d", ret)
	}
	return nil
}

func deleteAutostartValue() error {
	subKey, _ := syscall.UTF16PtrFromString(runKeyPath)
	valName, _ := syscall.UTF16PtrFromString(runValueName)

	var hKey uintptr
	ret, _, _ := procRegOpenKeyExW.Call(hkeyCurrentUser, uintptr(unsafe.Pointer(subKey)), 0, keySetValue, uintptr(unsafe.Pointer(&hKey)))
	if ret != 0 {
		return fmt.Errorf("RegOpenKeyExW failed: Win32 error %d", ret)
	}
	defer procRegCloseKey.Call(hKey)

	ret, _, _ = procRegDeleteValW.Call(hKey, uintptr(unsafe.Pointer(valName)))
	if ret != 0 && ret != 2 {
		return fmt.Errorf("RegDeleteValueW failed: Win32 error %d", ret)
	}
	return nil
}
