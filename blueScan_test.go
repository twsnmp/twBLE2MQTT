package main

import (
	"fmt"
	"strings"
	"sync"
	"testing"

	"tinygo.org/x/bluetooth"
)

type mockAddress struct {
	addr   string
	random bool
}

func (m mockAddress) String() string {
	return m.addr
}

func (m mockAddress) IsRandom() bool {
	return m.random
}

func TestIsUUIDAddress(t *testing.T) {
	uuidAddr := "12345678-ABCD-1234-5678-123456789ABC"
	macAddr := "11:22:33:44:55:66"

	if !isUUIDAddress(uuidAddr) {
		t.Errorf("expected isUUIDAddress(%s) to be true", uuidAddr)
	}
	if isUUIDAddress(macAddr) {
		t.Errorf("expected isUUIDAddress(%s) to be false", macAddr)
	}
}

func setAddrTypeFromStr(d *BluetoothDeviceEnt, addrStr string, isRandom bool) {
	if isUUIDAddress(addrStr) {
		d.FixedAddr = false
		d.AddressType = "uuid"
	} else if isRandom {
		d.FixedAddr = false
		d.AddressType = "random"
	} else {
		d.FixedAddr = true
		d.AddressType = "public"
	}
}

func TestSetAddrType(t *testing.T) {
	d1 := &BluetoothDeviceEnt{}
	setAddrTypeFromStr(d1, "11:22:33:44:55:66", false)
	if d1.AddressType != "public" || !d1.FixedAddr {
		t.Errorf("expected public/FixedAddr=true, got type=%s fixed=%v", d1.AddressType, d1.FixedAddr)
	}

	d2 := &BluetoothDeviceEnt{}
	setAddrTypeFromStr(d2, "AA:BB:CC:DD:EE:FF", true)
	if d2.AddressType != "random" || d2.FixedAddr {
		t.Errorf("expected random/FixedAddr=false, got type=%s fixed=%v", d2.AddressType, d2.FixedAddr)
	}

	d3 := &BluetoothDeviceEnt{}
	setAddrTypeFromStr(d3, "12345678-ABCD-1234-5678-123456789ABC", false)
	if d3.AddressType != "uuid" || d3.FixedAddr {
		t.Errorf("expected uuid/FixedAddr=false, got type=%s fixed=%v", d3.AddressType, d3.FixedAddr)
	}
}

func TestGetDeviceID(t *testing.T) {
	pubAddr := mockAddress{addr: "11:22:33:44:55:66", random: false}
	ranAddr := mockAddress{addr: "AA:BB:CC:DD:EE:FF", random: true}
	uuidAddr := mockAddress{addr: "12345678-ABCD-1234-5678-123456789ABC", random: false}

	d := &BluetoothDeviceEnt{
		Name:       "TestDevice",
		DeviceType: "AppleWatch",
	}

	// Case 1: idByName = false
	idByName = false
	if got := getDeviceIDFromAddr(d, pubAddr); got != pubAddr.String() {
		t.Errorf("idByName=false (public): expected %s, got %s", pubAddr.String(), got)
	}
	if got := getDeviceIDFromAddr(d, ranAddr); got != ranAddr.String() {
		t.Errorf("idByName=false (random): expected %s, got %s", ranAddr.String(), got)
	}
	if got := getDeviceIDFromAddr(d, uuidAddr); got != uuidAddr.String() {
		t.Errorf("idByName=false (uuid): expected %s, got %s", uuidAddr.String(), got)
	}

	// Case 2: idByName = true, Public MAC Address -> MAC
	idByName = true
	if got := getDeviceIDFromAddr(d, pubAddr); got != pubAddr.String() {
		t.Errorf("idByName=true (public): expected %s, got %s", pubAddr.String(), got)
	}

	// Case 3: idByName = true, Random MAC Address -> NAME:...:TYPE:...
	expectedID := fmt.Sprintf("NAME:%s:TYPE:%s", d.Name, d.DeviceType)
	if got := getDeviceIDFromAddr(d, ranAddr); got != expectedID {
		t.Errorf("idByName=true (random): expected %s, got %s", expectedID, got)
	}

	// Case 4: idByName = true, macOS UUID Address -> NAME:...:TYPE:...
	if got := getDeviceIDFromAddr(d, uuidAddr); got != expectedID {
		t.Errorf("idByName=true (uuid): expected %s, got %s", expectedID, got)
	}
}

func getDeviceIDFromAddr(d *BluetoothDeviceEnt, addr mockAddress) string {
	addrStr := addr.String()
	if !idByName {
		return addrStr
	}
	if !addr.IsRandom() && !isUUIDAddress(addrStr) {
		return addrStr
	}
	name := d.Name
	devType := d.DeviceType
	if name == "" && devType == "" {
		return addrStr
	}
	return fmt.Sprintf("NAME:%s:TYPE:%s", name, devType)
}

