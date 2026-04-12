package main

import (
	"context"
	"encoding/binary"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"tinygo.org/x/bluetooth"
)

type BluetoothDeviceEnt struct {
	Address     string
	AddressType string
	Name        string
	FixedAddr   bool
	MinRSSI     int
	MaxRSSI     int
	RSSI        int
	Info        string
	Count       int
	Code        uint16
	SBType      uint8
	EnvData     []byte
	UUIDMap     map[string]bool
	FirstTime   int64
	LastTime    int64
}

func (d *BluetoothDeviceEnt) String() string {
	return fmt.Sprintf("type=Device,address=%s,name=%s,rssi=%d,min=%d,max=%d,addrType=%s,vendor=%s,info=%s,uuid=%s,ft=%s,lt=%s",
		d.Address, d.Name, d.RSSI, d.MinRSSI, d.MaxRSSI,
		d.AddressType, getVendor(d), d.Info, getUUID(d),
		time.Unix(d.FirstTime, 0).Format(time.RFC3339),
		time.Unix(d.LastTime, 0).Format(time.RFC3339),
	)
}

var deviceMap sync.Map
var total = 0
var skip = 0

type MotionSensorEnt struct {
	Address      string
	Moving       bool
	LastMove     int64
	LastMoveDiff int64
	Battery      int
	Light        bool
}

var motionSensorMap sync.Map

// startBlueScan : start scan
func startBlueScan(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	a := bluetooth.DefaultAdapter
	if err := a.Enable(); err != nil {
		log.Fatalf("start bluescan err=%v", err)
	}
	log.Println("start bluescan")
	timer := time.NewTicker(time.Second * time.Duration(syslogInterval))
	defer timer.Stop()

	go func() {
		for {
			select {
			case <-timer.C:
				sendMonitor()
				sendReport()
			case <-ctx.Done():
				a.StopScan()
				log.Println("stop bluetooth scan")
				return
			}
		}
	}()

	err := a.Scan(func(adapter *bluetooth.Adapter, result bluetooth.ScanResult) {
		checkBlueDevice(result)
	})
	if err != nil {
		log.Printf("scan error: %v", err)
	}
}

func checkBlueDevice(r bluetooth.ScanResult) {
	rssi := int(r.RSSI)
	if rssi == 0 || rssi == 127 {
		skip++
		return
	}
	total++
	now := time.Now().Unix()
	addr := r.Address.String()
	if v, ok := deviceMap.Load(addr); ok {
		if d, ok := v.(*BluetoothDeviceEnt); ok {
			d.RSSI = rssi
			if d.RSSI > d.MaxRSSI {
				d.MaxRSSI = d.RSSI
			}
			if d.RSSI < d.MinRSSI {
				d.MinRSSI = d.RSSI
			}
			checkDeviceInfo(d, r)
			d.Count++
			d.LastTime = now
			return
		} else {
			deviceMap.Delete(addr)
		}
	}
	d := &BluetoothDeviceEnt{
		Address:   addr,
		RSSI:      rssi,
		MinRSSI:   rssi,
		MaxRSSI:   rssi,
		Count:     1,
		UUIDMap:   make(map[string]bool),
		FirstTime: now,
		LastTime:  now,
	}
	checkDeviceInfo(d, r)
	deviceMap.Store(addr, d)
}

func getVendor(d *BluetoothDeviceEnt) string {
	if d.Code != 0x0000 {
		if v, ok := codeToVendorMap[d.Code]; ok {
			return fmt.Sprintf("%s(0x%04x)", v, d.Code)
		}
	}
	return getVendorFromAddress(d.Address)
}

