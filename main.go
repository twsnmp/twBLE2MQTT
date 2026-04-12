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
var syslogDst = ""
var mqttDst = ""
var mqttUser = ""
var mqttPassword = ""
var mqttClientID = "twBlueScan"
var mqttTopic = "twBlueScan"
var syslogInterval = 300
var codeToVendor string
var addrToVendor string
var debug bool
var allAddress bool

func init() {
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
	flag.VisitAll(func(f *flag.Flag) {
		if s := os.Getenv("TWBLUESCAN_" + strings.ToUpper(f.Name)); s != "" {
			f.Value.Set(s)
		}
	})
	flag.Parse()
}

type logWriter struct {
}

func (writer logWriter) Write(bytes []byte) (int, error) {
	return fmt.Print(time.Now().Format("2006-01-02T15:04:05.999 ") + string(bytes))
}

func main() {
	log.SetFlags(0)
	log.SetOutput(new(logWriter))
	if codeToVendor != "" {
		makeCodeToVendor()
		return
	}
	if addrToVendor != "" {
		makeAddressToVendor()
		return
	}
	log.Printf("version=%s", fmt.Sprintf("%s(%s)", version, commit))
	if syslogDst == "" && mqttDst == "" {
		log.Fatalln("no syslog or mqtt distenation")
	}
	quit := make(chan os.Signal, 1)
	signal.Notify(quit, os.Interrupt, syscall.SIGTERM)
	ctx, cancel := context.WithCancel(context.Background())
	var wg sync.WaitGroup
	wg.Add(3)
	go startSyslog(ctx, &wg)
	go startMQTT(ctx, &wg)
	go startBlueScan(ctx, &wg)
	<-quit
	sendSyslog("quit by signal")
	log.Println("quit by signal")
	cancel()
	wg.Wait()
}
