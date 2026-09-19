package mqttc

import (
	"crypto/tls"
	"errors"
	"fmt"
	"log"
	"net"
	"net/url"
	"os"
	"strings"
	"sync"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

const operationTimeout = 5 * time.Second

// A client that cannot reach the broker is reported on this schedule: silent
// for the first interval so ordinary startup races (agent up before the
// broker) do not log, then backing off so a broker that stays down for hours
// does not fill the journal.
const (
	firstStallReport = 15 * time.Second
	maxStallReport   = 5 * time.Minute
)

func init() {
	// paho routes all of its own output to NOOPLogger unless told otherwise,
	// which is why an unreachable broker, a rejected password and (once TLS
	// is in use) a certificate the trust store rejects are indistinguishable
	// from "no robots have connected yet": nothing is logged at all. These
	// two levels carry genuine faults only. WARN is deliberately left off --
	// it fires on benign races such as Connect() during an auto-reconnect.
	mqtt.ERROR = log.New(os.Stderr, "MQTT ERROR: ", log.LstdFlags)
	mqtt.CRITICAL = log.New(os.Stderr, "MQTT CRITICAL: ", log.LstdFlags)
}

type Client struct {
	Client        mqtt.Client
	clientID      string
	broker        string
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
	wrapper := &Client{clientID: clientID, broker: broker, subscriptions: make(map[string]mqtt.MessageHandler)}
	opts := mqtt.NewClientOptions().AddBroker(broker).SetClientID(clientID).
		SetUsername(username).SetPassword(password).SetConnectTimeout(operationTimeout).
		SetConnectRetry(true).SetConnectRetryInterval(time.Second).SetAutoReconnect(true).
		SetOrderMatters(false).SetWriteTimeout(operationTimeout)
	opts.SetConnectionLostHandler(func(_ mqtt.Client, err error) {
		log.Printf("MQTT [%s] lost connection to %s: %v", clientID, broker, err)
	})
	opts.SetReconnectingHandler(func(_ mqtt.Client, _ *mqtt.ClientOptions) {
		log.Printf("MQTT [%s] reconnecting to %s", clientID, broker)
	})
	opts.SetOnConnectHandler(func(c mqtt.Client) {
		log.Printf("MQTT [%s] connected to %s", clientID, broker)
		wrapper.mu.Lock()
		subscriptions := make(map[string]mqtt.MessageHandler, len(wrapper.subscriptions))
		for topic, handler := range wrapper.subscriptions {
			subscriptions[topic] = handler
		}
		wrapper.mu.Unlock()
		for topic, handler := range subscriptions {
			if err := wait(c.Subscribe(topic, 1, handler)); err != nil {
				log.Printf("MQTT [%s] subscribe %s: %v", clientID, topic, err)
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
	go wrapper.reportStalledConnection()
	return wrapper
}

// reportStalledConnection logs a client that never manages to connect.
// ConnectRetry keeps paho retrying in the background indefinitely, but it
// records why each attempt failed at DEBUG level only -- far too noisy to
// leave enabled -- and its one ERROR line is unreachable while retries
// continue. The reason is recovered here instead, by dialling the broker
// directly. Runs for the life of the process; each binary builds one client.
func (c *Client) reportStalledConnection() {
	delay := firstStallReport
	for {
		time.Sleep(delay)
		if c.Client != nil && c.Client.IsConnectionOpen() {
			delay = firstStallReport
			continue
		}
		log.Printf("MQTT [%s] not connected to %s: %v", c.clientID, c.broker, diagnoseBroker(c.broker))
		if delay *= 2; delay > maxStallReport {
			delay = maxStallReport
		}
	}
}

// diagnoseBroker dials the broker the way paho would, so the underlying fault
// -- refused, timed out, or a certificate the system trust store will not
// accept -- reaches the log rather than being swallowed by the retry loop.
func diagnoseBroker(broker string) error {
	if !strings.Contains(broker, "://") {
		broker = "tcp://" + broker // paho's AddBroker assumes the same default
	}
	u, err := url.Parse(broker)
	if err != nil {
		return fmt.Errorf("unusable broker URL %q: %w", broker, err)
	}
	var secure bool
	switch u.Scheme {
	case "ssl", "tls", "mqtts", "mqtt+ssl", "tcps", "wss":
		secure = true
	}
	addr := u.Host
	if u.Port() == "" {
		if secure {
			addr = net.JoinHostPort(addr, "8883")
		} else {
			addr = net.JoinHostPort(addr, "1883")
		}
	}
	dialer := &net.Dialer{Timeout: operationTimeout}
	var conn net.Conn
	if secure {
		// A nil config matches paho's own default: verify against the system
		// trust store, with the host checked against the certificate's SANs.
		conn, err = tls.DialWithDialer(dialer, "tcp", addr, nil)
	} else {
		conn, err = dialer.Dial("tcp", addr)
	}
	if err != nil {
		return err
	}
	conn.Close()
	return errors.New("broker reachable, so the transport is fine; check the username, password and ACL")
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
			log.Printf("MQTT [%s] subscribe %s: %v", c.clientID, topic, err)
		}
	}
}