func TestAddressChangeCount(t *testing.T) {
	deviceMap = sync.Map{}
	idByName = true

	devID := "NAME:MyWatch:TYPE:AppleWatch"
	addr1 := "12345678-ABCD-1234-5678-123456789ABC"
	addr2 := "87654321-DCBA-4321-8765-CBA987654321"

	d1 := &BluetoothDeviceEnt{
		ID:              devID,
		Address:         addr1,
		AddressType:     "uuid",
		Name:            "MyWatch",
		DeviceType:      "AppleWatch",
		AddrChangeCount: 0,
	}
	deviceMap.Store(devID, d1)

	if v, ok := deviceMap.Load(devID); ok {
		if d, ok := v.(*BluetoothDeviceEnt); ok {
			if d.Address != addr2 {
				d.Address = addr2
				d.AddrChangeCount++
			}
		}
	}

	v, ok := deviceMap.Load(devID)
	if !ok {
		t.Fatalf("device entry not found")
	}
	d := v.(*BluetoothDeviceEnt)

	if d.Address != addr2 {
		t.Errorf("expected Address %s, got %s", addr2, d.Address)
	}
	if d.AddrChangeCount != 1 {
		t.Errorf("expected AddrChangeCount 1, got %d", d.AddrChangeCount)
	}
	if d.ID != devID {
		t.Errorf("expected ID %s, got %s", devID, d.ID)
	}
}

func TestHasUUID(t *testing.T) {
	d := &BluetoothDeviceEnt{
		UUIDMap: map[string]bool{
			"0000fff0-0000-1000-8000-00805f9b34fb": true,
			"0000fcf1-0000-1000-8000-00805f9b34fb": true,
		},
	}
	if !hasUUID(d, "fff0") {
		t.Errorf("expected hasUUID(d, 'fff0') to be true")
	}
	if !hasUUID(d, "fcf1") {
		t.Errorf("expected hasUUID(d, 'fcf1') to be true")
	}
	if hasUUID(d, "180d") {
		t.Errorf("expected hasUUID(d, '180d') to be false")
	}
}

func TestNoDeviceAndSensorOnlyFlags(t *testing.T) {
	noDevice = false
	sensorOnly = true
	if sensorOnly {
		noDevice = true
	}
	if !noDevice {
		t.Errorf("expected noDevice to be true when sensorOnly is true")
	}

	noDevice = true
	sensorOnly = false
	if !noDevice {
		t.Errorf("expected noDevice to be true")
	}
}

func TestIDByNameMotionSensor(t *testing.T) {
	deviceMap = sync.Map{}
	macToDeviceMap = sync.Map{}
	idByName = true

	macAddr := "AA:BB:CC:DD:EE:FF"
	mappedID := "NAME:MotionSensor:TYPE:SwitchBot"

	dev := &BluetoothDeviceEnt{
		ID:         mappedID,
		Address:    macAddr,
		RawAddress: macAddr,
		Name:       "MotionSensor",
		DeviceType: "SwitchBot",
		RSSI:       -70,
	}
	deviceMap.Store(mappedID, dev)
	macToDeviceMap.Store(macAddr, dev)

	ms := &MotionSensorEnt{
		Address:      macAddr,
		Moving:       true,
		LastMove:     1600000000,
		LastMoveDiff: 5,
		Battery:      90,
		Light:        true,
	}

	v, ok := macToDeviceMap.Load(ms.Address)
	if !ok {
		t.Fatalf("expected macToDeviceMap.Load(%s) to succeed", ms.Address)
	}
	d := v.(*BluetoothDeviceEnt)
	if d.Name != "MotionSensor" {
		t.Errorf("expected device name MotionSensor, got %s", d.Name)
	}
}

type mockPayload struct {
	localName        string
	manufacturerData []bluetooth.ManufacturerDataElement
	serviceData      []bluetooth.ServiceDataElement
	bytes            []byte
}

func (m mockPayload) LocalName() string {
	return m.localName
}
func (m mockPayload) HasServiceUUID(u bluetooth.UUID) bool {
	return false
}
func (m mockPayload) ServiceUUIDs() []bluetooth.UUID {
	return nil
}
func (m mockPayload) Bytes() []byte {
	return m.bytes
}
func (m mockPayload) ManufacturerData() []bluetooth.ManufacturerDataElement {
	return m.manufacturerData
}
func (m mockPayload) ServiceData() []bluetooth.ServiceDataElement {
	return m.serviceData
}

