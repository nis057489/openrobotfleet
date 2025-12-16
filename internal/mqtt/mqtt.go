package mqttc

import (
	"log"
	"os"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Client struct {
	Client mqtt.Client
}

// NewClient creates a client using environment/default broker.
func NewClient(clientID string) *Client {
	return NewClientWithBroker(clientID, "")
}

// NewClientWithBroker lets callers override the MQTT broker address.
func NewClientWithBroker(clientID, broker string) *Client {
	return NewClientWithHandler(clientID, broker, nil)
}

// NewClientWithHandler lets callers provide an OnConnect handler.
func NewClientWithHandler(clientID, broker string, onConnect mqtt.OnConnectHandler) *Client {
	if broker == "" {
		broker = os.Getenv("MQTT_BROKER")
		if broker == "" {
			broker = "tcp://192.168.100.122:1883"
		}
	}
	opts := mqtt.NewClientOptions().
		AddBroker(broker).
		SetClientID(clientID).
		SetConnectTimeout(5 * time.Second)

	if onConnect != nil {
		opts.SetOnConnectHandler(onConnect)
	}

	c := mqtt.NewClient(opts)
	if token := c.Connect(); token.Wait() && token.Error() != nil {
		log.Printf("MQTT connect error: %v", token.Error())
	}
	return &Client{Client: c}
}

func (c *Client) Publish(topic string, payload []byte) {
	if c == nil || c.Client == nil {
		return
	}
	token := c.Client.Publish(topic, 0, false, payload)
	token.Wait()
}

func (c *Client) Subscribe(topic string, handler mqtt.MessageHandler) {
	if c == nil || c.Client == nil {
		return
	}
	token := c.Client.Subscribe(topic, 0, handler)
	token.Wait()
	if token.Error() != nil {
		log.Printf("MQTT subscribe error: %v", token.Error())
	}
}
