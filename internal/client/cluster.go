package client

import (
	"bufio"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
	"time"
)

type ClusterClient struct {
	brokers       []string
	conn          net.Conn
	connMu        sync.Mutex
	currentBroker string
	reader        *bufio.Reader
}

func NewClusterClient(brokers []string) (*ClusterClient, error) {
	if len(brokers) == 0 {
		return nil, fmt.Errorf("no brokers provided")
	}

	client := &ClusterClient{
		brokers: brokers,
	}

	if err := client.connect(); err != nil {
		return nil, err
	}

	return client, nil
}

func (c *ClusterClient) connect() error {
	for _, broker := range c.brokers {
		conn, err := net.DialTimeout("tcp", broker, 2*time.Second)
		if err != nil {
			log.Printf("Failed to connect to %s: %v", broker, err)
			continue
		}

		c.connMu.Lock()
		c.conn = conn
		c.currentBroker = broker
		c.reader = bufio.NewReader(conn)
		c.connMu.Unlock()

		log.Printf("Connected to broker: %s", broker)
		return nil
	}

	return fmt.Errorf("failed to connect to any broker")
}

func (c *ClusterClient) Write(data []byte) error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn == nil {
		return fmt.Errorf("no connection")
	}

	_, err := c.conn.Write(data)
	if err != nil {
		log.Printf("Write failed to %s: %v, trying to reconnect", c.currentBroker, err)
		c.conn.Close()
		c.conn = nil

		c.connMu.Unlock()
		reconnectErr := c.connect()
		c.connMu.Lock()

		if reconnectErr != nil {
			return fmt.Errorf("write failed and reconnect failed: %v", reconnectErr)
		}

		_, err = c.conn.Write(data)
	}

	return err
}

func (c *ClusterClient) ReadString(delim byte) (string, error) {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.reader == nil {
		return "", fmt.Errorf("no connection")
	}

	line, err := c.reader.ReadString(delim)
	if err != nil {
		log.Printf("Read failed from %s: %v", c.currentBroker, err)
		return "", err
	}

	if strings.HasPrefix(line, "-ERR NOT_LEADER") {
		parts := strings.Fields(line)
		if len(parts) >= 3 {
			leaderAddr := strings.TrimPrefix(parts[2], "leader=")
			log.Printf("Not leader, redirecting to: %s", leaderAddr)

			c.conn.Close()
			c.conn = nil
			c.connMu.Unlock()

			if err := c.connectToSpecific(leaderAddr); err != nil {
				c.connMu.Lock()
				return "", fmt.Errorf("failed to connect to leader: %v", err)
			}
			c.connMu.Lock()

			return "", fmt.Errorf("redirected to leader, retry operation")
		}
	}

	return line, nil
}

func (c *ClusterClient) connectToSpecific(addr string) error {
	conn, err := net.DialTimeout("tcp", addr, 2*time.Second)
	if err != nil {
		return err
	}

	c.connMu.Lock()
	c.conn = conn
	c.currentBroker = addr
	c.reader = bufio.NewReader(conn)
	c.connMu.Unlock()

	log.Printf("Reconnected to leader: %s", addr)
	return nil
}

func (c *ClusterClient) GetReader() *bufio.Reader {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.reader
}

func (c *ClusterClient) GetConn() net.Conn {
	c.connMu.Lock()
	defer c.connMu.Unlock()
	return c.conn
}

func (c *ClusterClient) Close() error {
	c.connMu.Lock()
	defer c.connMu.Unlock()

	if c.conn != nil {
		return c.conn.Close()
	}
	return nil
}
