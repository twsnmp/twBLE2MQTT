package main

import (
	"fmt"
	"log"
	"time"

	"github.com/shirou/gopsutil/v3/cpu"
	"github.com/shirou/gopsutil/v3/load"
	"github.com/shirou/gopsutil/v3/mem"
	gopsnet "github.com/shirou/gopsutil/v3/net"
	"github.com/shirou/gopsutil/v3/process"
)

var lastMonitorTime int64
var lastBytesRecv uint64
var lastBytesSent uint64

// sendMonitor collects system resource information (CPU, Memory, Load, Network) 
// and sends it via Syslog and MQTT.
func sendMonitor() {
	mqttData := new(mqttMonitorDataEnt)
	msg := "type=Monitor,"
	
	// CPU usage
	cpus, err := cpu.Percent(0, false)
	if err != nil {
		log.Printf("sendMonitor err=%v", err)
		return
	}
	mqttData.CPU = cpus[0]
	msg += fmt.Sprintf("cpu=%.3f", cpus[0])
	
	// Load average
	loads, err := load.Avg()
	if err != nil {
		log.Printf("sendMonitor err=%v", err)
		return
	}
	msg += fmt.Sprintf(",load=%.3f", loads.Load1)
	mqttData.Load = loads.Load1
	
	// Memory usage
	mems, err := mem.VirtualMemory()
	if err != nil {
		log.Printf("sendMonitor err=%v", err)
		return
	}
	msg += fmt.Sprintf(",mem=%.3f", mems.UsedPercent)
	mqttData.Memory = mems.UsedPercent
	
	// Network stats
	nets, err := gopsnet.IOCounters(false)
	if err != nil {
		log.Printf("sendMonitor err=%v", err)
		return
	}
	now := time.Now().Unix()
	if lastMonitorTime > 0 {
		diff := now - lastMonitorTime
		if diff > 0 {
			dSent := nets[0].BytesSent - lastBytesSent
			dRecv := nets[0].BytesRecv - lastBytesRecv
			
			// Calculate speeds in Mbps
			rxSpeed := 8.0 * float64(dRecv) / float64(diff)
			rxSpeed /= (1000 * 1000)
			txSpeed := 8.0 * float64(dSent) / float64(diff)
			txSpeed /= (1000 * 1000)
			
			msg += fmt.Sprintf(",recv=%d,sent=%d,rxSpeed=%.3f,txSpeed=%.3f",
				dRecv, dSent, rxSpeed, txSpeed)
			mqttData.Recv = dRecv
			mqttData.Sent = dSent
			mqttData.TxSpeed = txSpeed
			mqttData.RxSpeed = rxSpeed
		}
	}
	lastMonitorTime = time.Now().Unix()
	lastBytesRecv = nets[0].BytesRecv
	lastBytesSent = nets[0].BytesSent
	
	// Number of processes
	pids, err := process.Pids()
	if err != nil {
		log.Printf("sendMonitor err=%v", err)
		return
	}
	msg += fmt.Sprintf(",process=%d,param=", len(pids))
	mqttData.Process = len(pids)
	mqttData.Time = time.Now().Format(time.RFC3339)
	
	// Send report
	sendSyslog(msg)
	publishMQTT(mqttData)
}
