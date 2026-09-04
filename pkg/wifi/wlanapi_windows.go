package wifi

import (
	"fmt"
	"syscall"
	"unsafe"
)

// Raw bindings for the Windows Native Wifi API (wlanapi.dll).
//
// netsh can only report the wireless service's last scan cache: it cannot
// trigger a scan, it reports signal as a percentage rather than dBm, and it
// costs a process launch per call. This API gives forced scans, real RSSI,
// per-BSSID information elements, and asynchronous connect/disconnect events.

var (
	modwlanapi = syscall.NewLazyDLL("wlanapi.dll")

	procWlanOpenHandle             = modwlanapi.NewProc("WlanOpenHandle")
	procWlanCloseHandle            = modwlanapi.NewProc("WlanCloseHandle")
	procWlanFreeMemory             = modwlanapi.NewProc("WlanFreeMemory")
	procWlanEnumInterfaces         = modwlanapi.NewProc("WlanEnumInterfaces")
	procWlanScan                   = modwlanapi.NewProc("WlanScan")
	procWlanGetNetworkBssList      = modwlanapi.NewProc("WlanGetNetworkBssList")
	procWlanGetAvailableNetworkLst = modwlanapi.NewProc("WlanGetAvailableNetworkList")
	procWlanRegisterNotification   = modwlanapi.NewProc("WlanRegisterNotification")
	procWlanDisconnect             = modwlanapi.NewProc("WlanDisconnect")
	procWlanQueryInterface         = modwlanapi.NewProc("WlanQueryInterface")
)

type wlanGUID struct {
	Data1 uint32
	Data2 uint16
	Data3 uint16
	Data4 [8]byte
}

type dot11SSID struct {
	Length uint32
	SSID   [32]byte
}

func (s dot11SSID) String() string {
	n := s.Length
	if n > 32 {
		n = 32
	}
	return string(s.SSID[:n])
}

type wlanInterfaceInfo struct {
	InterfaceGUID        wlanGUID
	InterfaceDescription [256]uint16
	State                uint32
}

type wlanInterfaceInfoList struct {
	NumberOfItems uint32
	Index         uint32
	InterfaceInfo [1]wlanInterfaceInfo
}

type wlanRateSet struct {
	RateSetLength uint32
	RateSet       [126]uint16
}

// wlanBSSEntry mirrors WLAN_BSS_ENTRY. The explicit padding fields reproduce
// the C compiler's alignment; changing the field order will silently corrupt
// every value read after the change.
type wlanBSSEntry struct {
	SSID                  dot11SSID // 0
	PhyID                 uint32    // 36
	BSSID                 [6]byte   // 40
	_                     [2]byte   // 46 -> align 48
	BSSType               uint32    // 48
	BSSPhyType            uint32    // 52
	RSSI                  int32     // 56
	LinkQuality           uint32    // 60
	InRegDomain           uint8     // 64
	_                     [1]byte   // 65
	BeaconPeriod          uint16    // 66
	_                     [4]byte   // 68 -> align 72
	Timestamp             uint64    // 72
	HostTimestamp         uint64    // 80
	CapabilityInformation uint16    // 88
	_                     [2]byte   // 90 -> align 92
	ChCenterFrequency     uint32    // 92
	RateSet               wlanRateSet
	IEOffset              uint32
	IESize                uint32
}

type wlanBSSList struct {
	TotalSize     uint32
	NumberOfItems uint32
	BSSEntries    [1]wlanBSSEntry
}

type wlanAvailableNetwork struct {
	ProfileName            [256]uint16
	SSID                   dot11SSID
	BSSType                uint32
	NumberOfBssids         uint32
	NetworkConnectable     uint32
	NotConnectableReason   uint32
	NumberOfPhyTypes       uint32
	PhyTypes               [8]uint32
	MorePhyTypes           uint32
	SignalQuality          uint32
	SecurityEnabled        uint32
	DefaultAuthAlgorithm   uint32
	DefaultCipherAlgorithm uint32
	Flags                  uint32
	Reserved               uint32
}

type wlanAvailableNetworkList struct {
	NumberOfItems uint32
	Index         uint32
	Network       [1]wlanAvailableNetwork
}

// wlanNotificationData mirrors WLAN_NOTIFICATION_DATA.
type wlanNotificationData struct {
	NotificationSource uint32
	NotificationCode   uint32
	InterfaceGUID      wlanGUID
	DataSize           uint32
	_                  [4]byte
	Data               uintptr
}

