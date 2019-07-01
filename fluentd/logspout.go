package fluentd

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"
	"regexp"
	"time"

	"github.com/gliderlabs/logspout/router"
)

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

// FluentdAdapter is an adapter for streaming JSON to a fluentd collector.
type FluentdAdapter struct {
	conn             net.Conn
	route            *router.Route
	transport        router.AdapterTransport
	tagPrefix        string
	serviceNameLabel string
}

func retryExp(fun func() error, tries uint) error {
	try := uint(0)
	for {
		err := fun()
		if err == nil {
			return nil
		}

		try++
		if try > tries {
			return err
		}

		time.Sleep((1 << try) * 10 * time.Millisecond)
	}
}

func (adapter *FluentdAdapter) retryTemporary(json []byte) error {
	log.Println("fluentd-adapter: retrying tcp up to 11 times")
	err := retryExp(func() error {
		_, err := adapter.conn.Write(json)
		if err == nil {
			log.Println("fluentd-adapter: retry successful")
			return nil
		}

		return err
	}, 11)

	if err != nil {
		log.Println("fluentd-adapter: retry failed")
		return err
	}

	return nil
}

func (adapter *FluentdAdapter) reconnect() error {
	log.Println("fluentd-adapter: reconnecting forever")

	for {
		conn, err := adapter.transport.Dial(adapter.route.Address, adapter.route.Options)
		if err != nil {
			time.Sleep(10 * time.Second)
			continue
		}

		log.Println("fluentd-adapter: reconnected")

		adapter.conn = conn
		return nil
	}
}
func (adapter *FluentdAdapter) retry(json []byte, err error) error {
	if opError, ok := err.(*net.OpError); ok {
		if opError.Temporary() || opError.Timeout() {
			retryErr := adapter.retryTemporary(json)
			if retryErr == nil {
				return nil
			}
		}
	}

	return adapter.reconnect()
}

// Stream handles a stream of messages from Logspout. Implements router.logAdapter.
func (adapter *FluentdAdapter) Stream(logstream chan *router.Message) {
	for message := range logstream {
		// Skip if message is empty
		messageIsEmpty, err := regexp.MatchString("^[[:space:]]*$", message.Data)
		if messageIsEmpty {
			log.Println("Skipping empty message!")
			continue
		}

		timestamp := int32(message.Time.Unix())
		serviceName := message.Container.Config.Labels[adapter.serviceNameLabel]
		tag := ""
		// Set tag prefix
		if len(adapter.tagPrefix) > 0 {
			tag = adapter.tagPrefix
		}
		// Set tag suffix
		if len(serviceName) > 0 {
			tag = tag + "." + serviceName
		}
		log.Println("Tag:" + tag)
		record := make(map[string]string)
		record["log"] = message.Data
		record["container_id"] = message.Container.ID
		record["container_name"] = message.Container.Name
		record["source"] = message.Source

		// Construct data to JSON
		data := []interface{}{tag, timestamp, record}
		json, err := json.Marshal(data)
		if err != nil {
			log.Println("fluentd-adapter: ", err)
			continue
		}

		// Send to fluentd
		_, err = adapter.conn.Write(json)
		if err != nil {
			err = adapter.retry(json, err)
			if err != nil {
				log.Println("fluentd-adapter: ", err)
				continue
			}
		}
	}
}

// NewFluentdAdapter creates a Logspout fluentd adapter instance.
func NewFluentdAdapter(route *router.Route) (router.LogAdapter, error) {
	transport, found := router.AdapterTransports.Lookup(route.AdapterTransport("tcp"))

	if !found {
		return nil, errors.New("unable to find adapter: " + route.Adapter)
	}

	conn, err := transport.Dial(route.Address, route.Options)
	if err != nil {
		return nil, err
	}

	return &FluentdAdapter{
		conn:             conn,
		route:            route,
		transport:        transport,
		tagPrefix:        getenv("TAG_PREFIX", "docker"),
		serviceNameLabel: getenv("SERVICE_NAME_LABEL", ""),
	}, nil
}

func init() {
	router.AdapterFactories.Register(NewFluentdAdapter, "fluentd-tcp")
}
