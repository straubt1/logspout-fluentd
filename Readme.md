# logspout-fluentd

[Logspout module](https://github.com/gliderlabs/logspout/tree/master/custom) for forwarding logs to fluentd and/or fluentbit. What's different about this module is that it uses the [fluentd goland log driver](github.com/fluent/fluent-logger-golang/fluent) to actually forward logs in the most efficient way similar to how the fluentd log driver works and with the  same parameters it accepts. These parameters are configurable via environment variables.

## Usage

This module works by acting as a fluentd forwarder. You can configuer the *fixed* prefix tag via the `TAG_PREFIX` environment variable and the *dynamic* tag suffix by another `TAG_SUFFIX_LABEL` environment variable that should point to a docker label.

You can run logspout the following way:

```bash
>> docker run --rm --name="logspout" \
			-v /var/run/docker.sock:/var/run/docker.sock \
			-e TAG_PREFIX=docker \
			-e TAG_SUFFIX_LABEL="com.mycompany.service" \
			-e FLUENTD_ASYNC_CONNECT="true" \
			-e LOGSPOUT="ignore" \
			<REGISTRY>/<CUSTOM_LOGSPOUT>:<VERSION> \
				./logspout fluentd://<FLUENTD_IP>:<FLUENTD_PORT>
```

Parameters required:

- `FLUENTD_IP` the IP of fluentd
- `FLUENTD_PORT` the Port of fluentd

Optional environment variables:

- `TAG_PREFIX` the *fixed* tag prefix
- `TAG_SUFFIX_LABEL` the docker label key for which to substitue as the *dynamic* tag suffix

The other fluentd specific environment variables include the list below and
their explanation can be better read from the fluentd log driver [website](https://docs.docker.com/config/containers/logging/fluentd/).

- `FLUENTD_BUFFER_LIMIT` [int] The amount of data to buffer before flushing to disk. Defaults to the amount of RAM available to the container.
- `FLUENTD_RETRY_WAIT` [int] How long to wait between retries. Defaults to 1 second.
- `FLUENTD_MAX_RETRIES` [int] The maximum number of retries. Defaults to 10.
- `FLUENTD_ASYNC_CONNECT` [true|false] Docker connects to Fluentd in the background. Messages are buffered until the connection is established. Defaults to false.
- `FLUENTD_SUBSECOND_PRECISION` [true|false] Generates event logs in nanosecond resolution. Defaults to false.
- `FLUENTD_REQUEST_ACK` [true|false] For reliability. Fluent-bit currently doesn't support this. Defaults to false.
- `FLUENTD_WRITE_TIMEOUT` [int] Write timeout to post to fluentd/fluent-bit. Defaults to 3 seconds.


Configure Logspout to receive forwarded messages, something like this:

```
<source>
  type forward
  port 24224
  bind 0.0.0.0
</source>

# Assuming environment variable "TAG_PREFIX" is set to docker
<match docker.**>
  # Handle messages here.
</match>
```

## Build and Run Locally

```bash

# Build logspout locally with custom fluentd module using Dockerfile
>> docker build -t mycustomlogspout .

# Example to run custom built logspout locally:
>> docker run --rm --name="logspout" \
			-v /var/run/docker.sock:/var/run/docker.sock \
			-e TAG_PREFIX=docker \
			-e TAG_SUFFIX_LABEL="com.mycompany.service" \
			-e FLUENTD_ASYNC_CONNECT="true" \
			-e LOGSPOUT="ignore" \
			mycustomlogspout \
				./logspout fluentd://<FLUENT_IP>:24224

```


## Optional Testing

```bash

# Run standalone fluent-bit instance
>> docker run -ti -p 24224:24224 \
        fluent/fluent-bit:1.2 /fluent-bit/bin/fluent-bit \
            -i forward://0.0.0.0:24224 -o stdout


# Send sample log from test container, you should view the log entry
# captured in logspout and a similar log entry in fluent-bit.
>> docker run -i \
    --log-driver=json-file \
    --log-opt max-size=8m \
    --log-opt max-file=3 \
    --log-opt tag="docker" \
    -l com.mycompany.service="test" \
    alpine echo hello world

```

## Demo

![](demo.gif)