func checkDeviceInfo(d *BluetoothDeviceEnt, r bluetooth.ScanResult) {
	if d.AddressType == "" {
		setAddrType(d, r.Address)
	}
	name := r.LocalName()
	if name != "" {
		d.Name = name
	}

	for _, u := range r.ServiceUUIDs() {
		d.UUIDMap[u.String()] = true
	}

	for _, md := range r.ManufacturerData() {
		code := md.CompanyID
		data := md.Data
		fullData := make([]byte, 2+len(data))
		binary.LittleEndian.PutUint16(fullData, code)
		copy(fullData[2:], data)

		if isInkbird(d.Name) || isInkbird(name) {
			if len(fullData) == 9 || len(fullData) == 18 || (len(fullData) == 17 && fullData[0] == 0x54 && fullData[1] == 0x32) {
				d.EnvData = fullData
				code = 0
				d.Code = 0
			}
		}

		switch code {
		case 0x02d5:
			if len(data) >= 16 {
				d.EnvData = data
			}
		case 0x0969:
			if len(data) >= 12 {
				d.EnvData = data[7:]
			}
		case 0x004c, 0x0006:
		case 0x1c03, 0x1d03:
		case 0x0087:
		case 0x01a9:
		default:
			if code != 0 && debug {
				log.Printf("AdManufacturerSpecific code=%04x data=%x d=%+v", code, data, d)
			}
		}
		if code != 0x0000 {
			d.Code = code
		}
	}

	for _, sd := range r.ServiceData() {
		uuidStr := sd.UUID.String()
		data := sd.Data
		if strings.HasPrefix(uuidStr, "00000d00") {
			if len(data) == 6 && data[0] == 0x54 {
				d.EnvData = make([]byte, 2+len(data))
				d.EnvData[0] = 0x00
				d.EnvData[1] = 0x0d
				copy(d.EnvData[2:], data)
			}
		} else if strings.HasPrefix(uuidStr, "0000fd3d") {
			if len(data) == 6 && data[0] == 0x73 {
				t := int64(data[3])*256 + int64(data[4])
				if data[5]&0x80 == 0x80 {
					t += 0x10000
				}
				m := data[1]&0x40 == 0x40
				l := data[5]&0x02 == 0x02
				addr := r.Address.String()
				if v, ok := motionSensorMap.Load(addr); ok {
					if ms, ok := v.(*MotionSensorEnt); ok {
						send := ms.Moving != m
						ms.Battery = int(data[2])
						ms.LastMove = time.Now().Unix() - t
						ms.Light = l
						ms.Moving = m
						ms.LastMoveDiff = t
						if send {
							sendMotionSensor(ms, "change")
						}
					}
				} else {
					ms := &MotionSensorEnt{
						Address:  addr,
						Moving:   m,
						LastMove: time.Now().Unix() - t,
						Light:    l,
						Battery:  int(data[2]),
					}
					motionSensorMap.Store(addr, ms)
					sendMotionSensor(ms, "new")
				}
				d.SBType = 0x73
			} else if d.Code == 0x0969 && len(data) > 0 {
				d.SBType = data[0]
			}
		}
	}

	raw := r.Bytes()
	if len(raw) > 0 {
		info := ""
		for i := 0; i < len(raw); {
			l := int(raw[i])
			if l == 0 {
				break
			}
			if i+1+l > len(raw) {
				break
			}
			typ := raw[i+1]
			data := raw[i+2 : i+1+l]
			if typ == 0x01 && len(data) == 1 {
				info += getInfoFromFlag(int(data[0]))
			}
			i += 1 + l
		}
		if info != "" {
			d.Info = info
		}
	}
}

func getInfoFromFlag(flag int) string {
	ret := ""
	if (flag & 0x01) != 0 {
		ret = "LE Limited"
	}
	if (flag & 0x02) != 0 {
		if ret != "" {
			ret += ";"
		}
		ret += "LE General"
	}
	if (flag & 0x04) != 0 {
		if ret != "" {
			ret += ";"
		}
		ret += "No BR/EDR"
	}
	if (flag & 0x08) != 0 {
		if ret != "" {
			ret += ";"
		}
		ret += "LE & BR/EDR (controller)"
	}
	if (flag & 0x10) != 0 {
		if ret != "" {
			ret += ";"
		}
		ret += "LE & BR/EDR (host)"
	}
	return ret
}

func setAddrType(d *BluetoothDeviceEnt, addr bluetooth.Address) {
	d.FixedAddr = !addr.IsRandom()
	if addr.IsRandom() {
		d.AddressType = "random"
	} else {
		d.AddressType = "public"
	}
}

