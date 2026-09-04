package mqtt

import (
	"context"
	"crypto/tls"
	"crypto/x509"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"strings"
	"time"

	paho "github.com/eclipse/paho.mqtt.golang"
)

type Config struct{ URL, Username, Password, CAFile, ClientID string }

type Client struct {
	config   Config
	ingestor *Ingestor
	logger   *slog.Logger
	mqtt     paho.Client
	OnEvent  func(any)
}

func NewClient(config Config, ingestor *Ingestor, logger *slog.Logger) *Client {
	return &Client{config: config, ingestor: ingestor, logger: logger}
}

// Run maintains the broker connection until ctx is cancelled. Initial
// connection failures are retried; Paho handles reconnects after connection.
func (c *Client) Run(ctx context.Context) error {
	if strings.TrimSpace(c.config.URL) == "" {
		return errors.New("MQTT URL is empty")
	}
	brokerURL, tlsEnabled := normalizeBrokerURL(c.config.URL)
	options := paho.NewClientOptions().AddBroker(brokerURL).SetClientID(c.config.ClientID).SetUsername(c.config.Username).SetPassword(c.config.Password).SetAutoReconnect(true).SetConnectRetry(false)
	if tlsEnabled {
		tlsConfig, err := c.tlsConfig()
		if err != nil {
			return err
		}
		options.SetTLSConfig(tlsConfig)
	}
	options.SetOnConnectHandler(func(client paho.Client) { c.onConnect(client) })
	options.SetConnectionLostHandler(func(_ paho.Client, err error) { c.log("MQTT connection lost", "mqtt_connection_lost", err) })
	c.mqtt = paho.NewClient(options)
	for {
		token := c.mqtt.Connect()
		if token.Wait() && token.Error() == nil {
			break
		}
		if ctx.Err() != nil {
			return ctx.Err()
		}
		if token.Error() != nil {
			c.log("MQTT connection failed", "mqtt_connection_failed", token.Error())
		}
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(5 * time.Second):
		}
	}
	<-ctx.Done()
	c.mqtt.Disconnect(1000)
	return nil
}

func normalizeBrokerURL(value string) (string, bool) {
	url := strings.TrimSpace(value)
	lower := strings.ToLower(url)
	switch {
	case strings.HasPrefix(lower, "mqtt://"):
		return "tcp://" + url[len("mqtt://"):], false
	case strings.HasPrefix(lower, "mqtts://"):
		return "ssl://" + url[len("mqtts://"):], true
	case strings.HasPrefix(lower, "tls://"):
		return "ssl://" + url[len("tls://"):], true
	case strings.HasPrefix(lower, "ssl://"):
		return url, true
	default:
		return url, false
	}
}

func (c *Client) onConnect(client paho.Client) {
	topics := map[string]byte{"my_assistant/v1/+/discovery": 1, "my_assistant/v1/+/availability": 1, "my_assistant/v1/+/telemetry": 1}
	token := client.SubscribeMultiple(topics, func(_ paho.Client, message paho.Message) {
		if c.ingestor == nil {
			return
		}
		if err := c.ingestor.Handle(context.Background(), message.Topic(), message.Payload()); err != nil {
			c.log("MQTT message rejected", "mqtt_message_rejected", err)
		} else {
			c.publishEvents(message.Topic(), message.Payload())
			if c.logger != nil {
				c.logger.Info("MQTT message ingested", "event", "mqtt_message_ingested", "topic", message.Topic())
			}
		}
	})
	if token.Wait() && token.Error() != nil {
		c.log("MQTT subscription failed", "mqtt_subscription_failed", token.Error())
		return
	}
	if c.logger != nil {
		c.logger.Info("MQTT connected", "event", "mqtt_connected")
	}
}

func (c *Client) publishEvents(topic string, payload []byte) {
	if c.OnEvent == nil {
		return
	}
	parts := strings.Split(strings.Trim(topic, "/"), "/")
	if len(parts) != 4 {
		return
	}
	deviceID := parts[2]
	var raw map[string]any
	if json.Unmarshal(payload, &raw) != nil {
		raw = map[string]any{}
	}
	switch parts[3] {
	case "discovery":
		raw["device_id"] = deviceID
		c.OnEvent(map[string]any{"type": "device.state", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "data": raw})
	case "availability":
		state := strings.TrimSpace(string(payload))
		c.OnEvent(map[string]any{"type": "device.availability", "timestamp": time.Now().UTC().Format(time.RFC3339Nano), "data": map[string]any{"device_id": deviceID, "available": state == "online", "state": state}})
	case "telemetry":
		measurements, ok := raw["measurements"].(map[string]any)
		if !ok {
			return
		}
		timestamp, _ := raw["timestamp"].(string)
		for metric, value := range measurements {
			c.OnEvent(map[string]any{"type": "sensor.reading", "timestamp": timestamp, "data": map[string]any{"device_id": deviceID, "metric": metric, "value": value, "timestamp": timestamp, "available": true, "state": "online"}})
		}
	}
}

func (c *Client) tlsConfig() (*tls.Config, error) {
	config := &tls.Config{MinVersion: tls.VersionTLS12}
	if strings.TrimSpace(c.config.CAFile) == "" {
		return config, nil
	}
	data, err := os.ReadFile(c.config.CAFile)
	if err != nil {
		return nil, fmt.Errorf("read MQTT CA file: %w", err)
	}
	pool, err := x509.SystemCertPool()
	if err != nil || pool == nil {
		pool = x509.NewCertPool()
	}
	if !pool.AppendCertsFromPEM(data) {
		return nil, errors.New("MQTT CA file contains no certificates")
	}
	config.RootCAs = pool
	return config, nil
}

func (c *Client) log(message, event string, err error) {
	if c.logger != nil {
		c.logger.Error(message, "event", event, "error_type", fmt.Sprintf("%T", err), "error_message", err.Error())
	}
}