const (
	wlanNotificationSourceACM = 0x00000008

	wlanNotificationACMScanComplete          = 7
	wlanNotificationACMScanFail              = 8
	wlanNotificationACMConnectionStart       = 9
	wlanNotificationACMConnectionComplete    = 10
	wlanNotificationACMConnectionAttemptFail = 11
	wlanNotificationACMDisconnecting         = 20
	wlanNotificationACMDisconnected          = 21
)

const (
	dot11BSSTypeInfrastructure = 1
	dot11BSSTypeAny            = 3
)

// DOT11_AUTH_ALGORITHM values.
const (
	authAlgoOpen       = 1
	authAlgoSharedKey  = 2
	authAlgoWPA        = 3
	authAlgoWPAPSK     = 4
	authAlgoWPANone    = 5
	authAlgoRSNA       = 6
	authAlgoRSNAPSK    = 7
	authAlgoWPA3Ent192 = 8
	authAlgoWPA3SAE    = 9
	authAlgoOWE        = 10
	authAlgoWPA3Ent    = 11
)

// DOT11_CIPHER_ALGORITHM values.
const (
	cipherNone    = 0x00
	cipherWEP40   = 0x01
	cipherTKIP    = 0x02
	cipherCCMP    = 0x04
	cipherWEP104  = 0x05
	cipherBIP     = 0x06
	cipherGCMP    = 0x08
	cipherGCMP256 = 0x09
	cipherWEP     = 0x101
)

// DOT11_PHY_TYPE values.
const (
	phyTypeFHSS   = 1
	phyTypeDSSS   = 2
	phyTypeIR     = 3
	phyTypeOFDM   = 4
	phyTypeHRDSSS = 5
	phyTypeERP    = 6
	phyTypeHT     = 7
	phyTypeVHT    = 8
	phyTypeDMG    = 9
	phyTypeHE     = 10
	phyTypeEHT    = 11
)

func wlanErr(op string, r uintptr) error {
	if r == 0 {
		return nil
	}
	return fmt.Errorf("%s failed: %w", op, syscall.Errno(r))
}

func wlanOpenHandle() (syscall.Handle, error) {
	var negotiated uint32
	var handle syscall.Handle
	r, _, _ := procWlanOpenHandle.Call(
		uintptr(2), 0,
		uintptr(unsafe.Pointer(&negotiated)),
		uintptr(unsafe.Pointer(&handle)),
	)
	if err := wlanErr("WlanOpenHandle", r); err != nil {
		return 0, err
	}
	return handle, nil
}

func wlanCloseHandle(h syscall.Handle) {
	procWlanCloseHandle.Call(uintptr(h), 0)
}

func wlanFreeMemory(p unsafe.Pointer) {
	if p != nil {
		procWlanFreeMemory.Call(uintptr(p))
	}
}

// wlanPrimaryInterface returns the GUID of the first wireless interface.
func wlanPrimaryInterface(h syscall.Handle) (wlanGUID, error) {
	var list *wlanInterfaceInfoList
	r, _, _ := procWlanEnumInterfaces.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&list)))
	if err := wlanErr("WlanEnumInterfaces", r); err != nil {
		return wlanGUID{}, err
	}
	defer wlanFreeMemory(unsafe.Pointer(list))

	if list == nil || list.NumberOfItems == 0 {
		return wlanGUID{}, fmt.Errorf("no wireless interface present")
	}

	infos := unsafe.Slice(&list.InterfaceInfo[0], list.NumberOfItems)
	// Prefer a connected interface; otherwise take the first one.
	for _, info := range infos {
		if info.State == 1 { // wlan_interface_state_connected
			return info.InterfaceGUID, nil
		}
	}
	return infos[0].InterfaceGUID, nil
}

// wlanTriggerScan asks the driver for a fresh scan. Windows rate-limits this,
// so a failure is not fatal: the cached BSS list is still returned.
func wlanTriggerScan(h syscall.Handle, guid *wlanGUID) error {
	r, _, _ := procWlanScan.Call(uintptr(h), uintptr(unsafe.Pointer(guid)), 0, 0, 0)
	return wlanErr("WlanScan", r)
}

// nativeBSS is a BSS entry copied out of the API-owned buffer, together with
// its information elements. Both must be captured before WlanFreeMemory runs.
type nativeBSS struct {
	entry wlanBSSEntry
	ies   []byte
}

