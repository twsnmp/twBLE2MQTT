package main

import (
	"context"
	"fmt"
	"log"
	"net"
	"os"
	"strings"
	"sync"
	"time"
)

// Channel to queue messages to be sent to Syslog servers
var syslogCh = make(chan string, 2000)
var syslogCount = 0 // Counter for messages sent in the current interval

// startSyslog initializes UDP connections to syslog destinations 
// and starts a loop to send queued messages.
func startSyslog(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	log.Printf("start syslog dst=%s", syslogDst)
	dstList := strings.Split(syslogDst, ",")
	dst := []net.Conn{}
	
	// Establish connections to each destination
	for _, d := range dstList {
		if d == "" {
			continue
		}
		if !strings.Contains(d, ":") {
			d += ":514"
		}
		s, err := net.Dial("udp", d)
		if err != nil {
			log.Printf("start syslog err=%v", err)
			continue
		}
		sendSyslog(fmt.Sprintf("start send syslog to %s", d))
		dst = append(dst, s)
	}
	
	host, err := os.Hostname()
	if err != nil {
		host = "localhost"
	}
	
	defer func() {
		for _, d := range dst {
			d.Close()
		}
	}()
	
	for {
		select {
		case <-ctx.Done():
			log.Println("stop syslog")
			return
		case msg := <-syslogCh:
			syslogCount++
			// Format message in RFC5424-like format
			// <174> is local5.info priority
			s := fmt.Sprintf("<%d>%s %s twBlueScan: %s", 21*8+6, time.Now().Format("2006-01-02T15:04:05-07:00"), host, msg)
			for _, d := range dst {
				d.Write([]byte(s))
			}
		}
	}
}

// sendSyslog queues a message to be sent to Syslog servers.
func sendSyslog(msg string) {
	select {
	case syslogCh <- msg:
	default:
		if debug {
			log.Println("syslog channel full, skipping message")
		}
	}
}
