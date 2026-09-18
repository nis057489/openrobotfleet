package mqttc

import (
	mqtt "github.com/eclipse/paho.mqtt.golang"
	"github.com/eclipse/paho.mqtt.golang/packets"
	"net"
	"sync"
	"testing"
	"time"
)

// A minimal local MQTT peer makes broker reconnect tests deterministic and
// independent of Docker, installed Mosquitto, and external networks.
func TestSubscriptionsRecoverAfterReconnect(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	subscriptions := make(chan net.Conn, 4)
	credentials := make(chan *packets.ConnectPacket, 4)
	var mu sync.Mutex
	var connections []net.Conn
	defer func() {
		mu.Lock()
		defer mu.Unlock()
		for _, conn := range connections {
			conn.Close()
		}
	}()
	go func() {
		for {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			mu.Lock()
			connections = append(connections, conn)
			mu.Unlock()
			go func() {
				defer conn.Close()
				for {
					packet, err := packets.ReadPacket(conn)
					if err != nil {
						return
					}
					switch p := packet.(type) {
					case *packets.ConnectPacket:
						credentials <- p
						ack := packets.NewControlPacket(packets.Connack).(*packets.ConnackPacket)
						ack.Write(conn)
					case *packets.SubscribePacket:
						ack := packets.NewControlPacket(packets.Suback).(*packets.SubackPacket)
						ack.MessageID = p.MessageID
						ack.ReturnCodes = []byte{1}
						ack.Write(conn)
						subscriptions <- conn
					case *packets.PingreqPacket:
						packets.NewControlPacket(packets.Pingresp).Write(conn)
					case *packets.DisconnectPacket:
						return
					}
				}
			}()
		}
	}()
	c := NewClientWithCredentials("controller", "tcp://"+listener.Addr().String(), "controller", "test-secret", nil)
	defer c.Client.Disconnect(10)
	received := make(chan string, 4)
	c.Subscribe("lab/status/+", func(_ mqtt.Client, msg mqtt.Message) { received <- string(msg.Payload()) })
	nextSub := func() net.Conn {
		t.Helper()
		select {
		case conn := <-subscriptions:
			return conn
		case <-time.After(8 * time.Second):
			t.Fatal("subscription not restored")
			return nil
		}
	}
	first := nextSub()
	select {
	case auth := <-credentials:
		if auth.Username != "controller" || string(auth.Password) != "test-secret" {
			t.Fatal("credentials not sent")
		}
	case <-time.After(time.Second):
		t.Fatal("no connection")
	}
	first.Close()
	var second net.Conn
	deadline := time.After(8 * time.Second)
	for second == nil {
		select {
		case conn := <-subscriptions:
			if conn != first {
				second = conn
			}
		case <-deadline:
			t.Fatal("no resubscription after broker disconnect")
		}
	}
	message := packets.NewControlPacket(packets.Publish).(*packets.PublishPacket)
	message.TopicName = "lab/status/robot-a"
	message.Payload = []byte("reconnected")
	if err := message.Write(second); err != nil {
		t.Fatal(err)
	}
	select {
	case payload := <-received:
		if payload != "reconnected" {
			t.Fatal(payload)
		}
	case <-time.After(time.Second):
		t.Fatal("telemetry not delivered after reconnect")
	}
}

func TestDisconnectedPublishReturnsError(t *testing.T) {
	if err := (&Client{}).Publish("lab/commands/a", 1, false, []byte("command")); err == nil {
		t.Fatal("disconnected publish reported success")
	}
}
