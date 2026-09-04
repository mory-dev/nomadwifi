package wifi

import (
	"strings"
	"sync"
	"syscall"
	"time"
	"unsafe"

	"github.com/dariomory/nomadwifi/pkg/state"
)

// EventKind identifies an asynchronous change reported by the wireless service.
type EventKind string

const (
	EventConnected     EventKind = "connected"
	EventDisconnected  EventKind = "disconnected"
	EventConnectFailed EventKind = "connect_failed"
	EventScanComplete  EventKind = "scan_complete"
)

// Event is a wireless state change delivered as it happens, rather than
// discovered on the next poll. Reacting to a disconnect takes milliseconds
// this way instead of up to a full polling interval.
type Event struct {
	Kind EventKind
	At   time.Time
}

type nativeClient struct {
	mu       sync.Mutex
	handle   syscall.Handle
	guid     wlanGUID
	open     bool
	events   chan Event
	notified bool
}

var native = &nativeClient{events: make(chan Event, 32)}

// callbackRef keeps the callback alive for the lifetime of the process;
// the wireless service invokes it from its own thread.
var (
	callbackOnce sync.Once
	callbackPtr  uintptr
)

func (c *nativeClient) ensure() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.open {
		return nil
	}

	h, err := wlanOpenHandle()
	if err != nil {
		return err
	}
	guid, err := wlanPrimaryInterface(h)
	if err != nil {
		wlanCloseHandle(h)
		return err
	}

	c.handle = h
	c.guid = guid
	c.open = true
	return nil
}

// NativeAvailable reports whether the Native Wifi API can be used on this
// machine. When it cannot, every caller falls back to the netsh code path.
func NativeAvailable() bool {
	return native.ensure() == nil
}

// Events returns the stream of wireless state changes. The channel is shared
// and buffered; a slow consumer drops events rather than blocking the service
// callback.
func Events() <-chan Event {
	if err := native.ensure(); err == nil {
		native.startNotifications()
	}
	return native.events
}

func (c *nativeClient) startNotifications() {
	c.mu.Lock()
	if c.notified {
		c.mu.Unlock()
		return
	}
	c.notified = true
	handle := c.handle
	c.mu.Unlock()

	callbackOnce.Do(func() {
		callbackPtr = syscall.NewCallback(notificationCallback)
	})

	var prevSource uint32
	procWlanRegisterNotification.Call(
		uintptr(handle),
		uintptr(wlanNotificationSourceACM),
		1, // bIgnoreDuplicate
		callbackPtr,
		0,
		0,
		uintptr(unsafe.Pointer(&prevSource)),
	)
}

// notificationCallback runs on a wireless-service thread. It must not block,
// so it only translates the code and posts to a buffered channel.
func notificationCallback(data *wlanNotificationData, _ uintptr) uintptr {
	if data == nil || data.NotificationSource != wlanNotificationSourceACM {
		return 0
	}

	var kind EventKind
	switch data.NotificationCode {
	case wlanNotificationACMDisconnected:
		kind = EventDisconnected
	case wlanNotificationACMConnectionComplete:
		kind = EventConnected
	case wlanNotificationACMConnectionAttemptFail:
		kind = EventConnectFailed
	case wlanNotificationACMScanComplete:
		kind = EventScanComplete
	default:
		return 0
	}

	select {
	case native.events <- Event{Kind: kind, At: time.Now()}:
	default: // consumer is behind; dropping is better than stalling the service
	}
	return 0
}

// TriggerScan asks the driver to perform a fresh scan. Windows rate-limits
// scans, so this is best-effort; the caller still gets the cached list.
func TriggerScan() error {
	if err := native.ensure(); err != nil {
		return err
	}
	native.mu.Lock()
	h, guid := native.handle, native.guid
	native.mu.Unlock()
	return wlanTriggerScan(h, &guid)
}

// nativeScan reads every known BSSID from the driver and joins it with the
// per-SSID security information.
func nativeScan() ([]AccessPoint, error) {
	if err := native.ensure(); err != nil {
		return nil, err
	}

	native.mu.Lock()
	h, guid := native.handle, native.guid
	native.mu.Unlock()

	bssList, err := wlanBSSEntries(h, &guid)
	if err != nil {
		return nil, err
	}

	security := networkSecurityBySSID(h, &guid)

	aps := make([]AccessPoint, 0, len(bssList))
	for i := range bssList {
		e := &bssList[i].entry

		ssid := e.SSID.String()
		if strings.TrimSpace(ssid) == "" {
			ssid = HiddenSSID
		}

		ap := AccessPoint{
			SSID:  ssid,
			BSSID: formatBSSID(e.BSSID),
			RSSI:  int(e.RSSI),
			// Derived from RSSI rather than taken from uLinkQuality: see
			// signalPercentFromRSSI for why the driver's value is not comparable
			// across the associated AP and its neighbours.
			SignalPercent: signalPercentFromRSSI(int(e.RSSI)),
			FrequencyKHz:  e.ChCenterFrequency,
			Band:          bandFromFrequencyKHz(e.ChCenterFrequency),
			Channel:       channelFromFrequencyKHz(e.ChCenterFrequency),
			RadioType:     phyTypeName(e.BSSPhyType),
		}

		if stations, util, ok := parseBSSLoad(bssList[i].ies); ok {
			ap.StationCount = stations
			ap.ChannelUtilization = util
			ap.HasChannelUtil = true
		}

		if sec, ok := security[strings.ToLower(ssid)]; ok {
			ap.Authentication = sec.auth
			ap.Cipher = sec.cipher
			if ap.RadioType == "" {
				ap.RadioType = sec.phy
			}
		}

		aps = append(aps, ap)
	}

	return aps, nil
}

