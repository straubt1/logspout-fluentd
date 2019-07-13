### Logspout with Custom Built Fluentd module.

Ingesting logs into Fluentd but still want to view logs locally from your container? Then you've come to the right place.

## TLDR;

If you're ingesting your container logs into fluentd, there is a big chance that you might be using docker's built-in [fluentd log driver](https://docs.docker.com/config/containers/logging/fluentd/) to do just that, but you might have come across a [limitation](https://docs.docker.com/config/containers/logging/configure/) in the docker community ediiton that disallows you to view logs locally via [docker logs](https://docs.docker.com/config/containers/logging/). Using [Logspout](https://github.com/gliderlabs/logspout) with a [custom built](https://github.com/dsouzajude/logspout-fluentd) Fluentd module, you can surpass this limitation and ingest logs directly into Fluentd while still having the ability to view your logs locally.


## What's Logspout?

From the official documentation:

    """
    Logspout is a log router for Docker containers that runs inside Docker. It
    attaches to all containers on a host, then routes their logs wherever you
    want. It also has an extensible module system.
    """

The way it attaches itself to containers is by listening to docker log events and then capturing the logs to be routed to whatever destination you want, in our case to Fluentd. But it's not all that simple since Logspout doesn't come with a "built-in" router for Fluentd but because Logspout has an extensible module system, we can write our own module to route logs to Fluentd. In the next section, I will show you how to do exactly that.

## Extending Logspout with a Fluentd module

Logspout has instructions on how to extend itself using custom modules [here](https://github.com/gliderlabs/logspout/tree/master/custom). You can also read up on example custom built extensions [here](https://github.com/gliderlabs/logspout#third-party-modules). I've taken a similar but yet up-to-date approach using Go using [Go 1.11 Modules](https://github.com/golang/go/wiki/Modules) and a [Multi-stage](https://docs.docker.com/develop/develop-images/multistage-build/) build that just includes a custom Logspout binary.

Based on the instructions, first off, we need to write a simple custom go module that does the following:

1. Register itself to Logspout to receive Docker container log streams
2. Process the log streams and forward it to Fluentd

The code to register the module to Logspout is easy as shown below. We simply define and register a new Fluentd Adapter as the first thing when the package is imported.

```go

package fluentd

import "github.com/gliderlabs/logspout/router"


// NewAdapter creates a Logspout fluentd adapter instance.
func NewAdapter(route *router.Route) (router.LogAdapter, error) {
    ...
    ...
}

func init() {
	router.AdapterFactories.Register(NewAdapter, "fluentd")
}

```

Our `NewAdapter` function should basically construct itself to communicate with Fluentd and send logs to it the Fluentd way using the Fluentd protocol [specification](https://github.com/fluent/fluentd/wiki/Forward-Protocol-Specification-v1). The code below shows how this is done.

```go

package fluentd

import (
	"log"
	"net"
	"os"
	"strconv"
	"time"

	"github.com/fluent/fluent-logger-golang/fluent"
	"github.com/gliderlabs/logspout/router"
	"github.com/pkg/errors"
)

const (
	defaultProtocol    = "tcp"
	defaultBufferLimit = 1024 * 1024

	defaultWriteTimeout = 3
	defaultRetryWait    = 1000
	defaultMaxRetries   = math.MaxInt32
)

func getenv(key, fallback string) string {
	value := os.Getenv(key)
	if len(value) == 0 {
		return fallback
	}
	return value
}

// NewAdapter creates a Logspout fluentd adapter instance.
func NewAdapter(route *router.Route) (router.LogAdapter, error) {
	transport, found := router.AdapterTransports.Lookup(route.AdapterTransport("tcp"))
	if !found {
		return nil, errors.New("Unable to find adapter: " + route.Adapter)
	}
	_, err := transport.Dial(route.Address, route.Options)
	if err != nil {
		return nil, err
	}
	log.Println("Connectivity successful to fluentd @ " + route.Address)

	// Construct fluentd config object
	host, port, err := net.SplitHostPort(route.Address)
	portNum, err := strconv.Atoi(port)
	if err != nil {
		return nil, errors.Wrapf(err, "Invalid fluentd-address %s", route.Address)
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

	requestAck, err := strconv.ParseBool(getenv("FLUENTD_REQUEST_ACK", "false"))
	if err != nil {
		return nil, err
	}

	writeTimeout, err := strconv.Atoi(getenv("FLUENTD_WRITE_TIMEOUT", strconv.Itoa(defaultWriteTimeout)))
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
		RequestAck:         requestAck,
		WriteTimeout:       time.Duration(writeTimeout) * time.Second,
	}
	writer, err := fluent.New(fluentConfig)
	if err != nil {
		return nil, errors.Wrapf(err, "Unable to create fluentd logger")
	}

	return &Adapter{
		writer:         writer,
		tagPrefix:      getenv("TAG_PREFIX", "docker"),
		tagSuffixLabel: getenv("TAG_SUFFIX_LABEL", ""),
	}, nil
}

```

The [fluent-logger-golang/fluent](https://github.com/fluent/fluent-logger-golang) library makes things a lot easier as it already encapsulates the logic to send logs to Fluentd using the Fluentd specification. The above code now configures our Fluentd Adapter to communicate and ingest logs to Fluentd. All that's left now is to capture the log stream and send it. The code below does exactly that:

```go

// Stream handles a stream of messages from Logspout and forwards it to Fluentd
func (ad *Adapter) Stream(logstream chan *router.Message) {
	for message := range logstream {
		// Skip if message is empty
		messageIsEmpty, err := regexp.MatchString("^[[:space:]]*$", message.Data)
		if messageIsEmpty {
			log.Println("Skipping empty message!")
			continue
		}

		// Set tag
		tag := ""
		if len(ad.tagPrefix) > 0 {
			tag = ad.tagPrefix
		}
		tagSuffix := message.Container.Config.Labels[ad.tagSuffixLabel]
		if tagSuffix == "" {
			tagSuffix = message.Container.Name + "-" + message.Container.Config.Hostname
		}
		tag = tag + "." + tagSuffix

		// Construct record
		record := map[string]string{
			"log":            message.Data,
			"container_id":   message.Container.ID,
			"container_name": message.Container.Name,
			"source":         message.Source,
		}

		// Send to fluentd
		err = ad.writer.PostWithTime(tag, message.Time, record)
		if err != nil {
			log.Println("fluentd-adapter PostWithTime Error: ", err)
			continue
		}
	}
}

```

The [PostWithTime(...)](https://godoc.org/github.com/fluent/fluent-logger-golang/fluent#Fluent.PostWithTime) finally forwards our logs to Fluentd.

Once the adapter logic is ready, we can now include it in our custom Logspout build by following the instructions above. We need to include the following:

1. `modules.go` that will import our adapter into the Logspout binary.
2. `build.sh` to download Logspout code so that it can be re-built with our custom module.
3. `Dockerfile` that puts it all together and instructs docker how to build our custom Logspout.

Steps 1 and 2 are initiated on [ONBUILD COPY triggers](https://github.com/gliderlabs/logspout/blob/master/Dockerfile) as soon as we base our image from `FROM gliderlabs/logspout:master` so we don't have to explicitly copy it into our Dockerfile.

Here's the `modules.go` that imports our Fluentd adapter logic:

```go

package main

import (
	_ "github.com/dsouzajude/logspout-fluentd/fluentd"
	_ "github.com/gliderlabs/logspout/httpstream"
	_ "github.com/gliderlabs/logspout/transports/tcp"
	_ "github.com/gliderlabs/logspout/transports/udp"
)

```

And here's the `build.sh` file that simply downloads the Logspout source code so it can be custom built.

```bash

#!/bin/sh
set -e
mkdir -p /go/src/github.com/gliderlabs
cp -r /src /go/src/github.com/gliderlabs/logspout

# backwards compatibility
ln -fs /tmp/docker.sock /var/run/docker.sock

```

And here's the Dockerfile that does a multi-stage builds and uses `go 1.11 modules`:

```dockerfile

# ## Multi-stage build

#
# Init stage, includes logspout source code
# and triggers the build.sh script
#
FROM gliderlabs/logspout:master as master

#
# Build stage, build logspout with fluentd adapter
#
FROM golang:1.12.5-alpine3.9 as builder
RUN apk add --update go build-base git mercurial ca-certificates git
ENV GO111MODULE=on
WORKDIR /go/src/github.com/gliderlabs/logspout
COPY --from=master /go/src/github.com/gliderlabs/logspout /go/src/github.com/gliderlabs/logspout
COPY modules.go .
ADD . /go/src/github.com/dsouzajude/logspout-fluentd
RUN cd /go/src/github.com/dsouzajude/logspout-fluentd; go mod download && go mod tidy
RUN cd /go/src/github.com/gliderlabs/logspout; go mod download && go mod tidy
RUN echo "replace github.com/dsouzajude/logspout-fluentd => /go/src/github.com/dsouzajude/logspout-fluentd" >> go.mod
RUN CGO_ENABLED=0 GOOS=linux GOARCH=amd64 go build -ldflags "-X main.Version=$1" -o /bin/logspout


# #
# # Final stage
# #
FROM alpine
WORKDIR /app
COPY --from=builder /bin/logspout /app/
CMD ["./logspout"]

```

To build, simply run the following command:

```bash

# Build logspout locally with custom fluentd module using Dockerfile
>> docker build -t mycustomlogspout .

```


## Using Logspout with our Custom Built Fluentd module

After we've built Logspout with our custom built Fluentd module, we simply run the docker image with the following command:


```bash
# Example to run custom built logspout locally:
>> docker run --rm --name="logspout" \
			-v /var/run/docker.sock:/var/run/docker.sock \
			-e TAG_PREFIX=docker \
			-e TAG_SUFFIX_LABEL="com.mycompany.service" \
			-e FLUENTD_ASYNC_CONNECT="true" \
			-e LOGSPOUT="ignore" \
			mycustomlogspout \
				./logspout fluentd://<FLUENTD_IP>:<FLUENTD_PORT>


```

The environment variable `TAG_PREFIX` will add a fixed tag prefix to every log entry in the stream forwarded to Fluentd. Similarly the `TAG_SUFFIX_LABEL` environment variable will add a dynamic tag suffix by picking the value from the docker label of your container. Other `FLUENTD*` specific environment variables relate directly to the fluentd log driver configurations described [here](https://docs.docker.com/config/containers/logging/fluentd/).

For more environment variables and configurations settings, you can read more on how to use them [here](https://github.com/dsouzajude/logspout-fluentd#usage).

We can test that logs are actually captured by Logspout and forwarded to Fluentd using a test echo container. The following demonstration tests a simple usecase in the picture below using [Fluent-Bit](https://fluentbit.io/) which is a more light-weight version of Fluentd but follows the same protocol specification.

![](logspout-fluentd-in-action.gif)

## Other features of Logspout

One really cool [feature](https://github.com/gliderlabs/logspout#inspect-log-streams-using-curl) of logspout is streaming all or specific container logs on a host via a REST API. You can simply curl the logspout endpoint and you get nice color coded logs. For this to work, you need to set the `PORT=<PORT_NUMBER>` environment variable and publish that port on the Logspout container. Following is a demo:

```bash

>> curl localhost:24223/logs

admiring_ishizaka|hello world
eloquent_lovelace|hello world
   elated_neumann|hello world

```

## Other Notes

- For some of you, it might not be trivial to have a requirement to view logs locally or on one of your virtual hosts. Usually you would have a good sophisticated log processing pipeline in place, such as an ELK/EFK stack to ingest your logs so that you can view it in systems like Kibana, Sumo Logic, Splunk or Datadog. Probably in most other cases, ssh-ing into machines to actually view the logs for debugging purposes is also not a good idea at all, but sometimes stuff breaks and you'll need manual intervention. So this solution is not intended as a guideline to be included as part of your log processing pipeline but rather helpful in environments where you need fast debugging and testing for your systems and applications.

- Logspout also helps solve the problem where in cases where your log messages is greater than 16k bytes, docker will split those logs into multiple chunks. However, as of this writing, most log drivers or third party plugins [are not equipped](https://github.com/moby/moby/issues/34620) yet to handle this docker log splitting. To Logspout, this becomes transparent for your pipeline.

- Also note that Logspout will only gather logs from containers that are started without the `-t` option and that are configured with a logging driver that works with docker logs (journald and json-file).


## Further reading

You can read more on this solution and the code on my github repo [@dsouzajude/logspout-fluentd](https://github.com/dsouzajude/logspout-fluentd).