// wlanBSSEntries returns every BSSID the driver currently knows about.
func wlanBSSEntries(h syscall.Handle, guid *wlanGUID) ([]nativeBSS, error) {
	var list *wlanBSSList
	r, _, _ := procWlanGetNetworkBssList.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(guid)),
		0,
		uintptr(dot11BSSTypeAny),
		0,
		0,
		uintptr(unsafe.Pointer(&list)),
	)
	if err := wlanErr("WlanGetNetworkBssList", r); err != nil {
		return nil, err
	}
	defer wlanFreeMemory(unsafe.Pointer(list))

	if list == nil || list.NumberOfItems == 0 {
		return nil, nil
	}

	src := unsafe.Slice(&list.BSSEntries[0], list.NumberOfItems)
	out := make([]nativeBSS, 0, len(src))
	for i := range src {
		// IEOffset is relative to the start of this entry, so the address has
		// to be taken while the API buffer is still mapped.
		out = append(out, nativeBSS{
			entry: src[i],
			ies:   copyInformationElements(&src[i]),
		})
	}
	return out, nil
}

// copyInformationElements copies the raw IE blob that trails a BSS entry.
func copyInformationElements(e *wlanBSSEntry) []byte {
	if e.IESize == 0 || e.IEOffset == 0 || e.IESize > 8192 {
		return nil
	}
	raw := unsafe.Slice((*byte)(unsafe.Add(unsafe.Pointer(e), e.IEOffset)), e.IESize)
	out := make([]byte, len(raw))
	copy(out, raw)
	return out
}

// wlanAvailableNetworks returns the per-SSID view, which carries the auth and
// cipher algorithms that the per-BSSID list does not.
func wlanAvailableNetworks(h syscall.Handle, guid *wlanGUID) ([]wlanAvailableNetwork, error) {
	var list *wlanAvailableNetworkList
	const includeAllAdhoc = 0x01 | 0x02
	r, _, _ := procWlanGetAvailableNetworkLst.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(guid)),
		uintptr(includeAllAdhoc),
		0,
		uintptr(unsafe.Pointer(&list)),
	)
	if err := wlanErr("WlanGetAvailableNetworkList", r); err != nil {
		return nil, err
	}
	defer wlanFreeMemory(unsafe.Pointer(list))

	if list == nil || list.NumberOfItems == 0 {
		return nil, nil
	}
	src := unsafe.Slice(&list.Network[0], list.NumberOfItems)
	out := make([]wlanAvailableNetwork, len(src))
	copy(out, src)
	return out, nil
}

// authAlgorithmName renders a DOT11_AUTH_ALGORITHM the way netsh labels it, so
// downstream security mapping works identically on both code paths.
func authAlgorithmName(a uint32) string {
	switch a {
	case authAlgoOpen, authAlgoSharedKey:
		return "Open"
	case authAlgoWPA:
		return "WPA-Enterprise"
	case authAlgoWPAPSK, authAlgoWPANone:
		return "WPA-Personal"
	case authAlgoRSNA:
		return "WPA2-Enterprise"
	case authAlgoRSNAPSK:
		return "WPA2-Personal"
	case authAlgoWPA3SAE:
		return "WPA3-Personal"
	case authAlgoOWE:
		return "Open"
	case authAlgoWPA3Ent, authAlgoWPA3Ent192:
		return "WPA3-Enterprise"
	}
	return "Unknown"
}

func cipherAlgorithmName(c uint32) string {
	switch c {
	case cipherNone:
		return "None"
	case cipherWEP40, cipherWEP104, cipherWEP:
		return "WEP"
	case cipherTKIP:
		return "TKIP"
	case cipherCCMP:
		return "CCMP"
	case cipherBIP:
		return "BIP"
	case cipherGCMP, cipherGCMP256:
		return "GCMP"
	}
	return "Unknown"
}

// phyTypeName maps DOT11_PHY_TYPE onto the 802.11 designation used for scoring.
func phyTypeName(p uint32) string {
	switch p {
	case phyTypeEHT:
		return "802.11be"
	case phyTypeHE:
		return "802.11ax"
	case phyTypeVHT:
		return "802.11ac"
	case phyTypeHT:
		return "802.11n"
	case phyTypeERP:
		return "802.11g"
	case phyTypeOFDM:
		return "802.11a"
	case phyTypeHRDSSS, phyTypeDSSS, phyTypeFHSS:
		return "802.11b"
	case phyTypeDMG:
		return "802.11ad"
	}
	return ""
}

