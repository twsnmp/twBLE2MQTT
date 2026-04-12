package main

import (
	"context"
	"flag"
	"fmt"
	"log"
	"os"
	"os/signal"
	"strings"
	"sync"
	"syscall"
	"time"
)

var version = "v1.0.0"
var commit = ""

// Command line flags and configuration variables
var syslogDst = ""      // Destination list for syslog (comma separated)
var mqttDst = ""        // MQTT broker destination (e.g., tcp://broker:1883)
var mqttUser = ""       // MQTT username
var mqttPassword = ""   // MQTT password
var mqttClientID = "twBlueScan" // MQTT client ID
var mqttTopic = "twBlueScan"    // MQTT base topic
var syslogInterval = 300        // Interval for sending reports (seconds)
var codeToVendor string         // Path to CSV for company code to vendor mapping
var addrToVendor string         // Path to CSV for MAC address to vendor mapping
var debug bool                  // Enable debug mode
var allAddress bool              // Report all addresses (including private/random)

func init() {
	// Define command line flags
	flag.StringVar(&syslogDst, "syslog", "", "syslog destnation list")
	flag.StringVar(&mqttDst, "mqtt", "", "mqtt broker destnation")
	flag.StringVar(&mqttUser, "mqttUser", "", "mqtt user name")
	flag.StringVar(&mqttPassword, "mqttPassword", "", "mqtt password")
	flag.StringVar(&mqttClientID, "mqttClientID", "twBlueScan", "mqtt client id")
	flag.StringVar(&mqttTopic, "mqttTopic", "twBlueScan", "mqtt topic")
	flag.IntVar(&syslogInterval, "interval", 600, "syslog send interval(sec)")
	flag.StringVar(&codeToVendor, "code", "", "make company code to vendor map")
	flag.StringVar(&addrToVendor, "addr", "", "make address to vendor map")
	flag.BoolVar(&debug, "debug", false, "debug mode")
	flag.BoolVar(&allAddress, "all", false, "report all address(include private)")

	// Override flags with environment variables if present (prefix: TWBLUESCAN_)
	flag.VisitAll(func(f *flag.Flag) {
		if s := os.Getenv("TWBLUESCAN_" + strings.ToUpper(f.Name)); s != "" {
			f.Value.Set(s)
		}
	})
	flag.Parse()
}

// logWriter is a custom writer that adds timestamps to log output
type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().Format("2006-01-02T15:04:05.999 ") + string(bytes))
}

func main() {
	// Setup logging
	log.SetFlags(0)
	log.SetOutput(new(logWriter))

	// Utility modes to generate vendor maps from CSV files
	if codeToVendor != "" {
		makeCodeToVendor()
		return
	}
	if addrToVendor != "" {
		makeAddressToVendor()
		return
	}

	log.Printf("version=%s", fmt.Sprintf("%s(%s)", version, commit))

	// Ensure at least one destination is configured
	if syslogDst == "" && mqttDst == "" {
		log.Fatalln("no syslog or mqtt distenation")
	}

	// Handle termination signals for graceful shutdown
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)

	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(3)

	// Start background workers
	go startSyslog(ctx, &wg)
	go startMQTT(ctx, &wg)
	go startBlueScan(ctx, &wg)

	<-quit
	sendSyslog("quit by signal")
	log.Println("quit by signal")

	// Shutdown workers
	cancel()
	wg.Wait()
}
