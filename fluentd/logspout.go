package fluentd

import (
	"encoding/json"
	"errors"
	"log"
	"net"
	"os"

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
	tagPrefix        string
	serviceNameLabel string
}

// Stream handles a stream of messages from Logspout. Implements router.logAdapter.
func (adapter *FluentdAdapter) Stream(logstream chan *router.Message) {
	for message := range logstream {
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
			log.Println("fluentd-adapter: ", err)
			continue
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
		tagPrefix:        getenv("TAG_PREFIX", "docker"),
		serviceNameLabel: getenv("SERVICE_NAME_LABEL", ""),
	}, nil
}

func init() {
	router.AdapterFactories.Register(NewFluentdAdapter, "fluentd-tcp")
}