func sendOMRONEnv(d *BluetoothDeviceEnt) {
	seq := int(d.EnvData[1])
	temp := float64(int(d.EnvData[3])*256+int(d.EnvData[2])) * 0.01
	hum := float64(int(d.EnvData[5])*256+int(d.EnvData[4])) * 0.01
	lx := int(d.EnvData[7])*256 + int(d.EnvData[6])
	press := float64(int(d.EnvData[11])*(256*256*256)+int(d.EnvData[10])*(256*256)+int(d.EnvData[9])*256+int(d.EnvData[8])) * 0.001
	sound := float64(int(d.EnvData[13])*256+int(d.EnvData[12])) * 0.01
	v := int(d.EnvData[15])*256 + int(d.EnvData[14])
	co2 := int(d.EnvData[17])*256 + int(d.EnvData[16])
	if debug {
		log.Printf("omron seq=%d,temp=%.02f,hum=%.02f,lx=%d,press=%.02f,sound=%.02f,eTVOC=%d,eCO2=%d",
			seq, temp, hum, lx, press, sound, v, co2)
	}
	sendSyslog(fmt.Sprintf("type=OMRONEnv,address=%s,name=%s,rssi=%d,seq=%d,temp=%.02f,hum=%.02f,lx=%d,press=%.02f,sound=%.02f,eTVOC=%d,eCO2=%d",
		d.Address, d.Name, d.RSSI,
		seq, temp, hum, lx, press, sound, v, co2,
	))
	publishMQTT(&mqttEnvDataEnt{
		Time:        time.Now().Format(time.RFC3339),
		Address:     d.Address,
		Name:        d.Name,
		Type:        "OMRONEnv",
		RSSI:        d.RSSI,
		Temperature: temp,
		Humidity:    hum,
		Co2:         co2,
		Lux:         lx,
		Pressure:    press,
		Sound:       sound,
		TVOC:        v,
	})
}

func sendSwitchBotEnv(d *BluetoothDeviceEnt) {
	bat := int(d.EnvData[4] & 0x7f)
	temp := float64(int(d.EnvData[5]&0x0f))/10.0 + float64(d.EnvData[6]&0x7f)
	if (d.EnvData[6] & 0x80) != 0x80 {
		temp *= -1.0
	}
	hum := float64(int(d.EnvData[7] & 0x7f))
	if debug {
		log.Printf("switchbot temp=%.02f,hum=%.02f,bat=%d", temp, hum, bat)
	}
	sendSyslog(fmt.Sprintf("type=SwitchBotEnv,address=%s,name=%s,rssi=%d,temp=%.02f,hum=%.02f,bat=%d",
		d.Address, d.Name, d.RSSI,
		temp, hum, bat,
	))
	publishMQTT(&mqttEnvDataEnt{
		Time:        time.Now().Format(time.RFC3339),
		Address:     d.Address,
		Name:        d.Name,
		Type:        "SwitchBotEnv",
		RSSI:        d.RSSI,
		Temperature: temp,
		Humidity:    hum,
		Battery:     bat,
	})
}

func sendSwitchBotCo2(d *BluetoothDeviceEnt) {
	if len(d.EnvData) < 8 {
		return
	}
	bat := int(d.EnvData[0] & 0x7f)
	temp := float64(int(d.EnvData[1]&0x0f))/10.0 + float64(d.EnvData[2]&0x7f)
	if (d.EnvData[2] & 0x80) != 0x80 {
		temp *= -1.0
	}
	hum := float64(int(d.EnvData[3] & 0x7f))
	co2 := int(d.EnvData[6])*256 + int(d.EnvData[7])
	if debug {
		log.Printf("switchbot temp=%.02f,hum=%.02f,co2=%d,bat=%d", temp, hum, co2, bat)
	}
	sendSyslog(fmt.Sprintf("type=SwitchBotEnv,address=%s,name=%s,rssi=%d,temp=%.02f,hum=%.02f,co2=%d,bat=%d",
		d.Address, d.Name, d.RSSI,
		temp, hum, co2, bat,
	))
	publishMQTT(&mqttEnvDataEnt{
		Time:        time.Now().Format(time.RFC3339),
		Address:     d.Address,
		Name:        d.Name,
		Type:        "SwitchBotEnv",
		RSSI:        d.RSSI,
		Temperature: temp,
		Humidity:    hum,
		Co2:         co2,
		Battery:     bat,
	})
}

func sendSwitchBotIP64(d *BluetoothDeviceEnt) {
	if len(d.EnvData) < 5 {
		return
	}
	bat := int(d.EnvData[0] & 0x7f)
	temp := float64(int(d.EnvData[1]&0x0f))/10.0 + float64(d.EnvData[2]&0x7f)
	if (d.EnvData[2] & 0x80) != 0x80 {
		temp *= -1.0
	}
	hum := float64(int(d.EnvData[3] & 0x7f))
	if debug {
		log.Printf("switchbot temp=%.02f,hum=%.02f,bat=%d", temp, hum, bat)
	}
	sendSyslog(fmt.Sprintf("type=SwitchBotEnv,address=%s,name=%s,rssi=%d,temp=%.02f,hum=%.02f,bat=%d",
		d.Address, d.Name, d.RSSI,
		temp, hum, bat,
	))
	publishMQTT(&mqttEnvDataEnt{
		Time:        time.Now().Format(time.RFC3339),
		Address:     d.Address,
		Name:        d.Name,
		Type:        "SwitchBotEnv",
		RSSI:        d.RSSI,
		Temperature: temp,
		Humidity:    hum,
		Battery:     bat,
	})
}