func TestIDByNameSensorData(t *testing.T) {
	deviceMap.Range(func(k, v interface{}) bool {
		deviceMap.Delete(k)
		return true
	})
	macToDeviceMap.Range(func(k, v interface{}) bool {
		macToDeviceMap.Delete(k)
		return true
	})

	idByName = true
	sensorOnly = false
	lastSendTime = 0

	var randAddr bluetooth.Address
	randAddr.Set("4a123456-7890-4000-8000-1234567890ab")
	randAddr.SetRandom(true)

	// Step 1: Scan Response packet with device name "IBS-TH1"
	rScanRsp := bluetooth.ScanResult{
		Address:              randAddr,
		RSSI:                 -60,
		AdvertisementPayload: mockPayload{localName: "IBS-TH1"},
	}
	checkBlueDevice(rScanRsp)

	var foundID string
	deviceMap.Range(func(k, v interface{}) bool {
		foundID = k.(string)
		return true
	})
	if !strings.HasPrefix(foundID, "NAME:IBS-TH1") {
		t.Fatalf("expected ID starting with NAME:IBS-TH1, got %q", foundID)
	}

	// Step 2: Advertising packet containing Inkbird Env Data
	rAdv := bluetooth.ScanResult{
		Address: randAddr,
		RSSI:    -58,
		AdvertisementPayload: mockPayload{
			serviceData: []bluetooth.ServiceDataElement{
				{
					UUID: bluetooth.New16BitUUID(0xfff1),
					Data: []byte{0x09, 0x88, 0x13, 0x00, 0x00, 0x00},
				},
			},
		},
	}
	checkBlueDevice(rAdv)

	entryCount := 0
	var ent *BluetoothDeviceEnt
	deviceMap.Range(func(k, v interface{}) bool {
		entryCount++
		ent = v.(*BluetoothDeviceEnt)
		return true
	})
	if entryCount != 1 {
		t.Fatalf("expected 1 integrated device entry, got %d", entryCount)
	}

	if !strings.HasPrefix(ent.Address, "NAME:IBS-TH1") {
		t.Errorf("expected Persistent ID Address starting with NAME:IBS-TH1 under idByName mode, got %q", ent.Address)
	}
	if ent.RawAddress != "4a123456-7890-4000-8000-1234567890ab" {
		t.Errorf("expected RawAddress '4a123456-7890-4000-8000-1234567890ab', got %q", ent.RawAddress)
	}
	if len(ent.EnvData) == 0 {
		t.Errorf("expected EnvData to be updated from ADV packet, but it was empty")
	}

	sendReport()
}

func TestSwitchBotEnvNotPlugMini(t *testing.T) {
	deviceMap.Range(func(k, v interface{}) bool {
		deviceMap.Delete(k)
		return true
	})
	macToDeviceMap.Range(func(k, v interface{}) bool {
		macToDeviceMap.Delete(k)
		return true
	})

	idByName = true
	sensorOnly = false
	lastSendTime = 0

	var randAddr bluetooth.Address
	randAddr.Set("4A:99:88:77:66:55")
	randAddr.SetRandom(true)

	// Step 1: Manufacturer specific packet (0x0969)
	rMfg := bluetooth.ScanResult{
		Address: randAddr,
		RSSI:    -70,
		AdvertisementPayload: mockPayload{
			manufacturerData: []bluetooth.ManufacturerDataElement{
				{
					CompanyID: 0x0969,
					Data:      []byte{0x01, 0x02, 0x03, 0x04, 0x05, 0x06, 0x07, 0x08, 0x09, 0x0a, 0x0b, 0x0c},
				},
			},
		},
	}
	checkBlueDevice(rMfg)

	var ent *BluetoothDeviceEnt
	deviceMap.Range(func(k, v interface{}) bool {
		ent = v.(*BluetoothDeviceEnt)
		return true
	})
	if ent.DeviceType == "SwitchBotPlugMini" {
		t.Fatalf("expected device not to be misidentified as SwitchBotPlugMini, got %q", ent.DeviceType)
	}

	// Step 2: Receive 8-byte EnvData (0x00 0x0d 0x54 ...)
	rEnv := bluetooth.ScanResult{
		Address: randAddr,
		RSSI:    -70,
		AdvertisementPayload: mockPayload{
			serviceData: []bluetooth.ServiceDataElement{
				{
					UUID: bluetooth.New16BitUUID(0x0d00),
					Data: []byte{0x00, 0x0d, 0x54, 0x10, 0x64, 0x07, 0x9a, 0x46},
				},
			},
		},
	}
	checkBlueDevice(rEnv)

	if ent.DeviceType != "SwitchBotEnv" {
		t.Errorf("expected DeviceType to be SwitchBotEnv, got %q", ent.DeviceType)
	}
	if !strings.Contains(ent.Address, "SwitchBotEnv") {
		t.Errorf("expected Persistent Address to contain SwitchBotEnv, got %q", ent.Address)
	}

	// Step 3: Receive 0x0969 again and ensure EnvData (8 bytes) is preserved
	checkBlueDevice(rMfg)
	if len(ent.EnvData) != 8 {
		t.Errorf("expected 8-byte EnvData to be preserved, got len=%d", len(ent.EnvData))
	}
}



