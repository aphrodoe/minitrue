package mqttclient

import (
	"fmt"
	"time"

	mqtt "github.com/eclipse/paho.mqtt.golang"
)

type Options struct {
	BrokerURL string
	ClientID  string
}

type Client struct {
	raw mqtt.Client
}

func New(opts Options) (*Client, error) {
	o := mqtt.NewClientOptions()
	o.AddBroker(opts.BrokerURL)
	o.SetClientID(opts.ClientID)
	o.SetConnectRetry(true)
	o.SetConnectRetryInterval(2 * time.Second)
	c := mqtt.NewClient(o)

	token := c.Connect()
	if token.Wait() && token.Error() != nil {
		return nil, token.Error()
	}
	return &Client{raw: c}, nil
}

func (c *Client) Publish(topic string, payload []byte, qos byte, retained bool) error {
	token := c.raw.Publish(topic, qos, retained, payload)
	token.Wait()
	return token.Error()
}

func (c *Client) Subscribe(topic string, qos byte, handler mqtt.MessageHandler) error {
	token := c.raw.Subscribe(topic, qos, handler)
	token.Wait()
	return token.Error()
}

func (c *Client) Close() {
	c.raw.Disconnect(250)
}

func (c *Client) String() string {
	return fmt.Sprintf("MQTTClient")
}