func sendSwitchBotPlugMini(d *BluetoothDeviceEnt) {
	sw := d.EnvData[0] == 0x80
	over := (d.EnvData[3] & 0x80) == 0x80
	load := int(d.EnvData[3]&0x7f)*256 + int(d.EnvData[4]&0x7f)
	if debug {
		log.Printf("switchbot miniplug sw=%v,over=%v,load=%d", sw, over, load)
	}
	sendSyslog(fmt.Sprintf("type=SwitchBotPlugMini,address=%s,name=%s,rssi=%d,sw=%v,over=%v,load=%d",
		d.Address, d.Name, d.RSSI,
		sw, over, load,
	))
	publishMQTT(&mqttPowerMonitorPlugDataEnt{
		Time:    time.Now().Format(time.RFC3339),
		Address: d.Address,
		Name:    d.Name,
		Type:    "SwitchBotPlugMini",
		RSSI:    d.RSSI,
		Switch:  sw,
		Over:    over,
		Load:    load,
	})
}

func isInkbird(name string) bool {
	n := strings.ToLower(name)
	return strings.HasPrefix(n, "sps") ||
		strings.HasPrefix(n, "tps") ||
		strings.HasPrefix(n, "ibs-") ||
		strings.HasPrefix(n, "ith-") ||
		strings.HasPrefix(n, "ink@iam-")
}

func sendInkbirdEnv(d *BluetoothDeviceEnt) {
	if len(d.EnvData) < 8 {
		return
	}
	var temp, hum float64
	bat := -1
	co2 := 0

	if len(d.EnvData) == 9 {
		tempRaw := int16(uint16(d.EnvData[0]) | (uint16(d.EnvData[1]) << 8))
		humRaw := uint16(d.EnvData[2]) | (uint16(d.EnvData[3]) << 8)
		bat = int(d.EnvData[7])
		temp = float64(tempRaw) / 100.0
		hum = float64(humRaw) / 100.0
	} else if len(d.EnvData) == 18 {
		tempRaw := int16(uint16(d.EnvData[6]) | (uint16(d.EnvData[7]) << 8))
		humRaw := uint16(d.EnvData[8]) | (uint16(d.EnvData[9]) << 8)
		bat = int(d.EnvData[10])
		temp = float64(tempRaw) / 100.0
		hum = float64(humRaw) / 100.0
	} else if len(d.EnvData) == 17 {
		status := d.EnvData[9]
		tempRaw := int16((uint16(d.EnvData[10]) << 8) | uint16(d.EnvData[11]))
		humRaw := (uint16(d.EnvData[12]) << 8) | uint16(d.EnvData[13])
		co2 = int((uint16(d.EnvData[14]) << 8) | uint16(d.EnvData[15]))
		tempF := float64(tempRaw) / 10.0
		if (status & 0x02) != 0 {
			temp = (tempF - 32) * 5.0 / 9.0
		} else {
			temp = tempF
		}
		hum = float64(humRaw) / 10.0
	} else {
		return
	}

	if debug {
		log.Printf("inkbird type=InkbirdEnv,temp=%.02f,hum=%.02f,bat=%d,co2=%d", temp, hum, bat, co2)
	}

	msg := fmt.Sprintf("type=InkbirdEnv,address=%s,name=%s,rssi=%d,temp=%.02f,hum=%.02f",
		d.Address, d.Name, d.RSSI, temp, hum)
	if bat >= 0 {
		msg += fmt.Sprintf(",bat=%d", bat)
	}
	if co2 > 0 {
		msg += fmt.Sprintf(",co2=%d", co2)
	}
	sendSyslog(msg)

	publishMQTT(&mqttEnvDataEnt{
		Time:        time.Now().Format(time.RFC3339),
		Address:     d.Address,
		Name:        d.Name,
		Type:        "InkbirdEnv",
		RSSI:        d.RSSI,
		Temperature: temp,
		Humidity:    hum,
		Battery:     bat,
		Co2:         co2,
	})
}