func formatBSSID(b [6]byte) string {
	return fmt.Sprintf("%02x:%02x:%02x:%02x:%02x:%02x", b[0], b[1], b[2], b[3], b[4], b[5])
}

// channelFromFrequencyKHz derives the 802.11 channel number from the center
// frequency, which is the only value the BSS list reports.
func channelFromFrequencyKHz(khz uint32) int {
	mhz := int(khz / 1000)
	switch {
	case mhz == 2484:
		return 14
	case mhz >= 2412 && mhz <= 2472:
		return (mhz - 2407) / 5
	case mhz >= 5150 && mhz <= 5895:
		return (mhz - 5000) / 5
	case mhz >= 5925 && mhz <= 7125:
		return (mhz - 5950) / 5
	}
	return 0
}

// parseBSSLoad extracts the 802.11 BSS Load element (ID 11): the station count
// and the fraction of airtime the AP reports as busy. This is the only direct
// measure of how contended an access point actually is.
func parseBSSLoad(ies []byte) (stations int, utilizationPct int, ok bool) {
	for i := 0; i+2 <= len(ies); {
		id := ies[i]
		length := int(ies[i+1])
		body := i + 2
		if body+length > len(ies) {
			break
		}
		if id == 11 && length >= 5 {
			stations = int(ies[body]) | int(ies[body+1])<<8
			// Byte 2 is channel utilization scaled 0-255.
			utilizationPct = int(ies[body+2]) * 100 / 255
			return stations, utilizationPct, true
		}
		i = body + length
	}
	return 0, 0, false
}

// WLAN_INTF_OPCODE values.
const (
	intfOpcodeInterfaceState    = 6
	intfOpcodeCurrentConnection = 7
)

// WLAN_INTERFACE_STATE values.
const wlanInterfaceStateConnected = 1

type wlanAssociationAttributes struct {
	SSID          dot11SSID // 0
	BSSType       uint32    // 36
	BSSID         [6]byte   // 40
	_             [2]byte   // 46 -> align 48
	PhyType       uint32    // 48
	PhyIndex      uint32    // 52
	SignalQuality uint32    // 56
	RxRateKbps    uint32    // 60
	TxRateKbps    uint32    // 64
}

type wlanSecurityAttributes struct {
	SecurityEnabled uint32
	OneXEnabled     uint32
	AuthAlgorithm   uint32
	CipherAlgorithm uint32
}

type wlanConnectionAttributes struct {
	State          uint32
	ConnectionMode uint32
	ProfileName    [256]uint16
	Association    wlanAssociationAttributes
	Security       wlanSecurityAttributes
}

// wlanCurrentConnection reads the live association without spawning netsh.
func wlanCurrentConnection(h syscall.Handle, guid *wlanGUID) (*wlanConnectionAttributes, error) {
	var size uint32
	var data *wlanConnectionAttributes
	r, _, _ := procWlanQueryInterface.Call(
		uintptr(h),
		uintptr(unsafe.Pointer(guid)),
		uintptr(intfOpcodeCurrentConnection),
		0,
		uintptr(unsafe.Pointer(&size)),
		uintptr(unsafe.Pointer(&data)),
		0,
	)
	if err := wlanErr("WlanQueryInterface", r); err != nil {
		return nil, err
	}
	defer wlanFreeMemory(unsafe.Pointer(data))

	if data == nil || size < uint32(unsafe.Sizeof(wlanConnectionAttributes{})) {
		return nil, fmt.Errorf("WlanQueryInterface returned %d bytes, want %d",
			size, unsafe.Sizeof(wlanConnectionAttributes{}))
	}

	// Copy before the API buffer is released.
	attrs := *data
	return &attrs, nil
}

// wlanInterfaceName returns the adapter description, used to scope IP queries.
func wlanInterfaceName(h syscall.Handle, guid *wlanGUID) string {
	var list *wlanInterfaceInfoList
	r, _, _ := procWlanEnumInterfaces.Call(uintptr(h), 0, uintptr(unsafe.Pointer(&list)))
	if wlanErr("WlanEnumInterfaces", r) != nil {
		return ""
	}
	defer wlanFreeMemory(unsafe.Pointer(list))

	if list == nil || list.NumberOfItems == 0 {
		return ""
	}
	infos := unsafe.Slice(&list.InterfaceInfo[0], list.NumberOfItems)
	for _, info := range infos {
		if info.InterfaceGUID == *guid {
			return syscall.UTF16ToString(info.InterfaceDescription[:])
		}
	}
	return ""
}
