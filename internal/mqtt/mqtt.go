package mqttc

import (
	"fmt"
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"log"
	"os"
	"sync"
	"time"
)

const operationTimeout = 5 * time.Second

type Client struct {
	Client        mqtt.Client
	mu            sync.Mutex
	subscriptions map[string]mqtt.MessageHandler
}

func NewClient(clientID string) *Client { return NewClientWithBroker(clientID, "") }
func NewClientWithBroker(clientID, broker string) *Client {
	return NewClientWithHandler(clientID, broker, nil)
}
func NewClientWithHandler(clientID, broker string, onConnect mqtt.OnConnectHandler) *Client {
	return NewClientWithCredentials(clientID, broker, os.Getenv("MQTT_USERNAME"), os.Getenv("MQTT_PASSWORD"), onConnect)
}

func NewClientWithCredentials(clientID, broker, username, password string, onConnect mqtt.OnConnectHandler) *Client {
	if broker == "" {
		broker = os.Getenv("MQTT_BROKER")
	}
	if broker == "" {
		broker = "tcp://192.168.1.10:1883"
	}
	wrapper := &Client{subscriptions: make(map[string]mqtt.MessageHandler)}
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).
		SetUsername(username).SetPassword(password).SetConnectTimeout(operationTimeout).
		SetConnectRetry(true).SetConnectRetryInterval(time.Second).SetAutoReconnect(true).
		SetOrderMatters(false).SetWriteTimeout(operationTimeout)
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		wrapper.mu.Lock()
		subscriptions := make(map[string]mqtt.MessageHandler, len(wrapper.subscriptions))
		for topic, handler := range wrapper.subscriptions {
			subscriptions[topic] = handler
		}
		wrapper.mu.Unlock()
		for topic, handler := range subscriptions {
			if err := wait(c.Subscribe(topic, 1, handler)); err != nil {
				log.Printf("MQTT subscribe %s: %v", topic, err)
			}
		}
		if onConnect != nil {
			onConnect(c)
		}
	})
	wrapper.Client = mqtt.NewClient(opts)
	// ConnectRetry owns initial retries as well as subsequent reconnects. Do not
	// block startup while the broker is unavailable.
	wrapper.Client.Connect()
	return wrapper
}

func wait(token mqtt.Token) error {
	if !token.WaitTimeout(operationTimeout) {
		return fmt.Errorf("MQTT operation timed out")
	}
	return token.Error()
}

func (c *Client) Publish(topic string, qos byte, retained bool, payload []byte) error {
	if c == nil || c.Client == nil || !c.Client.IsConnectionOpen() {
		return fmt.Errorf("MQTT disconnected")
	}
	return wait(c.Client.Publish(topic, qos, retained, payload))
}

func (c *Client) Subscribe(topic string, handler mqtt.MessageHandler) {
	if c == nil || c.Client == nil {
		return
	}
	c.mu.Lock()
	if c.subscriptions == nil {
		c.subscriptions = make(map[string]mqtt.MessageHandler)
	}
	c.subscriptions[topic] = handler
	c.mu.Unlock()
	if c.Client.IsConnectionOpen() {
		if err := wait(c.Client.Subscribe(topic, 1, handler)); err != nil {
			log.Printf("MQTT subscribe %s: %v", topic, err)
		}
	}
}
