package fluentd

import (
	"errors"
	"log"
	"math"
	"net"
	"os"
	"regexp"
	"strconv"

	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/gliderlabs/logspout/router"
)

const (
	defaultProtocol    = "tcp"
	defaultBufferLimit = 1024 * 1024

	defaultRetryWait  = 1000
	defaultMaxRetries = math.MaxInt32
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
	writer           *fluent.Fluent
	tagPrefix        string
	serviceNameLabel string
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

		// ts := int32(message.Time.Unix())
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
		// log.Println("Tag:" + tag)
		record := make(map[string]string)
		record["log"] = message.Data
		record["container_id"] = message.Container.ID
		record["container_name"] = message.Container.Name
		record["source"] = message.Source

		// Send to fluentd
		err = adapter.writer.PostWithTime(tag, message.Time, record)
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

	_, err := transport.Dial(route.Address, route.Options)
	if err != nil {
		return nil, err
	}

	// Construct fluentd config object
	host, port, err := net.SplitHostPort(route.Address)
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil, err
	}

	bufferLimit, err := strconv.Atoi(getenv("FLUENTD_BUFFER_LIMIT", strconv.Itoa(defaultBufferLimit)))
	if err != nil {
		return nil, err
	}

	retryWait, err := strconv.Atoi(getenv("FLUENTD_RETRY_WAIT", strconv.Itoa(defaultRetryWait)))
	if err != nil {
		return nil, err
	}

	maxRetries, err := strconv.Atoi(getenv("FLUENTD_MAX_RETRIES", strconv.Itoa(defaultMaxRetries)))
	if err != nil {
		return nil, err
	}

	asyncConnect, err := strconv.ParseBool(getenv("FLUENTD_ASYNC_CONNECT", "false"))
	if err != nil {
		return nil, err
	}

	subSecondPrecision, err := strconv.ParseBool(getenv("FLUENTD_SUBSECOND_PRECISION", "false"))
	if err != nil {
		return nil, err
	}

	fluentConfig := fluent.Config{
		FluentHost:         host,
		FluentPort:         portNum,
		FluentNetwork:      defaultProtocol,
		FluentSocketPath:   "",
		BufferLimit:        bufferLimit,
		RetryWait:          retryWait,
		MaxRetry:           maxRetries,
		Async:              asyncConnect,
		SubSecondPrecision: subSecondPrecision,
	}
	writer, err := fluent.New(fluentConfig)

	return &FluentdAdapter{
		writer:           writer,
		tagPrefix:        getenv("TAG_PREFIX", "docker"),
		serviceNameLabel: getenv("SERVICE_NAME_LABEL", ""),
	}, nil
}

func init() {
	router.AdapterFactories.Register(NewFluentdAdapter, "fluentd-tcp")
}