func sendMotionSensor(ms *MotionSensorEnt, event string) {
	var d *BluetoothDeviceEnt
	if v, ok := deviceMap.Load(ms.Address); !ok {
		return
	} else {
		if d, ok = v.(*BluetoothDeviceEnt); !ok {
			return
		}
	}
	if debug {
		log.Printf("switchbot motion sensor %s %+v %+v", event, d, ms)
	}
	sendSyslog(fmt.Sprintf("type=SwitchBotMotionSensor,address=%s,name=%s,rssi=%d,moving=%v,event=%s,lastMoveDiff=%d,lastMove=%s,battery=%d,light=%v",
		ms.Address, d.Name, d.RSSI, ms.Moving, event, ms.LastMoveDiff, time.Unix(ms.LastMove, 0).Format(time.RFC3339), ms.Battery, ms.Light))
	publishMQTT(&mqttMotionSensorDataEnt{
		Time:         time.Now().Format(time.RFC3339),
		Address:      ms.Address,
		Name:         d.Name,
		Type:         "SwitchBotMotionSensor",
		RSSI:         d.RSSI,
		Moving:       ms.Moving,
		Light:        ms.Light,
		LastMove:     ms.LastMove,
		LastMoveDiff: ms.LastMoveDiff,
		Battery:      ms.Battery,
	})

}

var lastSendTime int64

func sendReport() {
	count := 0
	new := 0
	remove := 0
	omron := 0
	swbot := 0
	inkbird := 0
	report := 0
	junk := 0
	now := time.Now().Unix()
	deviceMap.Range(func(k, v interface{}) bool {
		d, ok := v.(*BluetoothDeviceEnt)
		if !ok {
			return true
		}
		important := d.Name != "" || d.FixedAddr || len(d.EnvData) > 0
		if (!important && d.LastTime < now-15*60+10) || d.LastTime < now-60*60*48 {
			deviceMap.Delete(k)
			remove++
			return true
		}
		count++
		if !allAddress && !important {
			junk++
			return true
		}
		if d.LastTime < lastSendTime {
			return true
		}
		if d.FirstTime > lastSendTime {
			new++
		}
		if strings.HasPrefix(d.Name, "Rbt") && len(d.EnvData) >= 18 && d.EnvData[0] == 1 {
			sendOMRONEnv(d)
			omron++
		} else if len(d.EnvData) == 8 && d.EnvData[0] == 0 && d.EnvData[1] == 0x0d && d.EnvData[2] == 0x54 {
			sendSwitchBotEnv(d)
			swbot++
		} else if d.Code == 0x0969 && len(d.EnvData) >= 4 {
			switch d.SBType {
			case 0x35:
				sendSwitchBotCo2(d)
				swbot++
			case 0x77:
				sendSwitchBotIP64(d)
				swbot++
			default:
				sendSwitchBotPlugMini(d)
				swbot++
			}
		} else if isInkbird(d.Name) && (len(d.EnvData) == 9 || len(d.EnvData) == 17 || len(d.EnvData) == 18) {
			sendInkbirdEnv(d)
			inkbird++
		}
		if debug {
			log.Println(d.String())
		}
		sendSyslog(d.String())
		publishMQTT(&mqttDeviceDataEnt{
			Time:        time.Now().Format(time.RFC3339),
			Address:     d.Address,
			Name:        d.Name,
			AddressType: d.AddressType,
			Info:        d.Info,
			Vendor:      getVendor(d),
			UUID:        getUUID(d),
			MinRSSI:     d.MinRSSI,
			MaxRSSI:     d.MaxRSSI,
			RSSI:        d.RSSI,
			Count:       d.Count,
			FirstTime:   time.Unix(d.FirstTime, 0).Format(time.RFC3339),
			LastTime:    time.Unix(d.LastTime, 0).Format(time.RFC3339),
		})
		report++
		return true
	})
	motionSensorMap.Range(func(k, v interface{}) bool {
		if ms, ok := v.(*MotionSensorEnt); ok {
			sendMotionSensor(ms, "report")
		}
		return true
	})
	sendSyslog(fmt.Sprintf("type=Stats,total=%d,count=%d,new=%d,remove=%d,report=%d,junk=%d,send=%d",
		total, count, new, remove, report, junk, syslogCount))
	publishMQTT(&mqttBlueScanStatsDataEnt{
		Time:    time.Now().Format(time.RFC3339),
		Total:   total,
		Count:   count,
		New:     new,
		Remove:  remove,
		Report:  report,
		Adaptor: "default",
		Junk:    junk,
	})
	if debug {
		log.Printf("total=%d skip=%d count=%d new=%d remove=%d omron=%d swbot=%d inkbird=%d send=%d report=%d junk=%d",
			total, skip, count, new, remove, omron, swbot, inkbird, syslogCount, report, junk)
	}
	syslogCount = 0
	lastSendTime = now
}

func getUUID(d *BluetoothDeviceEnt) string {
	var uuids []string
	for u := range d.UUIDMap {
		uuids = append(uuids, u)
	}
	return strings.Join(uuids, ";")
}
