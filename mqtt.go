package main

import (
	"context"
	"encoding/json"
	"log"
	"strings"
	"sync"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

// Channel to queue messages to be sent to MQTT broker
var mqttCh = make(chan interface{}, 2000)

// Data structures for MQTT JSON payloads

type mqttDeviceDataEnt struct {
	Time        string `json:"time"`
	Address     string `json:"address"`
	AddressType string `json:"address_type"`
	Name        string `json:"name"`
	Vendor      string `json:"vendor"`
	MinRSSI     int    `json:"min_rssi"`
	MaxRSSI     int    `json:"max_rssi"`
	RSSI        int    `json:"rssi"`
	Info        string `json:"info"`
	UUID        string `json:"uuid"`
	Count       int    `json:"count"`
	FirstTime   string `json:"first_time"`
	LastTime    string `json:"last_time"`
}

type mqttEnvDataEnt struct {
	Time        string  `json:"time"`
	Type        string  `json:"type"`
	Address     string  `json:"address"`
	Name        string  `json:"name"`
	RSSI        int     `json:"rssi"`
	Temperature float64 `json:"temperature"`
	Humidity    float64 `json:"humidity"`
	Co2         int     `json:"co2"`
	Lux         int     `json:"lux"`
	Battery     int     `json:"battery"`
	Pressure    float64 `json:"pressure"`
	TVOC        int     `json:"tvoc"`
	Sound       float64 `json:"sound"`
}

type mqttMotionSensorDataEnt struct {
	Time         string `json:"time"`
	Type         string `json:"type"`
	Address      string `json:"address"`
	Name         string `json:"name"`
	RSSI         int    `json:"rssi"`
	Moving       bool   `json:"moving"`
	LastMove     int64  `json:"last_move"`
	LastMoveDiff int64  `json:"last_move_diff"`
	Battery      int    `json:"battery"`
	Light        bool   `json:"light"`
}

type mqttPowerMonitorPlugDataEnt struct {
	Time    string `json:"time"`
	Type    string `json:"type"`
	Address string `json:"address"`
	Name    string `json:"name"`
	RSSI    int    `json:"rssi"`
	Switch  bool   `json:"switch"`
	Over    bool   `json:"over"`
	Load    int    `json:"load"`
}

type mqttBlueScanStatsDataEnt struct {
	Time    string `json:"time"`
	Total   int    `json:"total"`
	Count   int    `json:"count"`
	New     int    `json:"new"`
	Remove  int    `json:"remove"`
	Report  int    `json:"report"`
	Junk    int    `json:"junk"`
	Adaptor string `json:"adapter"`
}

type mqttMonitorDataEnt struct {
	Time    string  `json:"time"`
	CPU     float64 `json:"cpu"`
	Memory  float64 `json:"memory"`
	Load    float64 `json:"load"`
	Sent    uint64  `json:"sent"`
	Recv    uint64  `json:"recv"`
	TxSpeed float64 `json:"tx_speed"`
	RxSpeed float64 `json:"rx_speed"`
	Process int     `json:"process"`
}

// startMQTT initializes the MQTT client and starts a loop to publish queued messages.
func startMQTT(ctx context.Context, wg *sync.WaitGroup) {
	defer wg.Done()
	if mqttDst == "" {
		return
	}
	broker := mqttDst
	if !strings.Contains(broker, "://") {
		broker = "tcp://" + broker
	}
	if strings.LastIndex(broker, ":") <= 5 {
		broker += ":1883"
	}
	log.Printf("start mqtt broker=%s", broker)
	
	opts := mqtt.NewClientOptions()
	opts.AddBroker(broker)
	if mqttUser != "" && mqttPassword != "" {
		opts.SetUsername(mqttUser)
		opts.SetPassword(mqttPassword)
	}
	opts.SetClientID(mqttClientID)
	opts.SetAutoReconnect(true)
	if debug {
		opts.OnConnect = connectHandler
		opts.OnConnectionLost = connectLostHandler
	}
	
	client := mqtt.NewClient(opts)
	if token := client.Connect(); token.Wait() && token.Error() != nil {
		log.Println(token.Error())
		return
	}
	defer client.Disconnect(250)

	for {
		select {
		case <-ctx.Done():
			log.Println("stop mqtt")
			return
		case msg := <-mqttCh:
			if s := makeMqttData(msg); s != "" {
				if debug {
					log.Println(s)
				}
				client.Publish(getMqttTopic(msg), 1, false, s).Wait()
			}
		}
	}
}

// getMqttTopic returns the appropriate sub-topic based on the message type.
func getMqttTopic(msg interface{}) string {
	r := mqttTopic
	switch msg.(type) {
	case *mqttDeviceDataEnt:
		r += "/Device"
	case *mqttEnvDataEnt:
		r += "/Env"
	case *mqttMotionSensorDataEnt:
		r += "/Motion"
	case *mqttPowerMonitorPlugDataEnt:
		r += "/Power"
	case *mqttBlueScanStatsDataEnt:
		r += "/BlueScanStats"
	case *mqttMonitorDataEnt:
		r += "/Monitor"
	default:
		log.Printf("getMqttTopic: unknown msg type %T", msg)
	}
	return r
}

// makeMqttData marshals the message into a JSON string.
func makeMqttData(msg interface{}) string {
	if j, err := json.Marshal(msg); err == nil {
		return string(j)
	}
	return ""
}

// publishMQTT queues a message to be published to the MQTT broker.
func publishMQTT(msg interface{}) {
	if mqttDst == "" {
		return
	}
	select {
	case mqttCh <- msg:
	default:
		if debug {
			log.Println("mqtt channel full, skipping message")
		}
	}
}

var connectHandler mqtt.OnConnectHandler = func(client mqtt.Client) {
	log.Println("Connected")
}

var connectLostHandler mqtt.ConnectionLostHandler = func(client mqtt.Client, err error) {
	log.Printf("Connect lost: %v", err)
}