type networkSecurity struct {
	auth   string
	cipher string
	phy    string
}

// networkSecurityBySSID builds an SSID-keyed view of the auth and cipher
// algorithms, which the per-BSSID list does not carry.
func networkSecurityBySSID(h syscall.Handle, guid *wlanGUID) map[string]networkSecurity {
	out := map[string]networkSecurity{}
	networks, err := wlanAvailableNetworks(h, guid)
	if err != nil {
		return out
	}
	for i := range networks {
		n := &networks[i]
		ssid := n.SSID.String()
		if strings.TrimSpace(ssid) == "" {
			ssid = HiddenSSID
		}
		phy := ""
		if n.NumberOfPhyTypes > 0 {
			// The list is ordered slowest-first; the last entry is the best
			// standard this network supports.
			idx := n.NumberOfPhyTypes - 1
			if idx > 7 {
				idx = 7
			}
			phy = phyTypeName(n.PhyTypes[idx])
		}
		out[strings.ToLower(ssid)] = networkSecurity{
			auth:   authAlgorithmName(n.DefaultAuthAlgorithm),
			cipher: cipherAlgorithmName(n.DefaultCipherAlgorithm),
			phy:    phy,
		}
	}
	return out
}

// NativeDisconnect drops the association without spawning a process.
func NativeDisconnect() error {
	if err := native.ensure(); err != nil {
		return err
	}
	native.mu.Lock()
	h, guid := native.handle, native.guid
	native.mu.Unlock()
	r, _, _ := procWlanDisconnect.Call(uintptr(h), uintptr(unsafe.Pointer(&guid)), 0)
	return wlanErr("WlanDisconnect", r)
}

// nativeLinkStatus reads the live association through the Native Wifi API.
// This replaces a ~340ms netsh launch on every status query, which matters
// because the GUI polls status continuously.
func nativeLinkStatus() (*InterfaceStatus, error) {
	if err := native.ensure(); err != nil {
		return nil, err
	}

	native.mu.Lock()
	h, guid := native.handle, native.guid
	native.mu.Unlock()

	attrs, err := wlanCurrentConnection(h, &guid)
	if err != nil {
		return nil, err
	}

	status := &InterfaceStatus{
		InterfaceName: interfaceFriendlyName(),
		Description:   wlanInterfaceName(h, &guid),
		Connected:     attrs.State == wlanInterfaceStateConnected,
	}
	if !status.Connected {
		return status, nil
	}

	a := attrs.Association
	status.SSID = a.SSID.String()
	status.BSSID = formatBSSID(a.BSSID)
	status.RadioType = phyTypeName(a.PhyType)
	status.SignalPercent = int(a.SignalQuality)
	status.RxMbps = int(a.RxRateKbps / 1000)
	status.TxMbps = int(a.TxRateKbps / 1000)

	// Frequency, band, channel and true RSSI come from the BSS entry we are
	// associated with; the connection attributes do not carry them.
	if entries, err := wlanBSSEntries(h, &guid); err == nil {
		for i := range entries {
			e := &entries[i].entry
			if formatBSSID(e.BSSID) == status.BSSID {
				status.Band = bandFromFrequencyKHz(e.ChCenterFrequency)
				status.Channel = channelFromFrequencyKHz(e.ChCenterFrequency)
				status.RSSI = int(e.RSSI)
				status.SignalPercent = signalPercentFromRSSI(int(e.RSSI))
				break
			}
		}
	}
	if status.Band == "" || status.Band == BandOther {
		status.Band = bandFromChannel(status.Channel)
	}

	return status, nil
}

var (
	ifaceNameOnce  sync.Once
	ifaceNameValue string
)

// interfaceFriendlyName resolves the adapter's connection name (typically
// "Wi-Fi"), which IP-level queries need. It is stable for the life of the
// adapter, so it is resolved once and reused.
func interfaceFriendlyName() string {
	ifaceNameOnce.Do(func() {
		// Remembered across runs so a one-shot CLI invocation does not pay for
		// a netsh launch just to learn a name that never changes.
		if cached := state.InterfaceName(); cached != "" {
			ifaceNameValue = cached
			return
		}

		ifaceNameValue = "Wi-Fi"
		cmd := SilentCommand("netsh", "wlan", "show", "interfaces")
		out, err := cmd.Output()
		if err == nil {
			if s := parseInterfaceStatus(out); s != nil && s.InterfaceName != "" {
				ifaceNameValue = s.InterfaceName
			}
		}
		state.SetInterfaceName(ifaceNameValue)
	})
	return ifaceNameValue
}
